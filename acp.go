package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
)

// 通过 ACP（Agent Client Protocol）接入外部 Agent：
// JSON-RPC 2.0，按行分隔，走子进程的 stdin/stdout。
// 协议版本 1；client 不声明文件系统与终端能力，Agent 只能拿到我们主动发送的文字。

type acpConfig struct {
	// command 在登录 shell 里执行，例如 "npx -y pi-acp"。
	command string
	// cwd 是 Agent 的工作目录，默认是数据目录下空的 agent/，避免它去翻用户的文件。
	cwd string
	// persistSession 把会话 id 存进设置，下次启动时用 session/load 恢复（角色对话用；查词不用）。
	persistSession bool
}

const (
	acpProtocolVersion    = 1
	acpMaxLineBytes       = 1024 * 1024
	acpStderrKeepBytes    = 4096
	acpInitTimeout        = 180 * time.Second
	acpPromptTimeout      = 600 * time.Second
	acpStopTimeout        = 12 * time.Second
	acpSessionLoadTimeout = 120 * time.Second
	pathCaptureTimeout    = 5 * time.Second
	settingSessionPrefix  = "ai.acpSession"
)

var (
	errACPNoCommand   = errors.New("no command")
	errACPSpawnFailed = errors.New("spawn failed")
	errACPTimeout     = errors.New("timeout")
	errACPEndOfStream = errors.New("end of stream")
	errACPWriteFailed = errors.New("write failed")
	errACPAgent       = errors.New("agent error")
	errACPBadResponse = errors.New("bad response")
	errACPLineTooLong = errors.New("line too long")
)

type acpAgent struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan map[string]any
	done   chan struct{}
	closed sync.Once

	writeMu sync.Mutex
	nextID  int64

	name    string
	version string
	command string

	lastError            string
	supportsSystemPrompt bool
	supportsLoadSession  bool
	supportsResume       bool

	sessionsMu sync.Mutex
	sessions   map[string]string

	alive bool

	stderrMu sync.Mutex
	stderr   []byte
}

type acpTurnInfo struct {
	active    bool
	cancelled bool
	turnID    string
	sessionID string
}

var (
	// acpMu 串行化「启动 / 提问」。cancel 与 stop 不走这把锁，否则会被正在进行的提问卡住。
	acpMu sync.Mutex
	// acpKillMu 保护 acpCurrent 指针。
	acpKillMu  sync.Mutex
	acpCurrent *acpAgent
	acpTurnMu  sync.Mutex
	acpTurn    acpTurnInfo
)

// ---------- JSON 小工具 ----------

func jsonIDEquals(v any, id int64) bool {
	switch x := v.(type) {
	case float64:
		return int64(x) == id
	case int64:
		return x == id
	}
	return false
}

func jsonErrorMessage(v any) string {
	return jsonStr(jsonMap(v), "message")
}

// ---------- Agent 进程 ----------

func (a *acpAgent) readLoop(stdout io.Reader) {
	defer close(a.lines)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), acpMaxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			a.appendDiagnostic(line)
			continue
		}
		select {
		case a.lines <- msg:
		case <-a.done:
			return
		}
	}
}

func (a *acpAgent) drainStderr(stderr io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := stderr.Read(buf)
		if n > 0 {
			a.stderrMu.Lock()
			a.stderr = append(a.stderr, buf[:n]...)
			if len(a.stderr) > acpStderrKeepBytes {
				a.stderr = a.stderr[len(a.stderr)-acpStderrKeepBytes:]
			}
			a.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (a *acpAgent) stderrTail() string {
	a.stderrMu.Lock()
	defer a.stderrMu.Unlock()
	return strings.TrimSpace(string(a.stderr))
}

// appendDiagnostic 把 Agent 打到 stdout 的非 JSON 行也留进诊断尾巴，进程退出时能一起显示。
func (a *acpAgent) appendDiagnostic(line string) {
	if len(line) > acpStderrKeepBytes-1 {
		line = line[len(line)-(acpStderrKeepBytes-1):]
	}
	a.stderrMu.Lock()
	defer a.stderrMu.Unlock()
	a.stderr = append(a.stderr, line...)
	a.stderr = append(a.stderr, '\n')
	if len(a.stderr) > acpStderrKeepBytes {
		a.stderr = a.stderr[len(a.stderr)-acpStderrKeepBytes:]
	}
}

func (a *acpAgent) writeLine(line []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if _, err := a.stdin.Write(append(line, '\n')); err != nil {
		return errACPWriteFailed
	}
	return nil
}

func (a *acpAgent) sendRequest(method string, params any) (int64, error) {
	a.writeMu.Lock()
	id := a.nextID
	a.nextID++
	a.writeMu.Unlock()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return 0, err
	}
	if err := a.writeLine(body); err != nil {
		return 0, err
	}
	return id, nil
}

func (a *acpAgent) sendNotification(method string, params any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	return a.writeLine(body)
}

func (a *acpAgent) sendResult(id any, result any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	if err != nil {
		return err
	}
	return a.writeLine(body)
}

func (a *acpAgent) sendError(id any, code int64, message string) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	if err != nil {
		return err
	}
	return a.writeLine(body)
}

// waitResponse 读一条响应，跳过通知与 Agent 发来的请求（一律拒绝）。
func (a *acpAgent) waitResponse(id int64, timeout time.Duration) (map[string]any, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case msg, ok := <-a.lines:
			if !ok {
				return nil, errACPEndOfStream
			}
			if _, ok := msg["method"].(string); ok {
				if reqID, hasID := msg["id"]; hasID {
					// 我们没有声明 fs / terminal / elicitation 能力，一律拒绝。
					_ = a.sendError(reqID, -32601, "书桌不支持这个请求。")
				}
				continue
			}
			if !jsonIDEquals(msg["id"], id) {
				continue
			}
			if errVal, ok := msg["error"]; ok {
				a.lastError = jsonErrorMessage(errVal)
				if a.lastError == "" {
					a.lastError = "外部 Agent 返回了错误。"
				}
				return nil, errACPAgent
			}
			return msg, nil
		case <-timer.C:
			return nil, errACPTimeout
		case <-a.done:
			return nil, errACPEndOfStream
		}
	}
}

func (a *acpAgent) initialize() error {
	params := map[string]any{
		"protocolVersion": acpProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "bookdesk", "version": "0.1.0"},
	}
	id, err := a.sendRequest("initialize", params)
	if err != nil {
		return err
	}
	msg, err := a.waitResponse(id, acpInitTimeout)
	if err != nil {
		return err
	}
	result := jsonMap(msg["result"])
	if result == nil {
		return errACPBadResponse
	}

	info := jsonMap(result["agentInfo"])
	name := jsonStr(info, "title")
	if name == "" {
		name = jsonStr(info, "name")
	}
	if name != "" {
		a.name = name
	}
	if v := jsonStr(info, "version"); v != "" {
		a.version = v
	}

	caps := jsonMap(result["agentCapabilities"])
	if b, ok := caps["loadSession"].(bool); ok {
		a.supportsLoadSession = b
	}
	sessionCaps := jsonMap(caps["sessionCapabilities"])
	if _, ok := sessionCaps["systemPrompt"].(map[string]any); ok {
		a.supportsSystemPrompt = true
	}
	if _, ok := sessionCaps["resume"].(map[string]any); ok {
		a.supportsResume = true
	}
	return nil
}

func (a *acpAgent) destroy() {
	a.closed.Do(func() {
		a.alive = false
		close(a.done)
		killGroup(a.cmd, syscall.SIGKILL)
		_ = a.cmd.Wait()
	})
}

// ---------- 登录 shell PATH ----------

var (
	loginPathOnce sync.Once
	loginPath     string
)

// loginShellPath 从真正的登录 shell 里取 PATH。nvm、fnm、mise 常把 PATH 写在 .zshrc，
// 只跑 `zsh -l -c` 读不到；先试交互式（-i），不行再退回非交互式。
func loginShellPath() string {
	loginPathOnce.Do(func() {
		if path := captureShellPath(true); path != "" {
			loginPath = path
			return
		}
		loginPath = captureShellPath(false)
	})
	return loginPath
}

func captureShellPath(interactive bool) string {
	script := `printf '\n__RC_PATH_BEGIN__%s__RC_PATH_END__\n' "$PATH"`
	flag := "-lc"
	if interactive {
		flag = "-lic"
	}
	ctx, cancel := context.WithTimeout(context.Background(), pathCaptureTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/zsh", flag, script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		killGroup(cmd, syscall.SIGKILL)
		return nil
	}
	cmd.WaitDelay = time.Second
	out, _ := cmd.Output()

	text := string(out)
	begin := strings.Index(text, "__RC_PATH_BEGIN__")
	if begin < 0 {
		return ""
	}
	start := begin + len("__RC_PATH_BEGIN__")
	rest := text[start:]
	end := strings.Index(rest, "__RC_PATH_END__")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// ---------- 已知 Agent ----------

type knownAgent struct {
	id         string
	name       string
	bins       []string
	args       string
	cliBins    []string
	npxPackage string
	npxArgs    string
	hint       string
	native     bool
}

// 命令来自 ACP 官方 registry（cdn.agentclientprotocol.com/registry/v1/latest/registry.json）。
var knownAgents = []knownAgent{
	{id: "pi", name: "Pi", bins: []string{"pi"}, hint: "npm i -g @mariozechner/pi-coding-agent", native: true},
	{id: "claude", name: "Claude Code", bins: []string{"claude-code-acp", "claude-agent-acp"}, cliBins: []string{"claude"}, npxPackage: "@agentclientprotocol/claude-agent-acp", hint: "npm i -g @agentclientprotocol/claude-agent-acp"},
	{id: "codex", name: "Codex", bins: []string{"codex"}, hint: "npm i -g @openai/codex", native: true},
	{id: "cursor", name: "Cursor", bins: []string{"cursor-agent", "agent"}, args: "acp", hint: "安装 Cursor CLI（cursor-agent）"},
	{id: "gemini", name: "Gemini CLI", bins: []string{"gemini"}, args: "--acp", npxPackage: "@google/gemini-cli", npxArgs: "--acp", hint: "npm i -g @google/gemini-cli"},
	{id: "opencode", name: "OpenCode", bins: []string{"opencode"}, args: "acp", hint: "安装 OpenCode CLI"},
	{id: "devin", name: "Devin", bins: []string{"devin"}, args: "acp", hint: "安装 Devin CLI"},
	{id: "grok", name: "Grok", bins: []string{"grok"}, args: "agent stdio", npxPackage: "@xai-official/grok", npxArgs: "agent stdio", hint: "npm i -g @xai-official/grok"},
	{id: "goose", name: "goose", bins: []string{"goose"}, args: "acp", hint: "安装 goose CLI"},
	{id: "kimi", name: "Kimi CLI", bins: []string{"kimi"}, args: "acp", hint: "安装 Kimi CLI"},
	{id: "qwen", name: "Qwen Code", bins: []string{"qwen"}, args: "--acp --experimental-skills", npxPackage: "@qwen-code/qwen-code", npxArgs: "--acp --experimental-skills", hint: "npm i -g @qwen-code/qwen-code"},
}

// 登录 shell 的 PATH 抓不到时的兜底目录（Finder 启动也能找到 Homebrew 命令）。
const fallbackPath = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

func executableOnPath(pathEnv, name string) bool {
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

func firstOnPath(pathEnv string, names []string) string {
	for _, name := range names {
		if executableOnPath(pathEnv, name) {
			return name
		}
	}
	return ""
}

// quietPiStartup 在 Agent 的工作目录里放一份项目级 Pi 设置，让它安静启动；
// 只影响这个目录下的会话，不动用户自己的 ~/.pi 配置。
func quietPiStartup(cwd string) {
	piDir := filepath.Join(cwd, ".pi")
	if err := ensureDir(piDir); err != nil {
		return
	}
	path := filepath.Join(piDir, "settings.json")
	if fileExists(path) {
		return
	}
	_ = writeFile(path, []byte("{\"quietStartup\":true}\n"))
}

// detectAgents 检测本机装了哪些已知 ACP Agent：看登录 shell 的 PATH 里有没有对应可执行文件，
// 没有就看能不能用 npx 拉取。结果按「已安装 → 需下载 → 未安装」排序。
func detectAgents() []AgentCandidate {
	captured := loginShellPath()
	pathEnv := captured
	if pathEnv == "" {
		pathEnv = fallbackPath
	}
	npxOK := executableOnPath(pathEnv, "npx")

	installed := make([]AgentCandidate, 0)
	adapted := make([]AgentCandidate, 0)
	downloadable := make([]AgentCandidate, 0)
	missing := make([]AgentCandidate, 0)

	for _, known := range knownAgents {
		if bin := firstOnPath(pathEnv, known.bins); bin != "" {
			command := bin
			if known.args != "" {
				command = bin + " " + known.args
			}
			detail := bin
			if known.native {
				detail = "原生 CLI，按需启动"
			}
			installed = append(installed, AgentCandidate{
				ID: known.id, Name: known.name, Command: command, State: "installed", Detail: detail,
			})
		} else if known.npxPackage != "" && npxOK {
			command := "npx -y " + known.npxPackage
			if known.npxArgs != "" {
				command += " " + known.npxArgs
			}
			// 底层 CLI 装了、只是没有 ACP 适配器：仍算已安装，适配器由 npx 拉取。
			if cliBin := firstOnPath(pathEnv, known.cliBins); cliBin != "" {
				adapted = append(adapted, AgentCandidate{
					ID: known.id, Name: known.name, Command: command, State: "adapter",
					Detail: "已装 " + cliBin + " · 经 " + known.npxPackage + " 适配器",
				})
			} else {
				downloadable = append(downloadable, AgentCandidate{
					ID: known.id, Name: known.name, Command: command, State: "download",
					Detail: "首次运行会用 npx 下载",
				})
			}
		} else {
			missing = append(missing, AgentCandidate{
				ID: known.id, Name: known.name, Command: "", State: "missing", Detail: known.hint,
			})
		}
	}

	out := make([]AgentCandidate, 0, len(installed)+len(adapted)+len(downloadable)+len(missing))
	out = append(out, installed...)
	out = append(out, adapted...)
	out = append(out, downloadable...)
	out = append(out, missing...)
	return out
}

// ---------- 进程与生命周期 ----------

func spawnAgent(config acpConfig) (*acpAgent, error) {
	command := strings.TrimSpace(config.command)
	if command == "" {
		return nil, errACPNoCommand
	}
	_ = ensureDir(config.cwd)
	if strings.Contains(command, "pi-acp") {
		quietPiStartup(config.cwd)
	}
	script := command
	if path := loginShellPath(); path != "" {
		script = "export PATH='" + strings.ReplaceAll(path, "'", `'\''`) + "'; " + command
	}

	cmd := exec.Command("/bin/zsh", "-l", "-c", script)
	cmd.Dir = config.cwd
	// 让 Agent 自成一个进程组，停止时能把 npx / node 一起杀掉。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errACPSpawnFailed
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errACPSpawnFailed
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, errACPSpawnFailed
	}
	if err := cmd.Start(); err != nil {
		return nil, errACPSpawnFailed
	}

	agent := &acpAgent{
		cmd:      cmd,
		stdin:    stdin,
		lines:    make(chan map[string]any, 64),
		done:     make(chan struct{}),
		nextID:   1,
		command:  command,
		sessions: map[string]string{},
		alive:    true,
	}
	go agent.readLoop(stdout)
	go agent.drainStderr(stderr)
	return agent, nil
}

func killAgentLocked() {
	acpKillMu.Lock()
	agent := acpCurrent
	acpCurrent = nil
	acpKillMu.Unlock()
	if agent != nil {
		agent.destroy()
	}
}

// retireAgent 在提问途中发现 Agent 已经死了：清掉当前指针，下一次提问重新启动。
func retireAgent(agent *acpAgent) {
	acpKillMu.Lock()
	if acpCurrent == agent {
		acpCurrent = nil
	}
	acpKillMu.Unlock()
	agent.destroy()
}

func ensureAgent(config acpConfig) (*acpAgent, error) {
	command := strings.TrimSpace(config.command)
	acpKillMu.Lock()
	current := acpCurrent
	acpKillMu.Unlock()
	if current != nil {
		if current.alive && current.command == command {
			return current, nil
		}
		killAgentLocked()
	}

	agent, err := spawnAgent(config)
	if err != nil {
		return nil, err
	}
	acpKillMu.Lock()
	acpCurrent = agent
	acpKillMu.Unlock()
	if err := agent.initialize(); err != nil {
		killAgentLocked()
		return nil, err
	}
	return agent, nil
}

// ---------- 会话 ----------

func acpSessionSettingKey(command, key string) string {
	return settingSessionPrefix + "\x1f" + command + "\x1f" + key
}

func storedACPSession(command, key string) string {
	stored, err := getSetting(acpSessionSettingKey(command, key))
	if err != nil || stored == nil {
		return ""
	}
	return strings.TrimSpace(*stored)
}

func rememberACPSession(command, key, sessionID string) {
	_ = setSetting(acpSessionSettingKey(command, key), sessionID)
}

func forgetStoredACPSession(command, key string) {
	_ = deleteSetting(acpSessionSettingKey(command, key))
}

// restoreSession 用 session/load（或 session/resume）恢复上次的会话。
// 返回 false 表示这个会话在 Agent 那边已经不存在，可以退回 session/new。
func restoreSession(agent *acpAgent, sessionID, cwd string) (bool, error) {
	method := "session/resume"
	if agent.supportsLoadSession {
		method = "session/load"
	}
	id, err := agent.sendRequest(method, map[string]any{
		"sessionId":  sessionID,
		"cwd":        cwd,
		"mcpServers": []any{},
	})
	if err != nil {
		if err == errACPWriteFailed {
			retireAgent(agent)
		}
		return false, err
	}
	// 回放的历史会以 session/update 通知的形式到来，waitResponse 直接忽略。
	if _, err := agent.waitResponse(id, acpSessionLoadTimeout); err != nil {
		if err == errACPAgent {
			return false, nil
		}
		retireAgent(agent)
		return false, err
	}
	return true, nil
}

func cachedSession(agent *acpAgent, key string) string {
	agent.sessionsMu.Lock()
	defer agent.sessionsMu.Unlock()
	return agent.sessions[key]
}

func cacheSession(agent *acpAgent, key, sessionID string) {
	agent.sessionsMu.Lock()
	agent.sessions[key] = sessionID
	agent.sessionsMu.Unlock()
}

func getOrCreateSession(agent *acpAgent, key, systemPrompt, cwd string, persist bool) (string, error) {
	if sessionID := cachedSession(agent, key); sessionID != "" {
		return sessionID, nil
	}

	if persist && (agent.supportsLoadSession || agent.supportsResume) {
		if stored := storedACPSession(agent.command, key); stored != "" {
			restored, err := restoreSession(agent, stored, cwd)
			if err != nil {
				forgetStoredACPSession(agent.command, key)
				return "", err
			}
			if restored {
				cacheSession(agent, key, stored)
				return stored, nil
			}
			// 会话在 Agent 那边已经不存在了，退回新建。
			forgetStoredACPSession(agent.command, key)
		}
	}

	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	if agent.supportsSystemPrompt && systemPrompt != "" {
		params["systemPrompt"] = map[string]any{
			"mode": "append",
			"content": []any{
				map[string]any{"type": "text", "text": systemPrompt},
			},
		}
	}
	id, err := agent.sendRequest("session/new", params)
	if err != nil {
		if err == errACPWriteFailed {
			retireAgent(agent)
		}
		return "", err
	}
	msg, err := agent.waitResponse(id, acpInitTimeout)
	if err != nil {
		return "", err
	}
	result := jsonMap(msg["result"])
	sessionID := jsonStr(result, "sessionId")
	if sessionID == "" {
		return "", errACPBadResponse
	}
	cacheSession(agent, key, sessionID)
	if persist {
		rememberACPSession(agent.command, key, sessionID)
	}
	return sessionID, nil
}

// ---------- 一轮提问 ----------

func setActiveTurn(turnID, sessionID string) {
	acpTurnMu.Lock()
	acpTurn = acpTurnInfo{active: true, turnID: turnID, sessionID: sessionID}
	acpTurnMu.Unlock()
}

func clearActiveTurn() {
	acpTurnMu.Lock()
	acpTurn.active = false
	acpTurnMu.Unlock()
}

func turnCancelled() bool {
	acpTurnMu.Lock()
	defer acpTurnMu.Unlock()
	return acpTurn.active && acpTurn.cancelled
}

func handleUpdate(win *mygo.Window, turnID, sessionID string, msg map[string]any, text *strings.Builder) {
	params := jsonMap(msg["params"])
	if params == nil {
		return
	}
	// 同一个 Agent 进程里有多个会话（每个角色一个、查词一个），
	// 别把别的会话的片段混进这一轮。
	if sid, ok := params["sessionId"].(string); ok && sid != "" && sid != sessionID {
		return
	}
	update := jsonMap(params["update"])
	if update == nil {
		return
	}
	switch jsonStr(update, "sessionUpdate") {
	case "agent_message_chunk":
		content := jsonMap(update["content"])
		if jsonStr(content, "type") != "text" {
			return
		}
		chunk := jsonStr(content, "text")
		if chunk == "" {
			return
		}
		text.WriteString(chunk)
		emitAcpUpdate(win, turnID, "text", chunk)
	case "agent_thought_chunk":
		content := jsonMap(update["content"])
		if jsonStr(content, "type") != "text" {
			return
		}
		chunk := jsonStr(content, "text")
		if chunk == "" {
			return
		}
		emitAcpUpdate(win, turnID, "thought", chunk)
	case "tool_call":
		title := jsonStr(update, "title")
		if title == "" {
			title = "工具调用"
		}
		emitAcpUpdate(win, turnID, "tool", title)
	}
}

// permissionReply 阅读场景不需要文件、命令或终端权限，一律拒绝。
// ACP 里回 cancelled 表示「这次请求被取消」，有的 Agent 会因此中止整轮；
// 所以优先选 reject_once / reject_always，让 Agent 换个方式继续。
func permissionReply(params map[string]any, sessionID string) any {
	cancelled := map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
	if params == nil {
		return cancelled
	}
	if sid, ok := params["sessionId"].(string); ok && sid != "" && sid != sessionID {
		return cancelled
	}
	options, ok := params["options"].([]any)
	if !ok {
		return cancelled
	}
	rejectOnce := ""
	rejectAlways := ""
	for _, item := range options {
		option := jsonMap(item)
		kind := jsonStr(option, "kind")
		optionID := jsonStr(option, "optionId")
		if rejectOnce == "" && kind == "reject_once" {
			rejectOnce = optionID
		}
		if rejectAlways == "" && kind == "reject_always" {
			rejectAlways = optionID
		}
	}
	chosen := rejectOnce
	if chosen == "" {
		chosen = rejectAlways
	}
	if chosen == "" {
		return cancelled
	}
	return map[string]any{
		"outcome": map[string]any{"outcome": "selected", "optionId": chosen},
	}
}

func agentErrorMessage(agent *acpAgent, err error) string {
	switch err {
	case errACPNoCommand:
		return "还没有配置外部 Agent 的命令。打开设置，填上例如 npx -y pi-acp。"
	case errACPSpawnFailed:
		return "启动外部 Agent 失败。请检查设置里的命令，确认它在终端里能直接运行。"
	case errACPTimeout:
		return "外部 Agent 长时间没有响应，已经停止它。"
	case errACPEndOfStream, errACPWriteFailed:
		if agent != nil {
			if tail := agent.stderrTail(); tail != "" {
				return "外部 Agent 进程已退出。\n" + tail
			}
		}
		return "外部 Agent 进程已退出。请检查设置里的命令，或先在这个 Agent 里登录。"
	case errACPAgent:
		if agent != nil && agent.lastError != "" {
			return agent.lastError
		}
		return "外部 Agent 返回了错误。"
	case errACPBadResponse:
		return "外部 Agent 返回了无法理解的响应。"
	case errACPLineTooLong:
		return "外部 Agent 返回了过长的消息。"
	}
	return "外部 Agent 出错了。"
}

// prompt 发送一次提问并等待这一轮结束。流式片段通过 acp:update 事件发给前端。
func acpPrompt(ctx context.Context, config acpConfig, sessionKey, turnID, systemPrompt, userText string) ChatReply {
	acpMu.Lock()
	defer acpMu.Unlock()

	agent, err := ensureAgent(config)
	if err != nil {
		return ChatReply{OK: false, Message: agentErrorMessage(nil, err)}
	}
	sessionID, err := getOrCreateSession(agent, sessionKey, systemPrompt, config.cwd, config.persistSession)
	if err != nil {
		return ChatReply{OK: false, Message: agentErrorMessage(agent, err)}
	}

	promptText := userText
	if !agent.supportsSystemPrompt && systemPrompt != "" {
		promptText = systemPrompt + "\n\n" + userText
	}
	params := map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": promptText}},
	}

	setActiveTurn(turnID, sessionID)
	defer clearActiveTurn()

	id, err := agent.sendRequest("session/prompt", params)
	if err != nil {
		if err == errACPWriteFailed {
			retireAgent(agent)
		}
		return ChatReply{OK: false, Message: agentErrorMessage(agent, err)}
	}

	win := mygo.CallerWindow(ctx)
	var text strings.Builder
	stopReason := ""

	deadline := time.NewTimer(acpPromptTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var stopDeadline time.Time

	for {
		now := time.Now()
		// 取消可能在消息之间到达；每轮都看一眼，别等下一次读超时才记时。
		if stopDeadline.IsZero() && turnCancelled() {
			stopDeadline = now.Add(acpStopTimeout)
		}
		if !stopDeadline.IsZero() && now.After(stopDeadline) {
			// Agent 没有在取消后收尾：结束它，别让下一次提问被卡住。
			retireAgent(agent)
			return stoppedReply(text.String())
		}

		select {
		case msg, ok := <-agent.lines:
			if !ok {
				retireAgent(agent)
				return ChatReply{OK: false, Message: agentErrorMessage(agent, errACPEndOfStream)}
			}
			if method, ok := msg["method"].(string); ok {
				switch method {
				case "session/update":
					handleUpdate(win, turnID, sessionID, msg, &text)
				case "session/request_permission":
					if reqID, hasID := msg["id"]; hasID {
						reply := permissionReply(jsonMap(msg["params"]), sessionID)
						_ = agent.sendResult(reqID, reply)
					}
				default:
					if reqID, hasID := msg["id"]; hasID {
						_ = agent.sendError(reqID, -32601, "书桌不支持这个请求。")
					}
				}
				continue
			}
			if !jsonIDEquals(msg["id"], id) {
				continue
			}
			if errVal, ok := msg["error"]; ok {
				message := jsonErrorMessage(errVal)
				if message == "" {
					message = "外部 Agent 返回了错误。"
				}
				return ChatReply{OK: false, Message: message}
			}
			if result := jsonMap(msg["result"]); result != nil {
				if sr, ok := result["stopReason"].(string); ok {
					stopReason = sr
				}
			}
			goto done
		case <-deadline.C:
			retireAgent(agent)
			return ChatReply{OK: false, Message: agentErrorMessage(agent, errACPTimeout)}
		case <-ticker.C:
			continue
		}
	}

done:
	answer := strings.TrimSpace(text.String())
	if stopReason == "cancelled" {
		return stoppedReply(answer)
	}
	if answer == "" {
		if stopReason == "refusal" {
			return ChatReply{OK: false, Message: "外部 Agent 拒绝回答这个问题。"}
		}
		return ChatReply{OK: false, Message: "外部 Agent 没有返回文字。"}
	}
	if stopReason == "max_tokens" {
		return ChatReply{OK: true, Text: answer + "\n\n（回答被截断）"}
	}
	return ChatReply{OK: true, Text: answer}
}

// cancel 是前端点「停止」：只发 session/cancel，不抢 acpMu。
func acpCancel(turnID string) {
	acpKillMu.Lock()
	agent := acpCurrent
	acpKillMu.Unlock()
	if agent == nil || !agent.alive {
		return
	}

	acpTurnMu.Lock()
	sessionID := ""
	matches := false
	if acpTurn.active {
		matches = turnID == "" || acpTurn.turnID == turnID
		if matches {
			acpTurn.cancelled = true
			sessionID = acpTurn.sessionID
		}
	}
	acpTurnMu.Unlock()
	if !matches {
		return
	}
	_ = agent.sendNotification("session/cancel", map[string]any{"sessionId": sessionID})
}

// acpForgetSession 清除一个会话键的记忆：本地存的会话 id（所有 Agent 命令下的），以及内存里缓存的会话。
// 下一次提问会新建会话；Agent 自己存的旧对话不在这个应用的管理范围。
func acpForgetSession(sessionKey string) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}

	// 键是「前缀 \x1f 命令 \x1f 会话键」，用后缀匹配删掉所有命令下的记录。
	var pattern strings.Builder
	pattern.WriteByte('%')
	pattern.WriteByte('\x1f')
	for i := 0; i < len(key); i++ {
		switch key[i] {
		case '%', '_', '\\':
			pattern.WriteByte('\\')
			pattern.WriteByte(key[i])
		default:
			pattern.WriteByte(key[i])
		}
	}
	_ = deleteSettingsLike(pattern.String())

	acpKillMu.Lock()
	agent := acpCurrent
	acpKillMu.Unlock()
	if agent != nil {
		agent.sessionsMu.Lock()
		delete(agent.sessions, key)
		agent.sessionsMu.Unlock()
	}
}

// acpStart 是设置页的「检测」：启动并初始化 Agent，返回它的名字。
func acpStart(ctx context.Context, config acpConfig) AgentTest {
	acpMu.Lock()
	defer acpMu.Unlock()

	killAgentLocked()
	agent, err := spawnAgent(config)
	if err != nil {
		return AgentTest{OK: false, Message: agentErrorMessage(nil, err)}
	}
	acpKillMu.Lock()
	acpCurrent = agent
	acpKillMu.Unlock()
	if err := agent.initialize(); err != nil {
		message := agentErrorMessage(agent, err)
		killAgentLocked()
		return AgentTest{OK: false, Message: message}
	}
	return AgentTest{OK: true, Name: agent.name}
}

func acpStop() {
	killAgentLocked()
}

func acpRunningName() string {
	acpKillMu.Lock()
	agent := acpCurrent
	acpKillMu.Unlock()
	if agent == nil || !agent.alive {
		return ""
	}
	return agent.name
}

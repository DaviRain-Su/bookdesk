package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
)

// Pi 和 Codex 有自己的原生协议，不走 ACP：
// - Pi：`pi -p --mode json`，按行输出 JSON 事件。
// - Codex：`codex exec --json`，按行输出 JSON 事件。
// 每次提问启动一个进程，结束就退出；会话 id 存在本地设置里，下一轮接着用。

type nativeEngine int

const (
	nativePi nativeEngine = iota
	nativeCodex
)

const (
	maxLineBytes       = 1024 * 1024
	promptTimeout      = 600 * time.Second
	stopTimeout        = 3 * time.Second
	exitGrace          = 3 * time.Second
	stderrKeepBytes    = 4096
	settingPiPrefix    = "ai.piSession"
	settingCodexPrefix = "ai.codexSession"
)

// nativeEngineForCommand 从设置里的命令判断是不是原生引擎：命令的第一个词是 pi / codex。
func nativeEngineForCommand(command string) (nativeEngine, bool) {
	parts := splitCommand(command)
	if len(parts) == 0 {
		return 0, false
	}
	switch filepath.Base(parts[0]) {
	case "pi":
		return nativePi, true
	case "codex":
		return nativeCodex, true
	}
	return 0, false
}

type nativeRun struct {
	cmd *exec.Cmd
	// done 在整轮结束时关闭，取消后的兜底计时器据此停止。
	done chan struct{}

	mu        sync.Mutex
	cancelled bool
	stderr    []byte

	// 以下字段只由 prompt 所在的 goroutine 读写。
	settled      bool
	failed       bool
	errorMessage string
	threadID     string
}

var (
	nativeMu      sync.Mutex
	nativeCurrent *nativeRun
	nativeTurn    string
)

func nativeRegister(run *nativeRun, turnID string) {
	nativeMu.Lock()
	nativeCurrent = run
	nativeTurn = turnID
	nativeMu.Unlock()
}

func nativeUnregister(run *nativeRun) {
	nativeMu.Lock()
	if nativeCurrent == run {
		nativeCurrent = nil
		nativeTurn = ""
	}
	nativeMu.Unlock()
}

func (r *nativeRun) isCancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

func (r *nativeRun) stderrTail() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(string(r.stderr))
}

func killGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}

func nativeCancel(turnID string) {
	nativeMu.Lock()
	run := nativeCurrent
	turn := nativeTurn
	nativeMu.Unlock()
	if run == nil {
		return
	}
	if turnID != "" && turnID != turn {
		return
	}
	run.mu.Lock()
	run.cancelled = true
	run.mu.Unlock()
	killGroup(run.cmd, syscall.SIGTERM)
	go func() {
		select {
		case <-run.done:
		case <-time.After(stopTimeout):
			killGroup(run.cmd, syscall.SIGKILL)
		}
	}()
}

func nativeSessionSettingKey(engine nativeEngine, key string) string {
	prefix := settingPiPrefix
	if engine == nativeCodex {
		prefix = settingCodexPrefix
	}
	return prefix + "\x1f" + key
}

func storedSessionID(engine nativeEngine, key string) string {
	stored, err := getSetting(nativeSessionSettingKey(engine, key))
	if err != nil || stored == nil {
		return ""
	}
	return strings.TrimSpace(*stored)
}

func rememberSession(engine nativeEngine, key, id string) {
	_ = setSetting(nativeSessionSettingKey(engine, key), id)
}

func forgetSession(engine nativeEngine, key string) {
	_ = deleteSetting(nativeSessionSettingKey(engine, key))
}

// shellArgv 把命令和参数交给登录 shell 执行：`exec "$0" "$@"`，参数不经过二次转义。
func shellArgv(argv []string) (string, []string) {
	script := `exec "$0" "$@"`
	if path := loginShellPath(); path != "" {
		script = "export PATH='" + strings.ReplaceAll(path, "'", `'\''`) + "'; " + script
	}
	args := append([]string{"-l", "-c", script}, argv...)
	return "/bin/zsh", args
}

func newNativeCmd(argv []string, cwd string) *exec.Cmd {
	shell, args := shellArgv(argv)
	cmd := exec.Command(shell, args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func drainNativeStderr(r io.Reader, run *nativeRun) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			run.mu.Lock()
			run.stderr = append(run.stderr, buf[:n]...)
			if len(run.stderr) > stderrKeepBytes {
				run.stderr = run.stderr[len(run.stderr)-stderrKeepBytes:]
			}
			run.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func jsonMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func jsonStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func handlePiLine(run *nativeRun, win *mygo.Window, turnID string, text *strings.Builder, line string) {
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) != nil {
		return
	}
	switch jsonStr(obj, "type") {
	case "message_update":
		event := jsonMap(obj["assistantMessageEvent"])
		if jsonStr(event, "type") != "text_delta" {
			return
		}
		delta := jsonStr(event, "delta")
		if delta == "" {
			return
		}
		text.WriteString(delta)
		emitAcpUpdate(win, turnID, "text", delta)
	case "tool_execution_start":
		name := jsonStr(obj, "toolName")
		if name == "" {
			name = "工具调用"
		}
		emitAcpUpdate(win, turnID, "tool", name)
	case "message_end":
		message := jsonMap(obj["message"])
		if jsonStr(message, "role") != "assistant" {
			return
		}
		if jsonStr(message, "stopReason") == "error" {
			run.failed = true
			msg := jsonStr(message, "errorMessage")
			if msg == "" {
				msg = "Pi 返回了错误。"
			}
			run.errorMessage = msg
		}
	case "agent_settled":
		run.settled = true
	}
}

func handleCodexLine(run *nativeRun, win *mygo.Window, turnID string, text *strings.Builder, line string) {
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) != nil {
		return
	}
	switch jsonStr(obj, "type") {
	case "thread.started":
		run.threadID = jsonStr(obj, "thread_id")
	case "item.completed":
		item := jsonMap(obj["item"])
		switch jsonStr(item, "type") {
		case "agent_message":
			chunk := jsonStr(item, "text")
			if chunk == "" {
				return
			}
			text.WriteString(chunk)
			emitAcpUpdate(win, turnID, "text", chunk)
		case "command_execution":
			emitAcpUpdate(win, turnID, "tool", "运行命令")
		}
	case "turn.completed":
		run.settled = true
	case "turn.failed":
		run.failed = true
		run.settled = true
		msg := jsonStr(jsonMap(obj["error"]), "message")
		if msg == "" {
			msg = "Codex 返回了错误。"
		}
		run.errorMessage = msg
	case "error":
		run.failed = true
		msg := jsonStr(obj, "message")
		if msg == "" {
			msg = "Codex 返回了错误。"
		}
		run.errorMessage = msg
	}
}

func nativeName(engine nativeEngine) string {
	if engine == nativePi {
		return "Pi"
	}
	return "Codex"
}

func spawnError(engine nativeEngine) ChatReply {
	return ChatReply{
		OK:      false,
		Message: "启动 " + nativeName(engine) + " 失败。请确认 `" + strings.ToLower(nativeName(engine)) + "` 在终端里能直接运行。",
	}
}

func failureMessage(engine nativeEngine, run *nativeRun) string {
	if run.errorMessage != "" {
		return run.errorMessage
	}
	if tail := run.stderrTail(); tail != "" {
		return nativeName(engine) + " 返回了错误：\n" + tail
	}
	return nativeName(engine) + " 返回了错误。"
}

func stoppedReply(text string) ChatReply {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ChatReply{OK: false, Message: "已停止。"}
	}
	return ChatReply{OK: true, Text: trimmed + "\n\n（已停止）"}
}

func nativePrompt(
	ctx context.Context,
	engine nativeEngine,
	commandRaw, cwd, sessionKey, turnID, systemPrompt, userText string,
) ChatReply {
	command := strings.TrimSpace(commandRaw)
	if command == "" {
		return ChatReply{OK: false, Message: "还没有配置外部 Agent 的命令。"}
	}

	sessionID := ""
	if sessionKey != "" {
		sessionID = storedSessionID(engine, sessionKey)
	}
	if engine == nativePi && sessionID == "" {
		sessionID = newUUID()
		if sessionKey != "" {
			rememberSession(engine, sessionKey, sessionID)
		}
	}

	argv := splitCommand(command)
	if len(argv) == 0 {
		return ChatReply{OK: false, Message: "还没有配置外部 Agent 的命令。"}
	}

	promptText := userText
	if engine == nativeCodex && systemPrompt != "" {
		promptText = systemPrompt + "\n\n" + userText
	}

	switch engine {
	case nativePi:
		argv = append(argv, "-p", "--mode", "json", "--no-tools", "--no-themes")
		if sessionID != "" {
			argv = append(argv, "--session-id", sessionID)
		}
		if systemPrompt != "" {
			argv = append(argv, "--append-system-prompt", systemPrompt)
		}
		argv = append(argv, promptText)
	case nativeCodex:
		argv = append(argv, "exec")
		if sessionID != "" {
			argv = append(argv, "resume", sessionID)
		}
		argv = append(argv, "--json", "--skip-git-repo-check", "-c", `sandbox_mode="read-only"`)
		argv = append(argv, promptText)
	}

	_ = ensureDir(cwd)
	cmd := newNativeCmd(argv, cwd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return spawnError(engine)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return spawnError(engine)
	}
	if err := cmd.Start(); err != nil {
		return spawnError(engine)
	}

	run := &nativeRun{cmd: cmd, done: make(chan struct{})}
	defer close(run.done)
	nativeRegister(run, turnID)
	defer nativeUnregister(run)

	go drainNativeStderr(stderr, run)

	win := mygo.CallerWindow(ctx)
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-run.done:
				return
			}
		}
	}()

	var text strings.Builder
	timedOut := false
	deadline := time.NewTimer(promptTimeout)
	defer deadline.Stop()

loop:
	for !run.settled {
		select {
		case line, ok := <-lines:
			if !ok {
				break loop
			}
			if engine == nativePi {
				handlePiLine(run, win, turnID, &text, line)
			} else {
				handleCodexLine(run, win, turnID, &text, line)
			}
		case <-deadline.C:
			timedOut = true
			killGroup(cmd, syscall.SIGKILL)
			break loop
		}
	}

	// 一轮结束后等进程自己退出；不退就杀掉。
	waitCh := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
	case <-time.After(exitGrace):
		killGroup(cmd, syscall.SIGKILL)
		<-waitCh
	}

	if engine == nativeCodex && run.threadID != "" && sessionKey != "" {
		rememberSession(engine, sessionKey, run.threadID)
	}

	if run.isCancelled() {
		return stoppedReply(text.String())
	}
	if timedOut {
		return ChatReply{OK: false, Message: nativeName(engine) + " 长时间没有响应，已经停止它。"}
	}
	// Codex 的 resume 失败（线程已被删掉）时，清掉记录，下次开新线程，
	// 免得每轮提问都卡在同一个失效的会话上。
	if engine == nativeCodex && sessionID != "" && sessionKey != "" && run.threadID == "" {
		forgetSession(engine, sessionKey)
		if run.failed {
			return ChatReply{
				OK:      false,
				Message: failureMessage(engine, run) + "\n（上次的 Codex 会话已经失效，下一次提问会开新会话。）",
			}
		}
		return ChatReply{OK: false, Message: "上次的 Codex 会话已经找不到了，下一次提问会开新会话。"}
	}
	if run.failed {
		return ChatReply{OK: false, Message: failureMessage(engine, run)}
	}

	answer := strings.TrimSpace(text.String())
	if answer == "" {
		return ChatReply{OK: false, Message: nativeName(engine) + " 没有返回文字，请再试一次。"}
	}
	return ChatReply{OK: true, Text: answer}
}

// nativeCheck 是设置页的「检测连接」：跑一次 --version，确认 CLI 能启动。
func nativeCheck(ctx context.Context, engine nativeEngine, commandRaw, cwd string) AgentTest {
	command := strings.TrimSpace(commandRaw)
	if command == "" {
		return AgentTest{OK: false, Message: "还没有配置外部 Agent 的命令。"}
	}
	argv := append(splitCommand(command), "--version")
	_ = ensureDir(cwd)
	cmd := newNativeCmd(argv, cwd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return AgentTest{OK: false, Message: "启动失败。请确认命令在终端里能直接运行。"}
	}
	if err := cmd.Start(); err != nil {
		return AgentTest{OK: false, Message: "启动失败。请确认命令在终端里能直接运行。"}
	}

	type readResult struct {
		data []byte
	}
	done := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for len(buf) < 64*1024 {
			n, err := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- readResult{data: buf}
	}()

	var out []byte
	select {
	case res := <-done:
		out = res.data
	case <-time.After(10 * time.Second):
		killGroup(cmd, syscall.SIGKILL)
	}

	waitCh := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		killGroup(cmd, syscall.SIGKILL)
		<-waitCh
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		return AgentTest{OK: true, Name: nativeName(engine), Message: ""}
	}
	firstLine := version
	if i := strings.IndexByte(firstLine, '\n'); i >= 0 {
		firstLine = firstLine[:i]
	}
	if len(firstLine) > 200 {
		firstLine = firstLine[:200]
	}
	return AgentTest{OK: true, Name: nativeName(engine), Message: strings.TrimSpace(firstLine)}
}

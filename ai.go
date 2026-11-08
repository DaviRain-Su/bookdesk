package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// Ai 把角色对话和查词交给外部 Agent（Pi / Codex 原生协议或 ACP），
// 不装 Agent 时退回内置的 Anthropic Messages API。
//
// API Key 只放在 macOS 钥匙串里（mygo Secrets），绝不写进数据库或仓库。
const (
	keyName             = "anthropic-api-key"
	settingModel        = "ai.model"
	settingBaseURL      = "ai.baseUrl"
	settingEngine       = "ai.engine"
	settingAgentCommand = "ai.agentCommand"
	settingAgentCwd     = "ai.agentCwd"
	defaultModel        = "claude-opus-5-5"
	defaultBaseURL      = "https://api.anthropic.com"
	defaultEngine       = "acp"
	defaultAgentCommand = "pi"
	apiVersion          = "2023-06-01"
)

// 上下文只取「已读范围」：当前可见页 + 之前章节里与人物相关的片段。
const (
	maxSnippets     = 4
	snippetRadius   = 220
	maxSnippetBytes = 2400
	maxVisibleBytes = 3000
)

// Ai 是服务类型。
type Ai struct{}

func readKey() *string {
	raw, err := mygo.Secrets.Get(keyName)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func readSetting(key, fallback string) string {
	stored, err := getSetting(key)
	if err != nil || stored == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*stored)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func missingKeyReply() ChatReply {
	return ChatReply{
		OK:      false,
		Message: "还没有配置 API Key。点右上角的设置按钮，把 Anthropic API Key 存进系统钥匙串。",
	}
}

func (Ai) Status(ctx context.Context) (AiStatus, error) {
	running := acpRunningName()
	status := AiStatus{
		HasKey:       readKey() != nil,
		Model:        readSetting(settingModel, defaultModel),
		BaseURL:      readSetting(settingBaseURL, defaultBaseURL),
		Engine:       readSetting(settingEngine, defaultEngine),
		AgentCommand: readSetting(settingAgentCommand, defaultAgentCommand),
		AgentRunning: running != "",
		AgentName:    running,
	}
	return status, nil
}

func (Ai) SetKey(ctx context.Context, key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		_ = mygo.Secrets.Delete(keyName)
		return nil
	}
	return mygo.Secrets.Set(keyName, []byte(trimmed))
}

func (Ai) ClearKey(ctx context.Context) error {
	_ = mygo.Secrets.Delete(keyName)
	return nil
}

func (Ai) SetConfig(ctx context.Context, model, baseURL, engine, agentCommand string) error {
	model = strings.TrimSpace(model)
	base := strings.TrimSpace(baseURL)
	engine = strings.TrimSpace(engine)
	agentCommand = strings.TrimSpace(agentCommand)

	if model == "" {
		if err := deleteSetting(settingModel); err != nil {
			return err
		}
	} else if err := setSetting(settingModel, model); err != nil {
		return err
	}
	if base == "" {
		if err := deleteSetting(settingBaseURL); err != nil {
			return err
		}
	} else if err := setSetting(settingBaseURL, strings.TrimRight(base, "/")); err != nil {
		return err
	}
	if engine == "" || engine == defaultEngine {
		if err := deleteSetting(settingEngine); err != nil {
			return err
		}
	} else if err := setSetting(settingEngine, engine); err != nil {
		return err
	}
	if agentCommand == "" {
		return deleteSetting(settingAgentCommand)
	}
	return setSetting(settingAgentCommand, agentCommand)
}

func agentConfig() acpConfig {
	command := readSetting(settingAgentCommand, defaultAgentCommand)
	cwd := readSetting(settingAgentCwd, "")
	if cwd == "" {
		cwd = userDataDir + "/agent"
	}
	return acpConfig{command: command, cwd: cwd}
}

// TestAgent 是设置页的「检测」：按当前设置确认 Agent 能启动。
func (Ai) TestAgent(ctx context.Context) (AgentTest, error) {
	config := agentConfig()
	if engine, ok := nativeEngineForCommand(config.command); ok {
		return nativeCheck(ctx, engine, config.command, config.cwd), nil
	}
	return acpStart(ctx, config), nil
}

// DetectAgents 是设置页的 Agent 列表：检测本机装了哪些已知 ACP Agent。
func (Ai) DetectAgents(ctx context.Context) ([]AgentCandidate, error) {
	return detectAgents(), nil
}

func (Ai) StopAgent(ctx context.Context) error {
	acpStop()
	return nil
}

func (Ai) Cancel(ctx context.Context, turnID string) error {
	nativeCancel(turnID)
	acpCancel(turnID)
	return nil
}

// ForgetSession 清除某个角色（会话键）的对话记忆。
func (Ai) ForgetSession(ctx context.Context, sessionKey string) error {
	acpForgetSession(sessionKey)
	return nil
}

// buildContext 只拼「已读范围」：当前可见页，加上之前章节里提到这个人物的片段。
// allowFuture 关闭时，绝不读取当前 spine 之后的任何章节。
func buildContext(input ChatInput) string {
	var out strings.Builder
	fmt.Fprintf(&out, "书名：《%s》\n", input.BookTitle)
	if input.ChapterTitle != nil {
		t := strings.TrimSpace(*input.ChapterTitle)
		if t != "" {
			fmt.Fprintf(&out, "读者当前所在章节：%s\n", t)
		}
	}

	var rows []ChapterRangeRow
	var err error
	if input.AllowFuture {
		rows, err = allChapters(input.BookID)
	} else {
		rows, err = chaptersBefore(input.BookID, input.CurrentSpineIndex)
	}
	if err == nil {
		snippetCount := 0
		snippetBytes := 0
		for _, row := range rows {
			if snippetCount >= maxSnippets || snippetBytes >= maxSnippetBytes {
				break
			}
			at := indexOfIgnoreCase(row.Text, input.CharacterName)
			if at < 0 {
				continue
			}
			rawStart := 0
			if at > snippetRadius {
				rawStart = at - snippetRadius
			}
			rawEnd := len(row.Text)
			if at+len(input.CharacterName)+snippetRadius < rawEnd {
				rawEnd = at + len(input.CharacterName) + snippetRadius
			}
			start := floorCharBoundary(row.Text, rawStart)
			end := ceilCharBoundary(row.Text, rawEnd)
			prefix := ""
			if start > 0 {
				prefix = "…"
			}
			suffix := ""
			if end < len(row.Text) {
				suffix = "…"
			}
			section := row.SpineIndex + 1
			if section < 1 {
				section = 1
			}
			fmt.Fprintf(&out, "\n【第 %d 节】%s%s%s\n", section, prefix, row.Text[start:end], suffix)
			snippetCount++
			snippetBytes += end - start
		}
	}

	visible := strings.TrimSpace(input.VisibleText)
	if visible != "" {
		cut := ceilCharBoundary(visible, min(len(visible), maxVisibleBytes))
		out.WriteString("\n【读者正在读的这一页】\n")
		out.WriteString(visible[:cut])
		out.WriteString("\n")
	}
	return out.String()
}

func characterSystemPrompt(input ChatInput) string {
	var out strings.Builder
	fmt.Fprintf(&out, `你正在扮演《%s》里的人物「%s」，和一位正在读这本书的读者对话。

规则：
- 只用这个人物的口吻和视角说话，不要跳出角色。
- 只依据下面给出的书中片段和读者正在读的这一页来回答。片段里没有的内容，就说你不知道或记不清，不要编造情节。
- 不要提到「片段」「上下文」「设定」这些词，也不要复述规则本身。
- 用中文回答，简短自然，一般不超过 150 字。`, input.BookTitle, input.CharacterName)
	if input.CharacterDescription != nil {
		d := strings.TrimSpace(*input.CharacterDescription)
		if d != "" {
			fmt.Fprintf(&out, "\n\n读者为这个人物写下的设定：%s", d)
		}
	}
	return out.String()
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}

type anthropicRequest struct {
	Model        string                `json:"model"`
	MaxTokens    int64                 `json:"max_tokens"`
	System       string                `json:"system"`
	Messages     []anthropicMessage    `json:"messages"`
	OutputConfig anthropicOutputConfig `json:"output_config"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var anthropicClient = &http.Client{Timeout: 5 * time.Minute}

func postMessages(baseURL, key, model, system, user string, maxTokens int64) ChatReply {
	url := strings.TrimRight(baseURL, "/") + "/v1/messages"
	body, err := json.Marshal(anthropicRequest{
		Model:        model,
		MaxTokens:    maxTokens,
		System:       system,
		Messages:     []anthropicMessage{{Role: "user", Content: user}},
		OutputConfig: anthropicOutputConfig{Effort: "low"},
	})
	if err != nil {
		return ChatReply{OK: false, Message: "准备请求失败。"}
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatReply{OK: false, Message: "连不上模型服务，请检查网络或设置里的 API 地址。"}
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := anthropicClient.Do(req)
	if err != nil {
		return ChatReply{OK: false, Message: "连不上模型服务，请检查网络或设置里的 API 地址。"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return ChatReply{OK: false, Message: "模型服务返回了无法解析的内容。"}
	}

	var payload anthropicResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		if resp.StatusCode != http.StatusOK {
			return ChatReply{OK: false, Message: fmt.Sprintf("模型服务返回错误（HTTP %d）。", resp.StatusCode)}
		}
		return ChatReply{OK: false, Message: "模型服务返回了无法解析的内容。"}
	}

	if resp.StatusCode != http.StatusOK {
		if payload.Error != nil && payload.Error.Message != "" {
			return ChatReply{OK: false, Message: payload.Error.Message}
		}
		return ChatReply{OK: false, Message: fmt.Sprintf("模型服务返回错误（HTTP %d）。", resp.StatusCode)}
	}

	var text strings.Builder
	for _, block := range payload.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	answer := strings.TrimSpace(text.String())
	if answer == "" {
		if payload.StopReason == "refusal" {
			return ChatReply{OK: false, Message: "模型拒绝回答这个问题。"}
		}
		return ChatReply{OK: false, Message: "模型没有返回文字，请再试一次。"}
	}
	return ChatReply{OK: true, Text: answer}
}

func (Ai) Chat(ctx context.Context, input ChatInput) (ChatReply, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" {
		return ChatReply{OK: false, Message: "先写点什么再问吧。"}, nil
	}
	if strings.TrimSpace(input.CharacterName) == "" {
		return ChatReply{OK: false, Message: "这张人物卡还没有名字。"}, nil
	}

	contextText := buildContext(input)
	system := characterSystemPrompt(input)
	user := contextText + "\n\n读者问：" + question

	engine := readSetting(settingEngine, defaultEngine)
	if engine == "acp" {
		sessionKey := input.SessionKey
		if sessionKey == "" {
			sessionKey = input.BookID + ":" + input.CharacterName
		}
		config := agentConfig()
		config.persistSession = true
		if nativeEngine, ok := nativeEngineForCommand(config.command); ok {
			return nativePrompt(ctx, nativeEngine, config.command, config.cwd, sessionKey, input.TurnID, system, user), nil
		}
		return acpPrompt(ctx, config, sessionKey, input.TurnID, system, user), nil
	}

	key := readKey()
	if key == nil {
		return missingKeyReply(), nil
	}
	model := readSetting(settingModel, defaultModel)
	baseURL := readSetting(settingBaseURL, defaultBaseURL)
	return postMessages(baseURL, *key, model, system, user, 4000), nil
}

const wordLookupSystem = `你是一位英语词典助手。用户会给你一个英文单词或短语，以及它所在的句子。
请用简洁的中文给出：
1. 音标（英式或美式均可）
2. 词性与中文释义（结合句子语境，最多 3 条）
3. 一句话说明它在这个句子里的意思
不要展开无关内容，不要输出 Markdown 标题。`

func (Ai) LookupWord(ctx context.Context, input WordLookupInput) (ChatReply, error) {
	word := strings.TrimSpace(input.Word)
	if word == "" {
		return ChatReply{OK: false, Message: "没有选中要查的词。"}, nil
	}

	user := "单词：" + word
	if input.Sentence != nil {
		s := strings.TrimSpace(*input.Sentence)
		if s != "" {
			cut := ceilCharBoundary(s, min(len(s), 600))
			user = "单词：" + word + "\n所在句子：" + s[:cut]
		}
	}

	engine := readSetting(settingEngine, defaultEngine)
	if engine == "acp" {
		config := agentConfig()
		if nativeEngine, ok := nativeEngineForCommand(config.command); ok {
			// 查词不保存会话：每次都是独立的一问一答。
			return nativePrompt(ctx, nativeEngine, config.command, config.cwd, "", input.TurnID, wordLookupSystem, user), nil
		}
		return acpPrompt(ctx, config, "word-lookup", input.TurnID, wordLookupSystem, user), nil
	}

	key := readKey()
	if key == nil {
		return missingKeyReply(), nil
	}
	model := readSetting(settingModel, defaultModel)
	baseURL := readSetting(settingBaseURL, defaultBaseURL)
	return postMessages(baseURL, *key, model, wordLookupSystem, user, 2000), nil
}

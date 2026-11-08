import { useCallback, useEffect, useState } from "react";
import { KeyRound, Plug, X } from "lucide-react";
import { Ai } from "../mygo";

interface Props {
  open: boolean;
  onClose: () => void;
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

interface AgentCandidate {
  id: string;
  name: string;
  command: string;
  state: string;
  detail: string;
}

const normalizeCommand = (command: string) => command.trim().replace(/\s+/g, " ");

// Pi 和 Codex 直接用自己的 CLI，每次提问启动一次，没有常驻进程。
const isNativeCommand = (command: string) => /^(pi|codex)(\s|$)/.test(command.trim());

export default function SettingsPanel({ open, onClose }: Props) {
  const [engine, setEngine] = useState("acp");
  const [agentCommand, setAgentCommand] = useState("");
  const [agentRunning, setAgentRunning] = useState(false);
  const [agentName, setAgentName] = useState("");
  const [agents, setAgents] = useState<AgentCandidate[]>([]);
  const [agentsLoading, setAgentsLoading] = useState(true);
  const [hasKey, setHasKey] = useState(false);
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [keyInput, setKeyInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const status = await Ai.status();
      setEngine(status.engine === "builtin" ? "builtin" : "acp");
      setAgentCommand(status.agentCommand);
      setAgentRunning(status.agentRunning);
      setAgentName(status.agentName);
      setHasKey(status.hasKey);
      setModel(status.model);
      setBaseUrl(status.baseUrl);
    } catch (err) {
      setError(`读取设置失败：${errorText(err)}`);
    }
  }, []);

  const refreshAgents = useCallback(async () => {
    setAgentsLoading(true);
    try {
      setAgents(await Ai.detectAgents());
    } catch (err) {
      setError(`检测本机 Agent 失败：${errorText(err)}`);
    } finally {
      setAgentsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return;
    setKeyInput("");
    setNotice("");
    setError("");
    void refresh();
    void refreshAgents();
  }, [open, refresh, refreshAgents]);

  const save = async () => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const key = keyInput.trim();
      if (key) await Ai.setKey(key);
      await Ai.setConfig(model.trim(), baseUrl.trim(), engine, agentCommand.trim());
      setKeyInput("");
      await refresh();
      setNotice("已保存。");
    } catch (err) {
      setError(`保存失败：${errorText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const clearKey = async () => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await Ai.clearKey();
      await refresh();
      setNotice("已从系统钥匙串删除 API Key。");
    } catch (err) {
      setError(`删除失败：${errorText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const pickAgent = async (candidate: AgentCandidate) => {
    if (!candidate.command || busy || testing) return;
    setEngine("acp");
    setAgentCommand(candidate.command);
    setBusy(true);
    setError("");
    setNotice("");
    try {
      // 点选即生效：切换 Agent 不用再点保存。
      await Ai.setConfig(model.trim(), baseUrl.trim(), "acp", candidate.command);
      await refresh();
      setNotice(`已切换到 ${candidate.name}，下次提问时生效。`);
    } catch (err) {
      setError(`切换失败：${errorText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const testAgent = async () => {
    setTesting(true);
    setError("");
    setNotice("");
    try {
      const result = await Ai.testAgent();
      if (result.ok) {
        setNotice(`已连上外部 Agent${result.name ? `：${result.name}` : ""}。`);
      } else {
        setError(result.message);
      }
      await refresh();
    } catch (err) {
      setError(`检测失败：${errorText(err)}`);
    } finally {
      setTesting(false);
    }
  };

  const stopAgent = async () => {
    setBusy(true);
    try {
      await Ai.stopAgent();
      await refresh();
      setNotice("已停止外部 Agent。");
    } catch (err) {
      setError(`停止失败：${errorText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div
        className={`drawer-backdrop ${open ? "is-open" : ""}`}
        onClick={onClose}
        aria-hidden
      />
      <aside className={`drawer settings-drawer ${open ? "is-open" : ""}`} aria-hidden={!open}>
        <header className="drawer-head">
          <div>
            <h2>设置</h2>
            <span className="drawer-sub">AI 只做配角，Key 只存在本机</span>
          </div>
          <div className="drawer-actions">
            <button className="icon-button" onClick={onClose} title="关闭">
              <X size={17} />
            </button>
          </div>
        </header>

        <div className="drawer-body settings-body">
          <section className="settings-section">
            <div className="settings-label">
              <span>AI 引擎</span>
            </div>
            <div className="settings-seg">
              <button
                className={engine === "acp" ? "is-active" : ""}
                onClick={() => setEngine("acp")}
              >
                外部 Agent（ACP）
              </button>
              <button
                className={engine === "builtin" ? "is-active" : ""}
                onClick={() => setEngine("builtin")}
              >
                内置 API
              </button>
            </div>
            <p className="settings-hint">
              角色对话和查词交给本机已安装的 Agent：Pi、Codex 用它们自己的 CLI，
              其他 Agent 走 ACP（Agent Client Protocol）。内置 API 是不装 Agent 时的回退。
            </p>
          </section>

          {engine === "acp" ? (
            <section className="settings-section">
              <div className="settings-label">
                <Plug size={14} />
                <span>外部 Agent</span>
                <span className={`settings-badge ${agentRunning || isNativeCommand(agentCommand) ? "is-on" : ""}`}>
                  {isNativeCommand(agentCommand)
                    ? "按需启动"
                    : agentRunning
                      ? `运行中${agentName ? `：${agentName}` : ""}`
                      : "未启动"}
                </span>
              </div>
              {agentsLoading ? (
                <p className="settings-hint">正在检测本机安装的 Agent…</p>
              ) : (
                <div className="agent-list">
                  {agents.map((agent) => {
                    const selectable = agent.command.length > 0;
                    const isSelected =
                      selectable &&
                      normalizeCommand(agent.command) === normalizeCommand(agentCommand);
                    return (
                      <button
                        key={agent.id}
                        className={`agent-row ${isSelected ? "is-selected" : ""} ${
                          selectable ? "" : "is-missing"
                        }`}
                        disabled={!selectable || busy || testing}
                        onClick={() => void pickAgent(agent)}
                        title={selectable ? agent.command : agent.detail}
                      >
                        <span className="agent-dot" />
                        <span className="agent-text">
                          <span className="agent-name">{agent.name}</span>
                          <span className="agent-cmd">{agent.command || agent.detail}</span>
                        </span>
                        <span className={`agent-state is-${agent.state}`}>
                          {agent.state === "installed"
                            ? "已安装"
                            : agent.state === "adapter"
                              ? "需适配器"
                              : agent.state === "download"
                                ? "需下载"
                                : "未安装"}
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
              <p className="settings-hint">
                点一个就能切换，不用输命令。列表来自本机检测：PATH 里有可执行文件的直接可用。
                Pi 和 Codex 直接用它们自己的 CLI（原生协议，按需启动，不需要适配器）；
                其他 Agent 走 ACP，标「需适配器」表示 CLI 已装、由 npx 拉取官方适配器，
                标「需下载」的也是第一次运行时由 npx 拉取。没有列出来的 Agent 可以在下面填自定义命令。
              </p>
              <input
                className="settings-input"
                value={agentCommand}
                placeholder="自定义命令，例如 npx -y @agentclientprotocol/claude-agent-acp"
                spellCheck={false}
                onChange={(event) => setAgentCommand(event.target.value)}
              />
              <p className="settings-hint">
                自定义命令在登录 shell 里执行，所以 npx、agent 这些命令能找到；改完点保存。
                Agent 的工作目录是数据目录下空的 agent/，它拿不到你的书文件。
              </p>
              <div className="settings-inline-actions">
                <button className="text-button" onClick={() => void testAgent()} disabled={testing || busy}>
                  {testing ? "正在连接…" : "检测连接"}
                </button>
                <button
                  className="text-button"
                  onClick={() => void refreshAgents()}
                  disabled={agentsLoading || busy}
                >
                  {agentsLoading ? "检测中…" : "重新检测"}
                </button>
                {agentRunning && !isNativeCommand(agentCommand) ? (
                  <button className="text-button is-danger-text" onClick={() => void stopAgent()} disabled={busy}>
                    停止 Agent
                  </button>
                ) : null}
              </div>
              <p className="settings-hint">
                Agent 第一次运行前要先在它自己的终端里登录（例如先运行一次 <code>pi</code>）。
                Agent 的 Key 由它自己管理，不经过这个应用。
              </p>
            </section>
          ) : null}

          {engine === "builtin" ? (
            <>
              <section className="settings-section">
                <div className="settings-label">
                  <KeyRound size={14} />
                  <span>Anthropic API Key</span>
                  <span className={`settings-badge ${hasKey ? "is-on" : ""}`}>
                    {hasKey ? "已保存" : "未配置"}
                  </span>
                </div>
                <input
                  className="settings-input"
                  type="password"
                  value={keyInput}
                  placeholder={hasKey ? "已存在系统钥匙串，输入新的可替换" : "sk-ant-…"}
                  spellCheck={false}
                  autoComplete="off"
                  onChange={(event) => setKeyInput(event.target.value)}
                />
                <p className="settings-hint">
                  Key 用 myzig 的 Secrets 存进 macOS 钥匙串，不写进数据库，也不进仓库。
                </p>
                {hasKey ? (
                  <button className="text-button is-danger-text" onClick={() => void clearKey()} disabled={busy}>
                    从钥匙串删除 Key
                  </button>
                ) : null}
              </section>

              <section className="settings-section">
                <div className="settings-label">
                  <span>模型</span>
                </div>
                <input
                  className="settings-input"
                  value={model}
                  placeholder="claude-opus-5-5"
                  spellCheck={false}
                  onChange={(event) => setModel(event.target.value)}
                />
                <p className="settings-hint">默认 claude-opus-5-5。用别的模型时填完整模型 ID。</p>
              </section>

              <section className="settings-section">
                <div className="settings-label">
                  <span>API 地址</span>
                </div>
                <input
                  className="settings-input"
                  value={baseUrl}
                  placeholder="https://api.anthropic.com"
                  spellCheck={false}
                  onChange={(event) => setBaseUrl(event.target.value)}
                />
                <p className="settings-hint">留空即用官方地址。请求从本机直接发出，不经过任何中转服务。</p>
              </section>
            </>
          ) : null}

          <section className="settings-section">
            <div className="settings-label">
              <span>发送范围</span>
            </div>
            <p className="settings-hint is-strong">
              对话只发送你当前这一页的文字，以及之前章节里提到这个人物的片段。
              「允许后续章节」默认关闭；关闭时，你还没读到的内容不会发给 Agent。
            </p>
          </section>

          {notice ? <div className="settings-notice">{notice}</div> : null}
          {error ? <div className="settings-notice is-error">{error}</div> : null}

          <div className="settings-actions">
            <button className="primary-button" onClick={() => void save()} disabled={busy}>
              保存
            </button>
          </div>
        </div>
      </aside>
    </>
  );
}

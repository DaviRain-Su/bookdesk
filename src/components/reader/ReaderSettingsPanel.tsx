import { Minus, Plus, RotateCcw, X } from "lucide-react";
import type { ReadingSettings } from "../../mygo";
import {
  COLUMN_WIDTH_OPTIONS,
  DEFAULT_READER_SETTINGS,
  MAX_FONT_SIZE,
  MAX_LINE_HEIGHT,
  MIN_FONT_SIZE,
  MIN_LINE_HEIGHT,
  READER_THEMES,
  clampFontSize,
  clampLineHeight,
  looksLikeClassic,
} from "./settings";

interface Props {
  settings: ReadingSettings;
  bookTitle: string;
  onChange: (patch: Partial<ReadingSettings>) => void;
  onClose: () => void;
}

function lineHeightLabel(value: number): string {
  return String(Math.round(value * 100) / 100);
}

/** 全屏阅读的「Aa」面板：主题、字号、行距、栏宽、横竖排、繁简、禅意、双页。 */
export default function ReaderSettingsPanel({
  settings,
  bookTitle,
  onChange,
  onClose,
}: Props) {
  const classic = looksLikeClassic(bookTitle);

  return (
    <>
      <div className="reader-settings-backdrop" onClick={onClose} />
      <div className="reader-settings" role="dialog" aria-label="阅读设置">
        <div className="reader-settings-head">
          <span>阅读设置</span>
          <button className="ghost-icon" onClick={onClose} title="关闭">
            <X size={14} />
          </button>
        </div>

        <div className="reader-settings-body">
          <div className="rs-row">
            <span className="rs-label">主题</span>
            <div className="rs-themes">
              {READER_THEMES.map((theme) => (
                <button
                  key={theme.id}
                  className={`rs-theme ${settings.theme === theme.id ? "is-active" : ""}`}
                  onClick={() => onChange({ theme: theme.id })}
                  title={theme.label}
                >
                  <span
                    className="rs-swatch"
                    style={{ background: theme.swatch, color: theme.swatchInk }}
                  >
                    文
                  </span>
                  <span>{theme.label}</span>
                </button>
              ))}
            </div>
          </div>

          <div className="rs-row">
            <span className="rs-label">字号</span>
            <div className="rs-stepper">
              <button
                onClick={() => onChange({ fontSize: clampFontSize(settings.fontSize - 1) })}
                disabled={settings.fontSize <= MIN_FONT_SIZE}
                title="小一号"
              >
                <Minus size={13} />
              </button>
              <span className="rs-value">{settings.fontSize}</span>
              <button
                onClick={() => onChange({ fontSize: clampFontSize(settings.fontSize + 1) })}
                disabled={settings.fontSize >= MAX_FONT_SIZE}
                title="大一号"
              >
                <Plus size={13} />
              </button>
            </div>
          </div>

          <div className="rs-row">
            <span className="rs-label">行距</span>
            <div className="rs-stepper">
              <button
                onClick={() => onChange({ lineHeight: clampLineHeight(settings.lineHeight - 0.05) })}
                disabled={settings.lineHeight <= MIN_LINE_HEIGHT}
                title="收紧行距"
              >
                <Minus size={13} />
              </button>
              <span className="rs-value">{lineHeightLabel(settings.lineHeight)}</span>
              <button
                onClick={() => onChange({ lineHeight: clampLineHeight(settings.lineHeight + 0.05) })}
                disabled={settings.lineHeight >= MAX_LINE_HEIGHT}
                title="放宽行距"
              >
                <Plus size={13} />
              </button>
            </div>
          </div>

          <div className="rs-row">
            <span className="rs-label">栏宽</span>
            <div className="rs-segment">
              {COLUMN_WIDTH_OPTIONS.map((option) => (
                <button
                  key={option.value}
                  className={settings.columnWidth === option.value ? "is-active" : ""}
                  onClick={() => onChange({ columnWidth: option.value })}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>

          <div className="rs-row">
            <span className="rs-label">排版</span>
            <div className="rs-segment">
              <button
                className={settings.vertical ? "" : "is-active"}
                onClick={() => onChange({ vertical: false })}
              >
                横排
              </button>
              <button
                className={settings.vertical ? "is-active" : ""}
                onClick={() => onChange({ vertical: true })}
              >
                竖排
              </button>
            </div>
          </div>

          <div className="rs-row">
            <span className="rs-label">显示</span>
            <div className="rs-segment">
              <button
                className={settings.convert === "none" ? "is-active" : ""}
                onClick={() => onChange({ convert: "none" })}
              >
                原文
              </button>
              <button
                className={settings.convert === "simplified" ? "is-active" : ""}
                onClick={() => onChange({ convert: "simplified" })}
              >
                简体
              </button>
              <button
                className={settings.convert === "traditional" ? "is-active" : ""}
                onClick={() => onChange({ convert: "traditional" })}
              >
                繁体
              </button>
            </div>
          </div>

          <div className="rs-row">
            <span className="rs-label">双页</span>
            <button
              className={`rs-toggle ${settings.spread && !settings.vertical ? "is-on" : ""}`}
              onClick={() => onChange({ spread: !settings.spread })}
              disabled={settings.vertical}
              aria-pressed={settings.spread && !settings.vertical}
              title={settings.vertical ? "竖排时不分页" : "双页对开"}
            >
              <span />
            </button>
          </div>

          <div className="rs-row">
            <span className="rs-label">禅意</span>
            <button
              className={`rs-toggle ${settings.zen ? "is-on" : ""}`}
              onClick={() => onChange({ zen: !settings.zen })}
              aria-pressed={settings.zen}
              title="隐藏顶栏和底栏"
            >
              <span />
            </button>
          </div>

          {classic ? (
            <div className="rs-hint">
              <span>这是中文古籍，可以试试竖排。</span>
              {settings.vertical ? null : (
                <button onClick={() => onChange({ vertical: true })}>切换竖排</button>
              )}
            </div>
          ) : null}
        </div>

        <div className="reader-settings-foot">
          <button className="rs-reset" onClick={() => onChange(DEFAULT_READER_SETTINGS)}>
            <RotateCcw size={13} />
            恢复默认
          </button>
        </div>
      </div>
    </>
  );
}

import {
  BookA,
  Highlighter,
  Languages,
  Lightbulb,
  Quote,
  Sparkles,
  Trash2,
  UserRound,
} from "lucide-react";
import {
  isEnglishSelection,
  type ReaderAction,
  type ReaderSelection,
} from "./useEpubReader";

interface Props {
  selection: ReaderSelection;
  onAction: (action: ReaderAction) => void;
}

/**
 * 书卡和全屏阅读共用的划选浮层。
 * 两边如果各写一份，按钮迟早会走样；动作本身仍由 useEpubReader 执行。
 */
export default function SelectionMenu({ selection, onAction }: Props) {
  const aiButtons = (
    <>
      <button onClick={() => onAction("ask")}>
        <Sparkles size={13} />
        问 AI
      </button>
      <button onClick={() => onAction("explain")}>
        <Lightbulb size={13} />
        解释
      </button>
      <button onClick={() => onAction("translate")}>
        <Languages size={13} />
        翻译
      </button>
    </>
  );

  return (
    <div
      className={`selection-popover ${selection.flip ? "is-flipped" : ""}`}
      style={{ left: selection.x, top: selection.y }}
      onMouseDown={(event) => event.preventDefault()}
    >
      {selection.existingId ? (
        <>
          <button onClick={() => onAction("card")}>
            <Quote size={13} />
            生成卡片
          </button>
          {aiButtons}
          <button className="is-danger" onClick={() => onAction("remove")}>
            <Trash2 size={13} />
            删除划线
          </button>
        </>
      ) : (
        <>
          <button onClick={() => onAction("highlight")}>
            <Highlighter size={13} />
            划线
          </button>
          <button onClick={() => onAction("card")}>
            <Quote size={13} />
            生成卡片
          </button>
          <button onClick={() => onAction("character")}>
            <UserRound size={13} />
            人物
          </button>
          {isEnglishSelection(selection.text) ? (
            <button onClick={() => onAction("lookup")}>
              <BookA size={13} />
              查词
            </button>
          ) : null}
          {aiButtons}
        </>
      )}
    </div>
  );
}

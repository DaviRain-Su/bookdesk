import { BookPlus, Plus, X } from "lucide-react";

export interface LibraryBook {
  id: string;
  title: string;
  author: string | null;
  coverRelPath: string | null;
}

interface Props {
  open: boolean;
  books: LibraryBook[];
  onClose: () => void;
  onAdd: (book: LibraryBook) => void;
  onImport: () => void;
}

export function coverSrc(book: LibraryBook): string | null {
  return book.coverRelPath ? `books://localhost/cover/${book.id}` : null;
}

export default function LibraryDrawer({ open, books, onClose, onAdd, onImport }: Props) {
  return (
    <>
      <div
        className={`drawer-backdrop ${open ? "is-open" : ""}`}
        onClick={onClose}
        aria-hidden
      />
      <aside className={`drawer ${open ? "is-open" : ""}`} aria-hidden={!open}>
        <header className="drawer-head">
          <div>
            <h2>书架</h2>
            <span className="drawer-sub">{books.length} 本书</span>
          </div>
          <div className="drawer-actions">
            <button className="pill-button" onClick={onImport}>
              <BookPlus size={15} />
              导入 EPUB
            </button>
            <button className="icon-button" onClick={onClose} title="关闭">
              <X size={17} />
            </button>
          </div>
        </header>

        <div className="drawer-body">
          {books.length === 0 ? (
            <div className="drawer-empty">
              <p>书架还是空的。</p>
              <p className="drawer-empty-hint">导入你自己拥有或有权使用的 EPUB 电子书。</p>
              <button className="primary-button" onClick={onImport}>
                <BookPlus size={16} />
                导入 EPUB
              </button>
            </div>
          ) : (
            books.map((book) => {
              const src = coverSrc(book);
              return (
                <button key={book.id} className="library-item" onClick={() => onAdd(book)}>
                  <div className="library-cover">
                    {src ? (
                      <img src={src} alt="" draggable={false} />
                    ) : (
                      <span className="library-cover-fallback">{book.title.slice(0, 1)}</span>
                    )}
                  </div>
                  <div className="library-meta">
                    <span className="library-title">{book.title}</span>
                    <span className="library-author">{book.author || "未知作者"}</span>
                  </div>
                  <span className="library-add" title="放到画布">
                    <Plus size={16} />
                  </span>
                </button>
              );
            })
          )}
        </div>

        <footer className="drawer-foot">
          只导入你购买的、有权使用的、公版的，或自己制作的 EPUB。带 DRM 的书无法导入。
        </footer>
      </aside>
    </>
  );
}

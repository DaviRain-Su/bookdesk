package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// Library 负责导入、读取和 books:// 协议。
type Library struct{}

var (
	userDataDir string
	booksDir    string
	imagesDir   string
)

func initPaths() error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	userDataDir = dir
	booksDir = filepath.Join(dir, "books")
	imagesDir = filepath.Join(dir, "images")
	if err := ensureDir(booksDir); err != nil {
		return err
	}
	return ensureDir(imagesDir)
}

func emitProgress(win *mygo.Window, done, total int64, title string) {
	if win == nil {
		return
	}
	_ = importProgressEvent.Emit(win, ImportProgress{Done: done, Total: total, Title: title})
}

func (Library) PickAndImport(ctx context.Context) (ImportResult, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   mygo.CallerWindow(ctx),
		Title:    "选择 EPUB 电子书",
		Filters:  []mygo.FileFilter{{Name: "EPUB 电子书", Extensions: []string{"epub"}}},
		Multiple: true,
	})
	if err != nil {
		return ImportResult{}, err
	}
	if len(paths) == 0 {
		return ImportResult{Books: []Book{}, Errors: []ImportError{}}, nil
	}
	return importPaths(ctx, paths)
}

func (Library) ImportPaths(ctx context.Context, paths []string) (ImportResult, error) {
	return importPaths(ctx, paths)
}

func importPaths(ctx context.Context, paths []string) (ImportResult, error) {
	return importPathsFor(mygo.CallerWindow(ctx), paths)
}

func importPathsFor(win *mygo.Window, paths []string) (ImportResult, error) {
	books := make([]Book, 0, len(paths))
	errs := make([]ImportError, 0)
	total := int64(len(paths))
	for i, path := range paths {
		emitProgress(win, int64(i), total, baseName(path))
		book, err := importOne(path)
		if err != nil {
			errs = append(errs, ImportError{Path: path, Message: importErrorMessage(err)})
			continue
		}
		books = append(books, book)
		emitProgress(win, int64(i+1), total, book.Title)
	}
	return ImportResult{Books: books, Errors: errs}, nil
}

func importErrorMessage(err error) string {
	switch err {
	case errEncrypted:
		return "不支持加密的电子书（DRM）"
	case errNotEpub:
		return "不是有效的 EPUB 文件"
	case errMissingContainer, errMissingOPF:
		return "EPUB 结构不完整，找不到书籍内容"
	case errBadPackage:
		return "EPUB 内容无法解析"
	}
	if os.IsNotExist(err) {
		return "文件不存在"
	}
	if os.IsPermission(err) {
		return "没有权限读取该文件"
	}
	return "导入失败"
}

func importOne(path string) (Book, error) {
	if extensionLower(path) != ".epub" {
		return Book{}, errNotEpub
	}

	id := newID("book")
	bookDir := filepath.Join(booksDir, id)
	if err := ensureDir(bookDir); err != nil {
		return Book{}, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(bookDir)
		}
	}()

	destEpub := filepath.Join(bookDir, "book.epub")
	if err := copyFile(path, destEpub); err != nil {
		return Book{}, err
	}

	parsed, err := parseEpub(destEpub)
	if err != nil {
		return Book{}, err
	}

	var coverRel *string
	if parsed.CoverBytes != nil {
		ext := parsed.CoverExt
		if ext == "" {
			ext = "jpg"
		}
		coverName := "cover." + ext
		if err := writeFile(filepath.Join(bookDir, coverName), parsed.CoverBytes); err != nil {
			return Book{}, err
		}
		rel := fmt.Sprintf("books/%s/%s", id, coverName)
		coverRel = &rel
	}

	book := Book{
		ID:           id,
		Title:        parsed.Title,
		Author:       parsed.Author,
		Language:     parsed.Language,
		CoverRelPath: coverRel,
		EpubRelPath:  fmt.Sprintf("books/%s/book.epub", id),
		ImportedAt:   nowSeconds(),
	}
	if err := insertBook(book); err != nil {
		return Book{}, err
	}

	chapters := make([]ChapterRow, len(parsed.Chapters))
	for i, ch := range parsed.Chapters {
		chapters[i] = ChapterRow{SpineIndex: ch.SpineIndex, Href: ch.Href, Text: ch.Text}
	}
	if err := insertChapters(book.ID, chapters); err != nil {
		return Book{}, err
	}
	ok = true
	return book, nil
}

// textIndexVersion 是正文索引的版本。2 起目录页、封面页、扉页不再
// 进搜索索引；旧库在启动时按这个版本重解析一次。
const textIndexVersion = "2"

func migrateTextIndex() {
	current, err := getSetting("text.version")
	if err == nil && current != nil && *current == textIndexVersion {
		return
	}
	books, err := listBooks()
	if err != nil {
		return
	}
	for _, book := range books {
		parsed, err := parseEpub(filepath.Join(booksDir, book.ID, "book.epub"))
		if err != nil {
			continue
		}
		chapters := make([]ChapterRow, len(parsed.Chapters))
		for i, ch := range parsed.Chapters {
			chapters[i] = ChapterRow{SpineIndex: ch.SpineIndex, Href: ch.Href, Text: ch.Text}
		}
		if len(chapters) == 0 {
			continue
		}
		_ = insertChapters(book.ID, chapters)
	}
	_ = setSetting("text.version", textIndexVersion)
}

func (Library) List(ctx context.Context) ([]Book, error) {
	return listBooks()
}

func (Library) ChapterText(ctx context.Context, bookID string, spineIndex int64) (*ChapterText, error) {
	return chapterText(bookID, spineIndex)
}

func (Library) EpubURL(ctx context.Context, bookID string) (string, error) {
	return "books://localhost/epub/" + bookID, nil
}

func (Library) EpubBase64(ctx context.Context, bookID string) (string, error) {
	book, err := getBook(bookID)
	if err != nil {
		return "", err
	}
	if book == nil {
		return "", fmt.Errorf("book not found")
	}
	data, err := readFileLimit(filepath.Join(userDataDir, book.EpubRelPath), 256*1024*1024)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (Library) CoverURL(ctx context.Context, bookID string) (string, error) {
	book, err := getBook(bookID)
	if err != nil {
		return "", err
	}
	if book == nil || book.CoverRelPath == nil {
		return "", nil
	}
	return "books://localhost/cover/" + bookID, nil
}

func (Library) PickImage(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  mygo.CallerWindow(ctx),
		Title:   "选择图片",
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}},
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	path := paths[0]
	ext := extensionLower(path)
	if ext == "" {
		ext = ".png"
	}
	name := newID("image") + ext
	if err := copyFile(path, filepath.Join(imagesDir, name)); err != nil {
		return "", err
	}
	return "images/" + name, nil
}

// ---------- books:// 协议 ----------

func mimeFor(relPath string) string {
	switch extensionLower(relPath) {
	case ".epub":
		return "application/epub+zip"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	return "image/jpeg"
}

func respondFile(w http.ResponseWriter, relPath string) {
	data, err := readFileLimit(filepath.Join(userDataDir, relPath), 256*1024*1024)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mimeFor(relPath))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	_, _ = w.Write(data)
}

func handleBooksProtocol(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/epub/"):
		id := strings.TrimPrefix(p, "/epub/")
		book, err := getBook(id)
		if err != nil || book == nil {
			http.Error(w, "book not found", http.StatusNotFound)
			return
		}
		respondFile(w, book.EpubRelPath)
	case strings.HasPrefix(p, "/cover/"):
		id := strings.TrimPrefix(p, "/cover/")
		book, err := getBook(id)
		if err != nil || book == nil {
			http.Error(w, "book not found", http.StatusNotFound)
			return
		}
		if book.CoverRelPath == nil {
			http.Error(w, "no cover", http.StatusNotFound)
			return
		}
		respondFile(w, *book.CoverRelPath)
	case strings.HasPrefix(p, "/image/"):
		name := strings.TrimPrefix(p, "/image/")
		if strings.Contains(name, "/") || name == "" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		respondFile(w, "images/"+name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

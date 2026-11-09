package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestImportFolderDedupes(t *testing.T) {
	dir := t.TempDir()
	resetTestDB()
	userDataDir = dir
	booksDir = filepath.Join(dir, "books")
	imagesDir = filepath.Join(dir, "images")
	if err := ensureDir(booksDir); err != nil {
		t.Fatal(err)
	}
	if err := openDB(); err != nil {
		t.Fatalf("openDB: %v", err)
	}

	folder := filepath.Join(t.TempDir(), "书库")
	if err := os.MkdirAll(filepath.Join(folder, "子目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(folder, ".隐藏目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := writeTestEpub(t)
	if err := copyFile(src, filepath.Join(folder, "a.epub")); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, filepath.Join(folder, "子目录", "b.EPUB")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"说明.txt", ".hidden.epub", ".隐藏目录/书.epub", "0-invalid.epub"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("不是书"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(folder, "子目录"), filepath.Join(folder, "目录链接")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, filepath.Join(folder, "链接.epub")); err != nil {
		t.Fatal(err)
	}

	result, err := importFolderFor(nil, folder)
	if err != nil {
		t.Fatalf("importFolderFor: %v", err)
	}
	if result.Scanned != 3 {
		t.Fatalf("scanned = %d, want 3", result.Scanned)
	}
	if len(result.Books) != 1 || result.Skipped != 1 || len(result.Errors) != 1 {
		t.Fatalf("first import = %+v", result)
	}
	if filepath.Base(result.Errors[0].Path) != "0-invalid.epub" {
		t.Fatalf("unexpected failed file: %+v", result.Errors[0])
	}
	books, err := listBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("books = %d, want 1", len(books))
	}

	again, err := importFolderFor(nil, folder)
	if err != nil {
		t.Fatalf("importFolderFor again: %v", err)
	}
	if len(again.Books) != 0 || again.Skipped != 2 || len(again.Errors) != 1 {
		t.Fatalf("second import = %+v", again)
	}
	if books, _ := listBooks(); len(books) != 1 {
		t.Fatalf("books after second import = %d, want 1", len(books))
	}
	entries, err := os.ReadDir(booksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("book directories after duplicate and failed imports = %d, want 1", len(entries))
	}
}

func TestCollectEpubFilesLimit(t *testing.T) {
	folder := t.TempDir()
	for i := 0; i <= maxFolderEpubs; i++ {
		name := filepath.Join(folder, fmt.Sprintf("%04d.epub", i))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := collectEpubFiles(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != maxFolderEpubs {
		t.Fatalf("files = %d, want %d", len(files), maxFolderEpubs)
	}
}

func TestCollectEpubFilesRejectsInvalidRoot(t *testing.T) {
	folder := t.TempDir()
	file := filepath.Join(folder, "书.epub")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{file, filepath.Join(folder, "不存在")} {
		if _, err := collectEpubFiles(root); err == nil {
			t.Fatalf("collectEpubFiles(%q) succeeded, want error", root)
		}
	}
}

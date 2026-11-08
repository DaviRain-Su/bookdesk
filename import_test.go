package main

import (
	"path/filepath"
	"testing"
)

func TestImportOne(t *testing.T) {
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
	book, err := importOne(writeTestEpub(t))
	if err != nil {
		t.Fatalf("importOne: %v", err)
	}
	if book.Title != "迁移测试书" || book.CoverRelPath == nil {
		t.Errorf("book = %+v", book)
	}
	books, err := listBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("books = %d", len(books))
	}
	ch, err := chapterText(book.ID, 1)
	if err != nil || ch == nil {
		t.Fatalf("chapterText: %v %v", ch, err)
	}
	if ch.Text == "" {
		t.Error("empty chapter text")
	}
	hits, err := searchChapters("量子纠缠", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Errorf("search hits = %d", len(hits))
	}
}

package main

import (
	"context"
	"path/filepath"
	"testing"
)

// 书架收藏和画布文件夹是后加的两列，写入后列表里要能读出来，清空后回到未分组。
func TestBookCollectionsAndBoardFolders(t *testing.T) {
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

	ctx := context.Background()
	if err := insertBook(Book{
		ID:          "book-a",
		Title:       "论语",
		EpubRelPath: "books/book-a/book.epub",
		ImportedAt:  nowSeconds(),
	}); err != nil {
		t.Fatalf("insertBook: %v", err)
	}

	if err := (Library{}).SetCollection(ctx, "book-a", "  古籍  "); err != nil {
		t.Fatalf("SetCollection: %v", err)
	}
	books, err := (Library{}).List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(books) != 1 || books[0].Collection == nil || *books[0].Collection != "古籍" {
		t.Fatalf("collection not saved: %+v", books)
	}

	if err := (Library{}).SetCollection(ctx, "book-a", ""); err != nil {
		t.Fatalf("clear collection: %v", err)
	}
	books, _ = (Library{}).List(ctx)
	if len(books) != 1 || books[0].Collection != nil {
		t.Fatalf("collection not cleared: %+v", books)
	}

	first, err := (Board{}).Create(ctx, "画布 A")
	if err != nil {
		t.Fatalf("Create board: %v", err)
	}
	if _, err := (Board{}).Create(ctx, "画布 B"); err != nil {
		t.Fatalf("Create board: %v", err)
	}

	if err := (Board{}).SetFolder(ctx, first.ID, "学习"); err != nil {
		t.Fatalf("SetFolder: %v", err)
	}
	boards, err := (Board{}).List(ctx)
	if err != nil {
		t.Fatalf("List boards: %v", err)
	}
	var found *BoardSummary
	for i := range boards {
		if boards[i].ID == first.ID {
			found = &boards[i]
		}
	}
	if found == nil || found.Folder == nil || *found.Folder != "学习" {
		t.Fatalf("folder not saved: %+v", boards)
	}

	if err := (Board{}).SetFolder(ctx, first.ID, ""); err != nil {
		t.Fatalf("clear folder: %v", err)
	}
	boards, _ = (Board{}).List(ctx)
	for _, b := range boards {
		if b.ID == first.ID && b.Folder != nil {
			t.Fatalf("folder not cleared: %+v", b)
		}
	}
}

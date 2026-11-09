package main

import (
	"context"
	"path/filepath"
	"testing"
)

// 五色划线：颜色写在已有的 highlights.color 字段里，改色后列表能读出来。
func TestHighlightColorRoundTrip(t *testing.T) {
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

	if err := insertBook(Book{
		ID:          "book-note",
		Title:       "划线测试",
		EpubRelPath: "books/book-note/book.epub",
		ImportedAt:  nowSeconds(),
	}); err != nil {
		t.Fatalf("insertBook: %v", err)
	}

	cfi := "epubcfi(/6/4!/4/2)"
	green := "green"
	ctx := context.Background()
	h, err := (Notes{}).SaveHighlight(ctx, SaveHighlightInput{
		BookID: "book-note",
		Location: BookLocation{
			BookID:     "book-note",
			CFI:        &cfi,
			SpineIndex: 1,
		},
		Text:  "一句话",
		Color: &green,
	})
	if err != nil {
		t.Fatalf("SaveHighlight: %v", err)
	}
	if h.Color != "green" {
		t.Fatalf("color = %q, want green", h.Color)
	}

	if err := (Notes{}).SetHighlightColor(ctx, h.ID, "blue"); err != nil {
		t.Fatalf("SetHighlightColor: %v", err)
	}
	list, err := (Notes{}).ListHighlights(ctx, nil)
	if err != nil {
		t.Fatalf("ListHighlights: %v", err)
	}
	if len(list) != 1 || list[0].Color != "blue" {
		t.Fatalf("highlights = %+v", list)
	}
}

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// 划选问 AI 的发送范围：关闭「允许后续章节」时，只能带当前页和之前的章节。
func TestSelectionContextStaysWithinReadRange(t *testing.T) {
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
		ID:          "book-ask",
		Title:       "测试书",
		EpubRelPath: "books/book-ask/book.epub",
		ImportedAt:  nowSeconds(),
	}); err != nil {
		t.Fatalf("insertBook: %v", err)
	}
	if err := insertChapters("book-ask", []ChapterRow{
		{SpineIndex: 0, Href: "ch1.xhtml", Text: "第一章里有一句话：同一句话出现在这里。"},
		{SpineIndex: 1, Href: "ch2.xhtml", Text: "第二章是读者正在读的这一页。"},
		{SpineIndex: 2, Href: "ch3.xhtml", Text: "第三章里又出现了同一句话。"},
	}); err != nil {
		t.Fatalf("insertChapters: %v", err)
	}

	base := SelectionAskInput{
		BookID:            "book-ask",
		BookTitle:         "测试书",
		Mode:              "explain",
		Selection:         "同一句话",
		VisibleText:       "第二章是读者正在读的这一页。",
		CurrentSpineIndex: 1,
	}

	closed := buildSelectionContext(base)
	if !strings.Contains(closed, "【读者选中的文字】") || !strings.Contains(closed, "同一句话") {
		t.Fatalf("selection missing from context: %s", closed)
	}
	if !strings.Contains(closed, "第二章是读者正在读的这一页。") {
		t.Fatalf("visible page missing from context: %s", closed)
	}
	if !strings.Contains(closed, "第 1 节") {
		t.Fatalf("previous chapter snippet missing: %s", closed)
	}
	if strings.Contains(closed, "第 3 节") {
		t.Fatalf("allowFuture=false leaked a later chapter: %s", closed)
	}

	open := base
	open.AllowFuture = true
	future := buildSelectionContext(open)
	if !strings.Contains(future, "第 3 节") {
		t.Fatalf("allowFuture=true should include later chapters: %s", future)
	}
}

func TestSelectionSystemPromptPerMode(t *testing.T) {
	explain := selectionSystemPrompt("explain", "论语")
	if !strings.Contains(explain, "解释") {
		t.Fatalf("explain prompt missing instruction: %s", explain)
	}
	translate := selectionSystemPrompt("translate", "论语")
	if !strings.Contains(translate, "翻译") {
		t.Fatalf("translate prompt missing instruction: %s", translate)
	}
	ask := selectionSystemPrompt("ask", "论语")
	if !strings.Contains(ask, "《论语》") {
		t.Fatalf("ask prompt missing book title: %s", ask)
	}
}

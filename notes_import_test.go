package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNotesMarkdown(t *testing.T) {
	content := "\ufeff# 示例书\n## 第一章\n> 第一段划线\n\n我的想法\n\n> 第二段划线\n## 第二章\n> 另一段\n"
	notes := parseNotesMarkdown(content)
	if len(notes) != 3 {
		t.Fatalf("notes = %d, want 3: %+v", len(notes), notes)
	}
	if notes[0].BookTitle != "示例书" || notes[0].Chapter != "第一章" {
		t.Fatalf("first = %+v", notes[0])
	}
	if notes[0].Quote != "第一段划线" || notes[0].Note != "我的想法" {
		t.Fatalf("first quote/note = %+v", notes[0])
	}
	if notes[1].Quote != "第二段划线" || notes[1].Note != "" {
		t.Fatalf("second = %+v", notes[1])
	}
	if notes[2].Chapter != "第二章" || notes[2].Quote != "另一段" {
		t.Fatalf("third = %+v", notes[2])
	}
}

func TestParseNotesCSV(t *testing.T) {
	content := "\ufeff书名,章节,划线,想法\n示例书,第一章,一句话,我的想法\n示例书,第二章,另一句话,\n"
	notes, err := parseNotesCSV(content)
	if err != nil {
		t.Fatalf("parseNotesCSV: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %d, want 2: %+v", len(notes), notes)
	}
	if notes[0].BookTitle != "示例书" || notes[0].Quote != "一句话" || notes[0].Note != "我的想法" {
		t.Fatalf("first = %+v", notes[0])
	}
}

func TestImportNotesMatchesBookAndDedupes(t *testing.T) {
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
		ID:          "book-notes",
		Title:       "示例书",
		EpubRelPath: "books/book-notes/book.epub",
		ImportedAt:  nowSeconds(),
	}); err != nil {
		t.Fatalf("insertBook: %v", err)
	}
	if err := insertChapters("book-notes", []ChapterRow{
		{SpineIndex: 1, Href: "ch1.xhtml", Text: "第一章\n这句话值得划线：慢一点，把书读完。"},
	}); err != nil {
		t.Fatalf("insertChapters: %v", err)
	}

	notePath := filepath.Join(dir, "notes.md")
	content := "# 示例书\n## 第一章\n> 慢一点，把书读完。\n我的想法\n> 慢一点，把书读完。\n"
	if err := os.WriteFile(notePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := importNoteFiles([]string{notePath})
	if err != nil {
		t.Fatalf("importNoteFiles: %v", err)
	}
	if result.Total != 2 || len(result.Notes) != 1 || result.Skipped != 1 {
		t.Fatalf("result = %+v", result)
	}
	note := result.Notes[0]
	if note.BookID == nil || *note.BookID != "book-notes" {
		t.Fatalf("bookId = %v", note.BookID)
	}
	if note.Location == nil || note.Location.SpineIndex != 1 {
		t.Fatalf("location = %+v", note.Location)
	}
	if note.Location.CharOffset == nil || *note.Location.CharOffset <= 0 {
		t.Fatalf("charOffset = %v", note.Location.CharOffset)
	}
}

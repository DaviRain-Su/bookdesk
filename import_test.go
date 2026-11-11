package main

import (
	"context"
	"fmt"
	"os"
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

func setupImportTestDB(t *testing.T) {
	t.Helper()
	resetTestDB()
	userDataDir = t.TempDir()
	booksDir = filepath.Join(userDataDir, "books")
	imagesDir = filepath.Join(userDataDir, "images")
	if err := ensureDir(booksDir); err != nil {
		t.Fatal(err)
	}
	if err := openDB(); err != nil {
		t.Fatal(err)
	}
}

func TestImportOneRollsBackOnIndexFailure(t *testing.T) {
	setupImportTestDB(t)
	ctx := context.Background()
	if _, err := database.Execute(ctx, `CREATE TRIGGER fail_chapter BEFORE INSERT ON book_text
WHEN NEW.spine_index = 2 BEGIN SELECT RAISE(ABORT, 'index failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := importOne(writeTestEpub(t)); err == nil {
		t.Fatal("import succeeded despite index failure")
	}
	for _, table := range []string{"books", "book_text", "book_text_fts"} {
		if table == "book_text_fts" && !ftsReady {
			continue
		}
		rows, err := database.Query(ctx, "SELECT COUNT(*) FROM "+table)
		if err != nil {
			t.Fatal(err)
		}
		if count := asInt(rows.Values[0][0]); count != 0 {
			t.Errorf("%s has %d orphaned rows", table, count)
		}
	}
	entries, err := os.ReadDir(booksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("failed import left %d book directories", len(entries))
	}
}

func TestReplaceChaptersIsAtomic(t *testing.T) {
	setupImportTestDB(t)
	ctx := context.Background()
	if err := insertChapters("book-atomic", []ChapterRow{{SpineIndex: 0, Href: "old.xhtml", Text: "原有正文"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Execute(ctx, `CREATE TRIGGER fail_chapter BEFORE INSERT ON book_text
WHEN NEW.spine_index = 79 BEGIN SELECT RAISE(ABORT, 'index failure'); END`); err != nil {
		t.Fatal(err)
	}
	chapters := make([]ChapterRow, 80)
	for i := range chapters {
		chapters[i] = ChapterRow{SpineIndex: int64(i), Href: fmt.Sprintf("%d.xhtml", i), Text: "新正文"}
	}
	if err := insertChapters("book-atomic", chapters); err == nil {
		t.Fatal("replacement succeeded despite index failure")
	}
	chapter, err := chapterText("book-atomic", 0)
	if err != nil || chapter == nil || chapter.Text != "原有正文" {
		t.Fatalf("original text was lost: %+v, %v", chapter, err)
	}
	hits, err := searchChapters("原有正文", nil)
	if err != nil || len(hits) != 1 {
		t.Fatalf("original search index was lost: %+v, %v", hits, err)
	}
	if _, err := database.Execute(ctx, "DROP TRIGGER fail_chapter"); err != nil {
		t.Fatal(err)
	}
	if err := insertChapters("book-atomic", nil); err != nil {
		t.Fatal(err)
	}
	chapter, err = chapterText("book-atomic", 0)
	if err != nil || chapter != nil {
		t.Fatalf("empty replacement kept stale text: %+v, %v", chapter, err)
	}
	hits, err = searchChapters("原有正文", nil)
	if err != nil || len(hits) != 0 {
		t.Fatalf("empty replacement kept stale index: %+v, %v", hits, err)
	}
}

func TestMigrateTextIndexRetriesFailures(t *testing.T) {
	for _, failure := range []string{"parse", "write"} {
		t.Run(failure, func(t *testing.T) {
			setupImportTestDB(t)
			source := writeTestEpub(t)
			book, err := importOne(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := setSetting("text.version", "old"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(booksDir, book.ID, "book.epub")
			if failure == "parse" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := database.Execute(context.Background(), `CREATE TRIGGER fail_chapter BEFORE INSERT ON book_text
BEGIN SELECT RAISE(ABORT, 'index failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			migrateTextIndex()
			version, err := getSetting("text.version")
			if err != nil || version == nil || *version != "old" {
				t.Fatalf("failed migration marked complete: %v, %v", version, err)
			}
			if failure == "parse" {
				if err := copyFile(source, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := database.Execute(context.Background(), "DROP TRIGGER fail_chapter"); err != nil {
					t.Fatal(err)
				}
			}
			migrateTextIndex()
			version, err = getSetting("text.version")
			if err != nil || version == nil || *version != textIndexVersion {
				t.Fatalf("migration did not retry: %v, %v", version, err)
			}
		})
	}
}

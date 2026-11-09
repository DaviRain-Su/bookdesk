package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func TestCollectShareItemsAndSelection(t *testing.T) {
	author := "曹雪芹"
	note := "这是私密的想法不要出现"
	otherNote := "另一本书的笔记"
	chapter := "第一回"
	book := Book{ID: "honglou", Title: "红楼梦", Author: &author, Language: strPtr("zh-CN")}
	other := "other"
	highlights := []Highlight{
		{ID: "h1", BookID: "honglou", Text: "满纸荒唐言", Color: "pink", CreatedAt: 2, Location: BookLocation{BookID: "honglou", PageLabel: &chapter}},
		{ID: "h-other", BookID: "other", Text: "不该出现的句子", CreatedAt: 1},
	}
	cards := []Card{
		{ID: "c1", Type: "text", Quote: strPtr("满纸荒唐言"), Body: strPtr(note), SourceBookID: &book.ID, CreatedAt: 3},
		{ID: "c2", Type: "text", Quote: strPtr("开谈不说红楼梦"), Body: strPtr("只在摘录卡里"), SourceBookID: &book.ID, CreatedAt: 4, QuoteLocation: &BookLocation{BookID: book.ID, PageLabel: strPtr("楔子")}},
		{ID: "c3", Type: "text", Quote: strPtr("别的书"), Body: strPtr(otherNote), SourceBookID: &other, CreatedAt: 5},
		{ID: "c4", Type: "text", Body: strPtr("没有引文的随手记"), SourceBookID: &book.ID, CreatedAt: 6},
	}
	items := collectShareItems(book, highlights, cards)
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].ID != "hl:h1" || items[0].Chapter != "第一回" || items[0].Note != note {
		t.Fatalf("highlight item = %+v", items[0])
	}
	if items[1].ID != "card:c2" || items[1].Chapter != "楔子" || !strings.Contains(items[1].Note, "摘录卡") {
		t.Fatalf("card item = %+v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.Quote, "不该出现") || strings.Contains(item.Note, "另一本书") || strings.Contains(item.Quote, "别的书") {
			t.Fatalf("foreign text in %+v", item)
		}
		if strings.Contains(item.Note, "随手记") {
			t.Fatalf("note without a quote was exported: %+v", item)
		}
	}

	none := exportPassages(items, nil)
	if len(none) != 0 {
		t.Fatalf("default selection = %+v", none)
	}
	all := selectAllShareChoices(items)
	if len(all) != 2 {
		t.Fatalf("select all = %+v", all)
	}
	for _, choice := range all {
		if choice.IncludeNote {
			t.Fatal("select all included a private note")
		}
	}
	passages := exportPassages(items, all)
	if len(passages) != 2 {
		t.Fatalf("passages = %+v", passages)
	}
	for _, passage := range passages {
		if passage.Note != "" {
			t.Fatalf("note included by default: %+v", passage)
		}
	}
	withNote := exportPassages(items, []BookExportChoice{{ID: "hl:h1", IncludeNote: true}})
	if len(withNote) != 1 || withNote[0].Note != note || withNote[0].Quote != "满纸荒唐言" {
		t.Fatalf("with note = %+v", withNote)
	}
	unknown := exportPassages(items, []BookExportChoice{{ID: "hl:missing", IncludeNote: true}, {ID: "card:c3", IncludeNote: true}})
	if len(unknown) != 0 {
		t.Fatalf("unknown ids exported: %+v", unknown)
	}
}

func TestExportHTMLAndMarkdownPrivacy(t *testing.T) {
	author := "Jane Austen"
	lang := "en"
	book := Book{ID: "pnp", Title: "Pride & Prejudice <script>", Author: &author, Language: &lang}
	items := []BookShareItem{
		{ID: "hl:1", Quote: "a truth universally acknowledged", Chapter: "Chapter 1", Note: "这是私密的想法不要出现", Color: "yellow"},
		{ID: "hl:2", Quote: "see https://example.com and <script>alert(1)</script>", Note: "第二条笔记", Color: "blue"},
	}
	when := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	emptyHTML, emptyMD := renderBookExport(book, items, nil, when)
	if strings.Contains(emptyHTML, "a truth") || strings.Contains(emptyHTML, "私密") || strings.Contains(emptyMD, "私密") || strings.Contains(emptyMD, "a truth") {
		t.Fatal("empty selection leaked a passage or a note")
	}
	if !strings.Contains(emptyHTML, "Pride") || !strings.Contains(emptyHTML, "Jane Austen") {
		t.Fatal("metadata missing from empty export")
	}
	assertNoExternalResources(t, emptyHTML)
	if strings.Contains(emptyHTML, "http://") || strings.Contains(emptyHTML, "https://") {
		t.Fatal("empty page contains a URL")
	}
	if strings.Contains(emptyHTML, "<script>") || !strings.Contains(emptyHTML, "&lt;script&gt;") {
		t.Fatalf("title was not escaped:\n%s", emptyHTML)
	}
	if !strings.Contains(emptyHTML, `lang="en"`) {
		t.Fatalf("lang missing:\n%s", emptyHTML[:400])
	}

	selectedHTML, selectedMD := renderBookExport(book, items, selectAllShareChoices(items), when)
	if !strings.Contains(selectedHTML, "a truth universally acknowledged") || !strings.Contains(selectedMD, "> a truth universally acknowledged") {
		t.Fatal("selected quote missing")
	}
	if strings.Contains(selectedHTML, "私密") || strings.Contains(selectedMD, "私密") || strings.Contains(selectedHTML, "第二条笔记") {
		t.Fatal("private note included without a tick")
	}
	assertNoExternalResources(t, selectedHTML)
	if strings.Contains(selectedHTML, "<script>") || strings.Contains(selectedHTML, "href=") {
		t.Fatal("quote became markup")
	}
	if !strings.Contains(selectedHTML, "https://example.com") {
		t.Fatal("quote text lost its URL")
	}
	if strings.Contains(selectedHTML, "src=") || strings.Contains(selectedMD, "](http") {
		t.Fatal("export turned text into a link or an image")
	}

	notedHTML, notedMD := renderBookExport(book, items, []BookExportChoice{{ID: "hl:1", IncludeNote: true}}, when)
	if !strings.Contains(notedHTML, "这是私密的想法不要出现") || !strings.Contains(notedMD, "这是私密的想法不要出现") {
		t.Fatal("ticked note missing")
	}
	if strings.Contains(notedHTML, "第二条笔记") || strings.Contains(notedHTML, "https://example.com") {
		t.Fatal("unticked passage leaked")
	}
	assertNoExternalResources(t, notedHTML)
}

func assertNoExternalResources(t *testing.T, doc string) {
	t.Helper()
	lower := strings.ToLower(doc)
	for _, bad := range []string{"<script", "<iframe", "<link", "<img", "<object", "<embed", "@import", "url(", "src=", "href=", "srcset="} {
		if strings.Contains(lower, bad) {
			t.Errorf("export contains %s", bad)
		}
	}
}

func TestPageLabelSurvivesAndOldDatabaseGainsTheColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bookdesk.db")
	raw, err := sqlite.Open(context.Background(), dbPath, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Execute(context.Background(), `CREATE TABLE highlights (
  id TEXT PRIMARY KEY,
  book_id TEXT NOT NULL,
  cfi TEXT,
  spine_index INTEGER NOT NULL DEFAULT 0,
  href TEXT,
  char_offset INTEGER,
  text TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT 'yellow',
  created_at INTEGER NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Execute(context.Background(),
		"INSERT INTO highlights (id, book_id, cfi, spine_index, text, color, created_at) VALUES ('hl-old', 'book-1', 'cfi-old', 1, '旧划线还在', 'green', 10)")
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	resetTestDB()
	userDataDir = dir
	booksDir = filepath.Join(dir, "books")
	imagesDir = filepath.Join(dir, "images")
	if err := openDB(); err != nil {
		t.Fatal(err)
	}
	list, err := listHighlights(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Text != "旧划线还在" || list[0].Color != "green" || list[0].Location.PageLabel != nil {
		t.Fatalf("old highlight = %+v", list)
	}

	chapter := "第一回"
	ctx := context.Background()
	if err := insertBook(Book{ID: "book-1", Title: "红楼梦", EpubRelPath: "books/book-1/book.epub", ImportedAt: 1}); err != nil {
		t.Fatal(err)
	}
	saved, err := (Notes{}).SaveHighlight(ctx, SaveHighlightInput{
		BookID: "book-1",
		Location: BookLocation{
			BookID:     "book-1",
			CFI:        strPtr("cfi-new"),
			SpineIndex: 2,
			PageLabel:  &chapter,
		},
		Text:  "满纸荒唐言",
		Color: strPtr("blue"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Location.PageLabel == nil || *saved.Location.PageLabel != "第一回" {
		t.Fatalf("saved label = %+v", saved.Location.PageLabel)
	}
	body := "只在勾选后出现"
	card, err := (Notes{}).SaveCard(ctx, SaveCardInput{
		Type:         "text",
		Quote:        strPtr("满纸荒唐言"),
		Body:         &body,
		SourceBookID: strPtr("book-1"),
		QuoteLocation: &BookLocation{
			BookID:    "book-1",
			PageLabel: &chapter,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.QuoteLocation == nil || card.QuoteLocation.PageLabel == nil || *card.QuoteLocation.PageLabel != "第一回" {
		t.Fatalf("card label = %+v", card.QuoteLocation)
	}
	loaded, err := (Share{}).ListBook(ctx, "book-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "红楼梦" || len(loaded.Items) != 2 {
		t.Fatalf("list = %+v", loaded)
	}
	var found BookShareItem
	for _, item := range loaded.Items {
		if item.Quote == "满纸荒唐言" {
			found = item
		}
	}
	if found.Chapter != "第一回" || found.Note != body {
		t.Fatalf("item = %+v", found)
	}
	doc, md := renderBookExport(Book{ID: loaded.BookID, Title: loaded.Title}, loaded.Items, nil, time.Unix(0, 0).UTC())
	if strings.Contains(doc, body) || strings.Contains(md, body) || strings.Contains(doc, "满纸荒唐言") {
		t.Fatal("default export included a passage or a note")
	}
	doc, md = renderBookExport(Book{ID: loaded.BookID, Title: loaded.Title}, loaded.Items, selectAllShareChoices(loaded.Items), time.Unix(0, 0).UTC())
	if !strings.Contains(doc, "满纸荒唐言") || strings.Contains(doc, body) || strings.Contains(md, body) {
		t.Fatal("select all did not keep the note private")
	}
	assertNoExternalResources(t, doc)
}

func strPtr(s string) *string { return &s }

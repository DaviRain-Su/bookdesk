package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestMarkdownNeutralizesActivePayloads(t *testing.T) {
	when := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	payloads := []string{
		`<script>alert(1)</script>`,
		`"><svg onload=alert(1)>`,
		`<style>@import url(http://evil.test/a.css)></style>`,
		`<a href="https://evil.test/x">x</a>`,
		`<iframe src="https://evil.test/f"></iframe>`,
		`![](https://evil.test/pixel.gif)`,
		`[click](https://evil.test/phish)`,
	}
	for _, payload := range payloads {
		book := Book{ID: "b", Title: payload, Author: strPtr(payload)}
		items := []BookShareItem{{
			ID:      "hl:1",
			Quote:   payload,
			Chapter: payload,
			Note:    payload,
			Color:   "yellow",
		}}
		_, md := renderBookExport(book, items, []BookExportChoice{{ID: "hl:1", IncludeNote: true}}, when)
		for _, raw := range []string{"<script", "<svg", "<style", "<a ", "<iframe", "![](", "](http", "](https", "url("} {
			if strings.Contains(md, raw) {
				t.Fatalf("payload %q left %q in markdown:\n%s", payload, raw, md)
			}
		}
	}

	book := Book{ID: "b", Title: "书名", Author: strPtr("# 作者\n---")}
	items := []BookShareItem{{
		ID:      "hl:1",
		Quote:   "# 不是标题\n---",
		Chapter: "---",
		Note:    "# secret\n---\n![](https://evil.test/pixel.gif)",
		Color:   "yellow",
	}}
	_, md := renderBookExport(book, items, []BookExportChoice{{ID: "hl:1", IncludeNote: true}}, when)
	rules, headings := 0, 0
	for _, line := range strings.Split(md, "\n") {
		if strings.TrimSpace(line) == "---" {
			rules++
		}
		if strings.HasPrefix(line, "#") {
			headings++
		}
	}
	if headings != 1 || rules != 2 {
		t.Fatalf("headings=%d rules=%d\n%s", headings, rules, md)
	}
	if strings.Contains(md, "![](") || strings.Contains(md, "<script") {
		t.Fatalf("note still active:\n%s", md)
	}
}

func TestHTMLEscapesNoteChapterAuthorAndNoReferrer(t *testing.T) {
	when := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	author := `<script>author</script>`
	book := Book{ID: "b", Title: "书", Author: &author}
	items := []BookShareItem{{
		ID:      "hl:1",
		Quote:   "引文",
		Chapter: `<iframe src="https://evil.test/f"></iframe>`,
		Note:    `<a href="https://evil.test/n">note</a>`,
		Color:   "blue",
	}}
	doc, _ := renderBookExport(book, items, []BookExportChoice{{ID: "hl:1", IncludeNote: true}}, when)
	if !strings.Contains(doc, `<meta name="referrer" content="no-referrer">`) {
		t.Fatal("missing no-referrer")
	}
	if strings.Contains(doc, "<script>") || strings.Contains(doc, "<iframe") || strings.Contains(doc, "<a ") {
		t.Fatalf("raw tag in html:\n%s", doc)
	}
	if !strings.Contains(doc, "&lt;script&gt;author") {
		t.Fatal("author was not escaped")
	}
	if !strings.Contains(doc, "&lt;iframe") {
		t.Fatal("chapter was not escaped")
	}
	if !strings.Contains(doc, "&lt;a href=") {
		t.Fatal("note was not escaped")
	}
}

func TestExportBaseNameAndSavePath(t *testing.T) {
	got := exportBaseName("书\u202e\u202a\u2066\u2069\u200b\u007f名")
	if got != "书名" {
		t.Fatalf("sanitized = %q", got)
	}
	if exportBaseName("\u202e\u200b\u007f") != "book" {
		t.Fatal("empty name should fall back")
	}
	want := strings.Repeat("e\u0301", 40)
	if exportBaseName(strings.Repeat("e\u0301", 41)) != want {
		t.Fatalf("grapheme truncation = %q", exportBaseName(strings.Repeat("e\u0301", 41)))
	}
	if utf8.RuneCountInString(want) != 80 {
		t.Fatal("test setup")
	}
	if utf8.RuneCountInString(exportBaseName(strings.Repeat("书", 45))) != 40 {
		t.Fatal("cjk truncation")
	}

	dir := t.TempDir()
	existing := filepath.Join(dir, "notes.html")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeSavePath(filepath.Join(dir, "notes"), "html"); err == nil {
		t.Fatal("appending the extension overwrote an existing file")
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing file changed: %q %v", data, err)
	}
	gotPath, err := finalizeSavePath(existing, "html")
	if err != nil || gotPath != existing {
		t.Fatalf("confirmed path = %q %v", gotPath, err)
	}
	fresh := filepath.Join(dir, "new")
	gotPath, err = finalizeSavePath(fresh, "HTML")
	if err != nil || gotPath != fresh+".HTML" && gotPath != fresh+".html" {
		t.Fatalf("fresh = %q %v", gotPath, err)
	}
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

func TestOldMainSchemaMigratesTwiceWithoutLosingCards(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bookdesk.db")
	raw, err := sqlite.Open(context.Background(), dbPath, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// 964ed37 跑完迁移之后的表：有收藏和画布文件夹，没有章节名列。
	statements := []string{
		`CREATE TABLE books (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  author TEXT,
  language TEXT,
  cover_rel_path TEXT,
  epub_rel_path TEXT NOT NULL,
  imported_at INTEGER NOT NULL,
  collection TEXT
)`,
		`CREATE TABLE book_text (
  book_id TEXT NOT NULL,
  spine_index INTEGER NOT NULL,
  href TEXT,
  text TEXT NOT NULL,
  PRIMARY KEY (book_id, spine_index)
)`,
		`CREATE TABLE highlights (
  id TEXT PRIMARY KEY,
  book_id TEXT NOT NULL,
  cfi TEXT,
  spine_index INTEGER NOT NULL DEFAULT 0,
  href TEXT,
  char_offset INTEGER,
  text TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT 'yellow',
  created_at INTEGER NOT NULL
)`,
		`CREATE TABLE cards (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  title TEXT,
  body TEXT,
  quote TEXT,
  quote_book_id TEXT,
  quote_cfi TEXT,
  quote_spine_index INTEGER,
  quote_href TEXT,
  quote_char_offset INTEGER,
  image_rel_path TEXT,
  source_book_id TEXT,
  character_name TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
)`,
		`CREATE TABLE whiteboards (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  viewport_json TEXT NOT NULL DEFAULT '{}',
  nodes_json TEXT NOT NULL DEFAULT '[]',
  edges_json TEXT NOT NULL DEFAULT '[]',
  updated_at INTEGER NOT NULL,
  folder TEXT
)`,
		`CREATE TABLE characters (
  id TEXT PRIMARY KEY,
  book_id TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  image_rel_path TEXT,
  created_at INTEGER NOT NULL
)`,
		`CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
)`,
		"CREATE INDEX idx_highlights_book ON highlights(book_id)",
		"CREATE INDEX idx_cards_source ON cards(source_book_id)",
		"CREATE INDEX idx_characters_book ON characters(book_id)",
		`INSERT INTO books (id, title, author, language, cover_rel_path, epub_rel_path, imported_at, collection)
VALUES ('b1', '旧书', '旧作者', 'zh', NULL, 'books/b1/book.epub', 3, '收藏')`,
		`INSERT INTO highlights (id, book_id, cfi, spine_index, href, char_offset, text, color, created_at)
VALUES ('hl-old', 'b1', 'cfi-old', 1, 'ch.html', 4, '旧划线还在', 'green', 10)`,
		`INSERT INTO cards (id, type, title, body, quote, quote_book_id, quote_cfi, quote_spine_index, quote_href, quote_char_offset, image_rel_path, source_book_id, character_name, created_at, updated_at)
VALUES ('card-old', 'text', '标题', '旧笔记还在', '旧摘录还在', 'b1', NULL, 1, NULL, NULL, NULL, 'b1', NULL, 11, 12)`,
	}
	for _, sql := range statements {
		if _, err := raw.Execute(context.Background(), sql); err != nil {
			t.Fatalf("old schema: %v\n%s", err, sql)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	check := func() {
		t.Helper()
		books, err := listBooks()
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 || books[0].Title != "旧书" || deref(books[0].Author) != "旧作者" || deref(books[0].Collection) != "收藏" {
			t.Fatalf("books = %+v", books)
		}
		highlights, err := listHighlights(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(highlights) != 1 || highlights[0].Text != "旧划线还在" || highlights[0].Color != "green" {
			t.Fatalf("highlights = %+v", highlights)
		}
		cards, err := listCards()
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) != 1 || deref(cards[0].Quote) != "旧摘录还在" || deref(cards[0].Body) != "旧笔记还在" || deref(cards[0].SourceBookID) != "b1" {
			t.Fatalf("cards = %+v", cards)
		}
	}
	reopen := func() {
		t.Helper()
		resetTestDB()
		userDataDir = dir
		booksDir = filepath.Join(dir, "books")
		imagesDir = filepath.Join(dir, "images")
		if err := openDB(); err != nil {
			t.Fatalf("openDB: %v", err)
		}
	}
	reopen()
	check()
	reopen()
	check()

	d, err := db()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(context.Background(), "UPDATE cards SET quote_page_label = ? WHERE id = ?", "楔子", "card-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(context.Background(), "UPDATE highlights SET page_label = ? WHERE id = ?", "第一回", "hl-old"); err != nil {
		t.Fatal(err)
	}
	cards, err := listCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].QuoteLocation == nil || deref(cards[0].QuoteLocation.PageLabel) != "楔子" {
		t.Fatalf("listCards did not read quote_page_label: %+v", cards)
	}
	highlights, err := listHighlights(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(highlights) != 1 || deref(highlights[0].Location.PageLabel) != "第一回" {
		t.Fatalf("listHighlights did not read page_label: %+v", highlights)
	}
}

func strPtr(s string) *string { return &s }

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestExportSelectionKeepsNotesPrivate(t *testing.T) {
	note := "这是不会公开的私记XYZ"
	quoteA := "庭有枇杷树，吾妻死之年所手植也。"
	quoteB := "今已亭亭如盖矣。"
	items := []exportCandidate{
		{ID: "h1", Kind: "highlight", Quote: quoteA, Note: note, Chapter: "正文", Color: "green", CreatedAt: 1},
		{ID: "h2", Kind: "highlight", Quote: quoteB, Note: "", Chapter: "", CreatedAt: 2},
		{ID: "c1", Kind: "card", Quote: "只在卡片上的一句。", Note: "卡片笔记也是私的", CreatedAt: 3},
	}
	meta := bookMeta{Title: "项脊轩志", Author: "归有光"}

	none, err := renderChoice(meta, items, ExportChoice{})
	if err != nil {
		t.Fatal(err)
	}
	if none.QuoteCount != 0 || none.NoteCount != 0 {
		t.Fatalf("empty selection counts = %d quotes, %d notes", none.QuoteCount, none.NoteCount)
	}
	for _, secret := range []string{quoteA, quoteB, note, "卡片笔记", "只在卡片上"} {
		if strings.Contains(none.HTML, secret) || strings.Contains(none.Markdown, secret) {
			t.Fatalf("unselected text %q leaked", secret)
		}
	}
	if !strings.Contains(none.HTML, "项脊轩志") || !strings.Contains(none.Markdown, "# 项脊轩志") {
		t.Fatal("book title missing from an empty page")
	}
	if !strings.Contains(none.HTML, "这一页没有放上划线") {
		t.Fatal("empty page should say nothing was selected")
	}

	quotesOnly := selectAllQuotes(items)
	if len(quotesOnly.NoteIDs) != 0 {
		t.Fatal("select all should not tick notes")
	}
	page, err := renderChoice(meta, items, quotesOnly)
	if err != nil {
		t.Fatal(err)
	}
	if page.QuoteCount != 3 || page.NoteCount != 0 {
		t.Fatalf("select all = %d quotes, %d notes", page.QuoteCount, page.NoteCount)
	}
	if strings.Contains(page.HTML, note) || strings.Contains(page.Markdown, note) || strings.Contains(page.HTML, "卡片笔记") {
		t.Fatal("select all included a private note")
	}
	if !strings.Contains(page.HTML, quoteA) || !strings.Contains(page.Markdown, quoteB) {
		t.Fatal("selected quotes missing")
	}
	if !strings.Contains(page.HTML, "正文") || !strings.Contains(page.Markdown, "正文") {
		t.Fatal("chapter missing")
	}

	withNote, err := renderChoice(meta, items, ExportChoice{QuoteIDs: []string{"h1"}, NoteIDs: []string{"h1"}})
	if err != nil {
		t.Fatal(err)
	}
	if withNote.NoteCount != 1 || !strings.Contains(withNote.HTML, note) || !strings.Contains(withNote.Markdown, note) {
		t.Fatal("ticked note was dropped")
	}
	if strings.Contains(withNote.HTML, quoteB) {
		t.Fatal("unticked quote was included")
	}

	// 只勾笔记、不勾划线，笔记仍然不能出现。
	noteOnly, err := renderChoice(meta, items, ExportChoice{NoteIDs: []string{"h1", "c1"}})
	if err != nil {
		t.Fatal(err)
	}
	if noteOnly.QuoteCount != 0 || strings.Contains(noteOnly.HTML, note) || strings.Contains(noteOnly.Markdown, "卡片笔记") {
		t.Fatal("a note tick without its quote still exported the note")
	}
}

func TestPublicHTMLHasNoExternalRequests(t *testing.T) {
	meta := bookMeta{Title: "本地的书", Author: "作者"}
	items := []exportedItem{{
		ID:    "h1",
		Quote: "正文里提到 https://example.com/book 和 <script src=\"https://evil.test/a.js\"></script>",
		Note:  "",
		Color: "blue",
	}}
	html, err := renderPublicHTML(meta, items)
	if err != nil {
		t.Fatal(err)
	}
	if refs := externalResourceRefs(html); len(refs) != 0 {
		t.Fatalf("external refs: %v", refs)
	}
	if strings.Contains(html, "<script") || strings.Contains(html, "<link") || strings.Contains(html, "<img") {
		t.Fatal("resource tag was not escaped")
	}
	if !strings.Contains(html, "https://example.com/book") {
		t.Fatal("a url inside the quote should remain readable text")
	}
	if !strings.Contains(html, "&lt;script") {
		t.Fatal("markup inside the quote should be escaped")
	}
	if !strings.Contains(html, "c-blue") {
		t.Fatal("highlight color class missing")
	}

	plain, err := renderPublicHTML(bookMeta{Title: "没有链接的书"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"http://", "https://", "url(", "@import", "<script", "<link"} {
		if strings.Contains(plain, needle) {
			t.Fatalf("plain page contains %q", needle)
		}
	}
	if !strings.Contains(plain, shareFooter) || !strings.Contains(renderPublicMarkdown(bookMeta{Title: "没有链接的书"}, nil), shareFooter) {
		t.Fatal("footer missing")
	}
}

func TestExportMergesHighlightWithCardNote(t *testing.T) {
	body := "合并过来的笔记"
	chapter := "卷一"
	cardChapter := "卷二"
	highlights := []Highlight{{
		ID:        "h1",
		BookID:    "b1",
		Text:      "同一段 文字",
		Color:     "pink",
		CreatedAt: 10,
		Chapter:   &chapter,
	}}
	cards := []Card{
		{
			ID:           "c-old",
			Type:         "text",
			Body:         strPtr("旧笔记"),
			Quote:        strPtr("同一段\n文字"),
			SourceBookID: strPtr("b1"),
			CreatedAt:    1,
			UpdatedAt:    1,
		},
		{
			ID:           "c-new",
			Type:         "text",
			Body:         strPtr(body),
			Quote:        strPtr("同一段 文字"),
			SourceBookID: strPtr("b1"),
			Chapter:      &cardChapter,
			CreatedAt:    2,
			UpdatedAt:    8,
		},
		{
			ID:           "c-other",
			Type:         "text",
			Body:         strPtr("别的书的笔记"),
			Quote:        strPtr("同一段 文字"),
			SourceBookID: strPtr("b2"),
			UpdatedAt:    99,
		},
		{
			ID:           "c-only",
			Type:         "text",
			Quote:        strPtr("只导入的摘录"),
			Body:         strPtr("导入时的想法"),
			SourceBookID: strPtr("b1"),
			CreatedAt:    4,
			UpdatedAt:    4,
		},
		{
			ID:           "c-image",
			Type:         "image",
			Quote:        strPtr("图片卡不该出现"),
			SourceBookID: strPtr("b1"),
		},
	}
	got := buildExportCandidates("b1", highlights, cards)
	if len(got) != 2 {
		t.Fatalf("candidates = %+v", got)
	}
	byID := map[string]exportCandidate{}
	for _, it := range got {
		byID[it.ID] = it
	}
	merged, ok := byID["h1"]
	if !ok || merged.Note != body || merged.Chapter != "卷一" || merged.Color != "pink" || merged.Kind != "highlight" {
		t.Fatalf("merged highlight = %+v", merged)
	}
	only, ok := byID["c-only"]
	if !ok || only.Kind != "card" || only.Note != "导入时的想法" {
		t.Fatalf("card-only = %+v", only)
	}
	page, err := renderChoice(bookMeta{Title: "书"}, got, ExportChoice{QuoteIDs: []string{"h1"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page.HTML, body) || strings.Contains(page.HTML, "别的书") || strings.Contains(page.HTML, "图片卡") {
		t.Fatal("private or foreign text leaked into the quote-only page")
	}
}

func TestChapterColumnAdditive(t *testing.T) {
	dir := t.TempDir()
	resetTestDB()
	userDataDir = dir
	booksDir = filepath.Join(dir, "books")
	imagesDir = filepath.Join(dir, "images")
	if err := ensureDir(booksDir); err != nil {
		t.Fatal(err)
	}
	if err := openDB(); err != nil {
		t.Fatal(err)
	}
	d, err := db()
	if err != nil {
		t.Fatal(err)
	}
	// 模拟升级前已经写在库里的划线：没有 chapter 这一列的值。
	if _, err := d.Execute(context.Background(),
		"INSERT INTO highlights (id, book_id, spine_index, text, color, created_at) VALUES ('old-hl', 'book-share', 0, '旧划线', 'yellow', 1)"); err != nil {
		t.Fatal(err)
	}
	author := "作者"
	if err := insertBook(Book{
		ID: "book-share", Title: "分享测试", Author: &author,
		EpubRelPath: "books/book-share/book.epub", ImportedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}

	list, err := listHighlights(strPtr("book-share"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Text != "旧划线" || list[0].Chapter != nil || list[0].Color != "yellow" {
		t.Fatalf("old highlight = %+v", list)
	}

	chapter := "第三章"
	ctx := context.Background()
	saved, err := (Notes{}).SaveHighlight(ctx, SaveHighlightInput{
		BookID:   "book-share",
		Location: BookLocation{BookID: "book-share", SpineIndex: 2},
		Text:     "新划线",
		Chapter:  &chapter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Chapter == nil || *saved.Chapter != "第三章" {
		t.Fatalf("saved chapter = %+v", saved.Chapter)
	}

	body := "不想公开"
	quote := "新划线"
	if _, err := (Notes{}).SaveCard(ctx, SaveCardInput{
		Type: "text", Body: &body, Quote: &quote, SourceBookID: strPtr("book-share"), Chapter: strPtr("第三章"),
	}); err != nil {
		t.Fatal(err)
	}

	book, err := (Share{}).ListBook(ctx, "book-share")
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "分享测试" || book.Author != "作者" || len(book.Items) != 2 {
		t.Fatalf("list = %+v", book)
	}
	rendered, err := (Share{}).RenderExport(ctx, "book-share", selectAllQuotes(buildExportCandidates("book-share", listHighlightsOrDie(t, "book-share"), listCardsOrDie(t))))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.HTML, "不想公开") || strings.Contains(rendered.Markdown, "不想公开") {
		t.Fatal("default-equivalent select all exported the note")
	}
	if refs := externalResourceRefs(rendered.HTML); len(refs) != 0 {
		t.Fatalf("refs: %v\n%s", refs, rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, "旧划线") || !strings.Contains(rendered.HTML, "新划线") || !strings.Contains(rendered.HTML, "第三章") {
		t.Fatal("selected quotes or chapter missing")
	}
	ticked, err := (Share{}).RenderExport(ctx, "book-share", ExportChoice{
		QuoteIDs: []string{saved.ID},
		NoteIDs:  []string{saved.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ticked.HTML, "不想公开") || !strings.Contains(ticked.Markdown, "不想公开") {
		t.Fatal("explicitly ticked note missing")
	}
	if strings.Contains(ticked.HTML, "旧划线") {
		t.Fatal("unticked old highlight was included")
	}
}

func listHighlightsOrDie(t *testing.T, bookID string) []Highlight {
	t.Helper()
	list, err := listHighlights(&bookID)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func listCardsOrDie(t *testing.T) []Card {
	t.Helper()
	list, err := listCards()
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestSafeFileNameAndPNGPayload(t *testing.T) {
	if got := safeFileName(" ../../etc/passwd ", "公开页"); got != "etcpasswd" {
		t.Fatalf("unsafe name %q", got)
	}
	if safeFileName("   ", "公开页") != "公开页" {
		t.Fatal("empty name should fall back")
	}
	if _, err := decodeBase64Payload("not base64!!!"); err == nil {
		t.Fatal("invalid payload accepted")
	}
	// 1x1 png
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	data, err := decodeBase64Payload("data:image/png;base64," + pngB64)
	if err != nil || len(data) < 8 {
		t.Fatalf("png payload: %v len %d", err, len(data))
	}
}

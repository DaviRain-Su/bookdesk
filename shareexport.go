package main

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type exportedPassage struct {
	Quote   string
	Chapter string
	Note    string
	Color   string
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func normalizeShareText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func cardBelongsToBook(card Card, bookID string) bool {
	if deref(card.SourceBookID) == bookID {
		return true
	}
	if card.QuoteLocation != nil && card.QuoteLocation.BookID == bookID {
		return true
	}
	return false
}

// collectShareItems 把一本书的划线和摘录卡收成可导出的条目。
// 别的书的卡片不会进来。和某条划线原文相同的卡片不另起一条，笔记挂到划线上。
// 没有对应划线的摘录卡仍然单独列出。
func collectShareItems(book Book, highlights []Highlight, cards []Card) []BookShareItem {
	type noteBag struct {
		text string
	}
	notesByQuote := map[string][]string{}
	var loose []Card
	for _, card := range cards {
		if card.Type == "image" || card.Type == "character" {
			continue
		}
		if !cardBelongsToBook(card, book.ID) {
			continue
		}
		quote := deref(card.Quote)
		if quote == "" {
			continue
		}
		key := normalizeShareText(quote)
		note := deref(card.Body)
		if note != "" {
			notesByQuote[key] = appendDistinct(notesByQuote[key], note)
		}
		loose = append(loose, card)
	}

	matched := map[string]bool{}
	items := make([]BookShareItem, 0, len(highlights)+len(loose))
	for _, h := range highlights {
		if h.BookID != book.ID {
			continue
		}
		quote := strings.TrimSpace(h.Text)
		if quote == "" {
			continue
		}
		key := normalizeShareText(quote)
		matched[key] = true
		items = append(items, BookShareItem{
			ID:        "hl:" + h.ID,
			Quote:     quote,
			Chapter:   deref(h.Location.PageLabel),
			Color:     fallbackColor(h.Color),
			Note:      strings.Join(notesByQuote[key], "\n\n"),
			CreatedAt: h.CreatedAt,
		})
	}
	seenLoose := map[string]bool{}
	for _, card := range loose {
		quote := deref(card.Quote)
		key := normalizeShareText(quote)
		if matched[key] || seenLoose[key] {
			continue
		}
		seenLoose[key] = true
		chapter := ""
		if card.QuoteLocation != nil {
			chapter = deref(card.QuoteLocation.PageLabel)
		}
		items = append(items, BookShareItem{
			ID:        "card:" + card.ID,
			Quote:     quote,
			Chapter:   chapter,
			Color:     "yellow",
			Note:      strings.Join(notesByQuote[key], "\n\n"),
			CreatedAt: card.CreatedAt,
		})
	}
	return items
}

func appendDistinct(list []string, note string) []string {
	for _, existing := range list {
		if existing == note {
			return list
		}
	}
	return append(list, note)
}

func fallbackColor(color string) string {
	switch strings.ToLower(strings.TrimSpace(color)) {
	case "green", "blue", "pink", "purple", "yellow":
		return strings.ToLower(strings.TrimSpace(color))
	default:
		return "yellow"
	}
}

func selectAllShareChoices(items []BookShareItem) []BookExportChoice {
	out := make([]BookExportChoice, 0, len(items))
	for _, item := range items {
		out = append(out, BookExportChoice{ID: item.ID, IncludeNote: false})
	}
	return out
}

func exportPassages(items []BookShareItem, choices []BookExportChoice) []exportedPassage {
	chosen := map[string]BookExportChoice{}
	for _, choice := range choices {
		if strings.TrimSpace(choice.ID) == "" {
			continue
		}
		chosen[choice.ID] = choice
	}
	out := make([]exportedPassage, 0, len(chosen))
	for _, item := range items {
		choice, ok := chosen[item.ID]
		if !ok {
			continue
		}
		passage := exportedPassage{
			Quote:   strings.TrimSpace(item.Quote),
			Chapter: strings.TrimSpace(item.Chapter),
			Color:   fallbackColor(item.Color),
		}
		if choice.IncludeNote {
			passage.Note = strings.TrimSpace(item.Note)
		}
		if passage.Quote == "" {
			continue
		}
		out = append(out, passage)
	}
	return out
}

func pageLang(book Book) string {
	lang := strings.ToLower(deref(book.Language))
	switch {
	case strings.HasPrefix(lang, "en"):
		return "en"
	case strings.HasPrefix(lang, "ja"):
		return "ja"
	case strings.HasPrefix(lang, "zh"):
		return "zh-Hans"
	default:
		return "zh-Hans"
	}
}

func exportAuthor(book Book) string {
	return deref(book.Author)
}

func renderShareHTML(book Book, passages []exportedPassage, when time.Time) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n")
	b.WriteString("<!-- exported by bookdesk / 书桌. local file. no trackers. -->\n")
	b.WriteString("<html lang=\"")
	b.WriteString(html.EscapeString(pageLang(book)))
	b.WriteString("\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<meta name=\"referrer\" content=\"no-referrer\">\n<title>")
	title := strings.TrimSpace(book.Title)
	if title == "" {
		title = "未命名书籍"
	}
	b.WriteString(html.EscapeString(title))
	b.WriteString(" — 书桌</title>\n<style>\n")
	b.WriteString(sharePageCSS)
	b.WriteString("\n</style>\n</head>\n<body>\n<article class=\"page\">\n")
	b.WriteString("<header class=\"book\">\n<p class=\"kicker\">书桌 · 阅读摘录</p>\n<h1>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</h1>\n")
	if author := exportAuthor(book); author != "" {
		b.WriteString("<p class=\"author\">")
		b.WriteString(html.EscapeString(author))
		b.WriteString("</p>\n")
	}
	b.WriteString("<p class=\"made\">本页由读者在自己的电脑上导出。没有账号，没有追踪，未勾选的笔记不会出现在这里。</p>\n")
	b.WriteString("<p class=\"when\">")
	b.WriteString(html.EscapeString(when.Format("2006年1月2日")))
	b.WriteString("</p>\n</header>\n")
	if len(passages) == 0 {
		b.WriteString("<p class=\"empty\">没有选择划线。书名和作者之外的内容都留在这台电脑上。</p>\n")
	} else {
		b.WriteString("<ol class=\"passages\">\n")
		for _, passage := range passages {
			b.WriteString("<li>\n<blockquote style=\"border-color:")
			b.WriteString(accentHex(passage.Color))
			b.WriteString("\">")
			b.WriteString(html.EscapeString(passage.Quote))
			b.WriteString("</blockquote>\n")
			if passage.Chapter != "" {
				b.WriteString("<p class=\"chapter\">")
				b.WriteString(html.EscapeString(passage.Chapter))
				b.WriteString("</p>\n")
			}
			if strings.TrimSpace(passage.Note) != "" {
				b.WriteString("<p class=\"note\">")
				b.WriteString(html.EscapeString(passage.Note))
				b.WriteString("</p>\n")
			}
			b.WriteString("</li>\n")
		}
		b.WriteString("</ol>\n")
	}
	b.WriteString("<footer><p>")
	b.WriteString(html.EscapeString(shareFooter))
	b.WriteString("</p></footer>\n</article>\n</body>\n</html>\n")
	return b.String()
}

const sharePageCSS = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  background: #f3efe6;
  color: #2a261f;
  font-family: "Songti SC", "STSong", "Noto Serif SC", "Source Han Serif SC", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", serif;
  line-height: 1.75;
}
.page { max-width: 40rem; margin: 0 auto; padding: 3.2rem 1.4rem 4rem; }
.kicker { margin: 0 0 0.6rem; letter-spacing: 0.14em; font-size: 0.78rem; color: #8a8172; }
h1 { margin: 0; font-size: 2rem; font-weight: 600; line-height: 1.35; }
.author { margin: 0.4rem 0 0; color: #5e584e; }
.made, .when, .empty { color: #7a7366; font-size: 0.92rem; }
.made { margin: 1.4rem 0 0; }
.when { margin: 0.2rem 0 0; }
.passages { list-style: none; margin: 2rem 0 0; padding: 0; }
.passages li { margin: 0 0 1.6rem; }
blockquote {
  margin: 0;
  padding: 0.15rem 0 0.15rem 0.9rem;
  border-left: 3px solid #d4a434;
  font-size: 1.12rem;
  white-space: pre-wrap;
}
.chapter { margin: 0.45rem 0 0; color: #8a8172; font-size: 0.85rem; }
.note { margin: 0.7rem 0 0; padding: 0.7rem 0.85rem; background: #efe8d8; white-space: pre-wrap; }
footer { margin-top: 2.6rem; padding-top: 0.9rem; border-top: 1px solid #e0d8c8; color: #9a9284; font-size: 0.82rem; }
footer p { margin: 0; }
`

func renderShareMarkdown(book Book, passages []exportedPassage, when time.Time) string {
	title := markdownInline(strings.TrimSpace(book.Title))
	if title == "" {
		title = "未命名书籍"
	}
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if author := markdownBlock(exportAuthor(book)); author != "" {
		b.WriteString(author)
		b.WriteString("\n\n")
	}
	b.WriteString("这份摘录由书桌（bookdesk）在本机导出。没有账号，没有追踪，未勾选的笔记不会出现在这里。\n\n")
	b.WriteString(when.Format("2006年1月2日"))
	b.WriteString("\n\n")
	if len(passages) == 0 {
		b.WriteString("没有选择划线。书名和作者之外的内容都留在这台电脑上。\n\n")
	}
	for _, passage := range passages {
		b.WriteString("---\n\n")
		quote := markdownBlock(passage.Quote)
		if quote == "" {
			quote = "（这段话是空的）"
		}
		for _, line := range strings.Split(quote, "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if chapter := markdownBlock(passage.Chapter); chapter != "" {
			b.WriteString(chapter)
			b.WriteString("\n\n")
		}
		if note := markdownBlock(passage.Note); note != "" {
			b.WriteString(note)
			b.WriteString("\n\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(shareFooter)
	b.WriteString("\n")
	return b.String()
}

// markdownInline 把一段文字收成一行，并让 HTML 和 Markdown 链接失效。
// 书名、作者、章节、引文、笔记都可能来自 EPUB 或读者粘贴的内容，不能原样写进会被 Jekyll / kramdown 执行的文件。
func markdownInline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return markdownSafeLine(strings.TrimSpace(s))
}

// markdownBlock 按行中和。行首的 #、---、列表和引用不会变成新的标题或分隔线。
func markdownBlock(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	s = strings.ReplaceAll(s, "\r", "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = markdownSafeLine(line)
	}
	return strings.Join(lines, "\n")
}

func markdownSafeLine(line string) string {
	var b strings.Builder
	for _, r := range line {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\\', '`', '*', '_', '[', ']', '(', ')', '!':
			// 反斜杠让链接、图片和强调失效。尖括号改成实体，避免 kramdown 把 \< 还原成原始标签。
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return escapeMarkdownBlockStart(b.String())
}

// escapeMarkdownBlockStart 在行首的块标记前加反斜杠，避免 #、---、列表和引用改变文档结构。
func escapeMarkdownBlockStart(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return line
	}
	rest := line[i:]
	if rest[0] == '#' || rest[0] == '-' || rest[0] == '+' || rest[0] == '*' || rest[0] == '>' || rest[0] == '=' || rest[0] == '|' || rest[0] == '~' || leadingOrderedList(rest) {
		return line[:i] + `\` + rest
	}
	return line
}

func leadingOrderedList(s string) bool {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(s) {
		return false
	}
	return s[i] == '.' || s[i] == ')'
}

func exportBaseName(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名书籍"
	}
	var b strings.Builder
	for _, r := range title {
		if isUnsafeFilenameRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if name == "" {
		name = "book"
	}
	return truncateGraphemes(name, 40)
}

func isUnsafeFilenameRune(r rune) bool {
	if r < 32 || r == 0x7f || r == 0x200b {
		return true
	}
	if r >= 0x202a && r <= 0x202e {
		return true
	}
	if r >= 0x2066 && r <= 0x2069 {
		return true
	}
	return strings.ContainsRune(`\/:*?"<>|`, r)
}

// truncateGraphemes 按字形簇截断，不把组合字符从基字上撕开。
func truncateGraphemes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	n := 0
	i := 0
	for i < len(s) && n < max {
		w := graphemeLen(s[i:])
		if w <= 0 {
			break
		}
		i += w
		n++
	}
	return s[:i]
}

func graphemeLen(s string) int {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 0 {
		return 0
	}
	if r == '\r' && strings.HasPrefix(s[size:], "\n") {
		return size + 1
	}
	i := size
	for i < len(s) && isGraphemeContinue(runeAt(s[i:])) {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
		if i > len(s) {
			break
		}
	}
	// 零宽连接符后面的字和它的组合标记仍算同一个簇（比如带连接符的表情）。
	if i < len(s) {
		prev, _ := utf8.DecodeLastRuneInString(s[:i])
		next, w := utf8.DecodeRuneInString(s[i:])
		if prev == 0x200d && next != utf8.RuneError {
			i += w
			for i < len(s) && isGraphemeContinue(runeAt(s[i:])) {
				_, ww := utf8.DecodeRuneInString(s[i:])
				i += ww
			}
		}
	}
	if isRegionalIndicator(r) && i < len(s) {
		next, w := utf8.DecodeRuneInString(s[i:])
		if isRegionalIndicator(next) {
			i += w
		}
	}
	return i
}

func runeAt(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func isGraphemeContinue(r rune) bool {
	if unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) {
		return true
	}
	if r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0xe0100 && r <= 0xe01ef) {
		return true
	}
	return r >= 0x1f3fb && r <= 0x1f3ff
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1f1e6 && r <= 0x1f1ff
}

func loadBookShare(bookID string) (Book, []BookShareItem, error) {
	bookID = strings.TrimSpace(bookID)
	if bookID == "" {
		return Book{}, nil, fmt.Errorf("没有指定书籍")
	}
	book, err := getBook(bookID)
	if err != nil {
		return Book{}, nil, err
	}
	if book == nil {
		return Book{}, nil, fmt.Errorf("找不到这本书")
	}
	highlights, err := listHighlights(&bookID)
	if err != nil {
		return Book{}, nil, err
	}
	cards, err := listCards()
	if err != nil {
		return Book{}, nil, err
	}
	return *book, collectShareItems(*book, highlights, cards), nil
}

func buildBookShareList(book Book, items []BookShareItem) BookShareList {
	return BookShareList{
		BookID:   book.ID,
		Title:    book.Title,
		Author:   deref(book.Author),
		Language: deref(book.Language),
		Items:    items,
	}
}

func renderBookExport(book Book, items []BookShareItem, choices []BookExportChoice, when time.Time) (htmlDoc, markdown string) {
	passages := exportPassages(items, choices)
	return renderShareHTML(book, passages, when), renderShareMarkdown(book, passages, when)
}

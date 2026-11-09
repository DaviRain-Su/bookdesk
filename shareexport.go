package main

import (
	"fmt"
	"html"
	"strings"
	"time"
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
	title := strings.TrimSpace(book.Title)
	if title == "" {
		title = "未命名书籍"
	}
	title = strings.ReplaceAll(title, "\n", " ")
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if author := exportAuthor(book); author != "" {
		b.WriteString(strings.ReplaceAll(author, "\n", " "))
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
		for _, line := range strings.Split(passage.Quote, "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if passage.Chapter != "" {
			b.WriteString(passage.Chapter)
			b.WriteString("\n\n")
		}
		if strings.TrimSpace(passage.Note) != "" {
			b.WriteString(passage.Note)
			b.WriteString("\n\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(shareFooter)
	b.WriteString("\n")
	return b.String()
}

func exportBaseName(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名书籍"
	}
	var b strings.Builder
	for _, r := range title {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if name == "" {
		name = "book"
	}
	if utf8.RuneCountInString(name) > 40 {
		rs := []rune(name)
		name = string(rs[:40])
	}
	return name
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

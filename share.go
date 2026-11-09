package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo"
)

// 分享出去：划线卡片的文件落盘，以及一本书的静态公开页。
// 这里没有网络请求。笔记只有在 choice.NoteIDs 里才会写进页面。

const shareFooter = "read with bookdesk / 书桌"

func clipOptional(s *string, limit int) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	r := []rune(t)
	if limit > 0 && len(r) > limit {
		t = string(r[:limit])
	}
	return &t
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func normQuote(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func cardBelongs(card Card, bookID string) bool {
	if card.SourceBookID != nil && *card.SourceBookID == bookID {
		return true
	}
	if card.QuoteLocation != nil && card.QuoteLocation.BookID == bookID {
		return true
	}
	return false
}

type bookMeta struct {
	Title  string
	Author string
}

type exportCandidate struct {
	ID        string
	Kind      string
	Quote     string
	Note      string
	Chapter   string
	Color     string
	CreatedAt int64
}

type exportedItem struct {
	ID        string
	Quote     string
	Note      string
	Chapter   string
	Color     string
	CreatedAt int64
}

// buildExportCandidates 把一本书的划线和摘录卡收成一份清单。
// 同一段文字只出现一次；卡片正文记在 Note 上，但是否公开由后面的选择决定。
func buildExportCandidates(bookID string, highlights []Highlight, cards []Card) []exportCandidate {
	type cardHit struct {
		note, chapter, id, quote string
		updated, created         int64
	}
	byQuote := map[string]cardHit{}
	var order []string
	for _, card := range cards {
		if card.Type == "image" || card.Type == "character" {
			continue
		}
		quote := strings.TrimSpace(deref(card.Quote))
		if quote == "" || !cardBelongs(card, bookID) {
			continue
		}
		key := normQuote(quote)
		hit, ok := byQuote[key]
		if ok && card.UpdatedAt < hit.updated {
			continue
		}
		byQuote[key] = cardHit{
			note:    strings.TrimSpace(deref(card.Body)),
			chapter: strings.TrimSpace(deref(card.Chapter)),
			id:      card.ID,
			quote:   quote,
			updated: card.UpdatedAt,
			created: card.CreatedAt,
		}
		if !ok {
			order = append(order, key)
		}
	}

	used := map[string]bool{}
	var out []exportCandidate
	for _, h := range highlights {
		if h.BookID != bookID {
			continue
		}
		quote := strings.TrimSpace(h.Text)
		if quote == "" {
			continue
		}
		key := normQuote(quote)
		hit := byQuote[key]
		used[key] = true
		chapter := strings.TrimSpace(deref(h.Chapter))
		if chapter == "" {
			chapter = hit.chapter
		}
		out = append(out, exportCandidate{
			ID:        h.ID,
			Kind:      "highlight",
			Quote:     quote,
			Note:      hit.note,
			Chapter:   chapter,
			Color:     h.Color,
			CreatedAt: h.CreatedAt,
		})
	}
	for _, key := range order {
		if used[key] {
			continue
		}
		hit := byQuote[key]
		out = append(out, exportCandidate{
			ID:        hit.id,
			Kind:      "card",
			Quote:     hit.quote,
			Note:      hit.note,
			Chapter:   hit.chapter,
			CreatedAt: hit.created,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

// selectAllQuotes 只选划线。笔记保持未选。
func selectAllQuotes(items []exportCandidate) ExportChoice {
	choice := ExportChoice{QuoteIDs: make([]string, 0, len(items))}
	for _, it := range items {
		choice.QuoteIDs = append(choice.QuoteIDs, it.ID)
	}
	return choice
}

// applyExportSelection 按勾选结果挑出要写进页面的条目。
// 笔记必须同时满足：这条划线被选中，并且它的 id 在 NoteIDs 里。
func applyExportSelection(items []exportCandidate, choice ExportChoice) []exportedItem {
	want := map[string]bool{}
	for _, id := range choice.QuoteIDs {
		if id != "" {
			want[id] = true
		}
	}
	notes := map[string]bool{}
	for _, id := range choice.NoteIDs {
		if id != "" {
			notes[id] = true
		}
	}
	out := make([]exportedItem, 0, len(want))
	for _, it := range items {
		if !want[it.ID] {
			continue
		}
		note := ""
		if notes[it.ID] {
			note = strings.TrimSpace(it.Note)
		}
		out = append(out, exportedItem{
			ID:        it.ID,
			Quote:     it.Quote,
			Note:      note,
			Chapter:   it.Chapter,
			Color:     it.Color,
			CreatedAt: it.CreatedAt,
		})
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func colorClass(color string) string {
	switch color {
	case "green", "blue", "pink", "purple":
		return color
	default:
		return "yellow"
	}
}

type pageView struct {
	Title  string
	Author string
	Items  []pageItem
}

type pageItem struct {
	Quote      string
	Chapter    string
	Note       string
	ColorClass string
}

func pageFrom(meta bookMeta, items []exportedItem) pageView {
	title := oneLine(meta.Title)
	if title == "" {
		title = "未命名书籍"
	}
	view := pageView{Title: title, Author: oneLine(meta.Author), Items: make([]pageItem, 0, len(items))}
	for _, it := range items {
		quote := strings.TrimSpace(it.Quote)
		if quote == "" {
			continue
		}
		view.Items = append(view.Items, pageItem{
			Quote:      quote,
			Chapter:    oneLine(it.Chapter),
			Note:       strings.TrimSpace(it.Note),
			ColorClass: colorClass(it.Color),
		})
	}
	return view
}

var publicPageTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  html, body { margin: 0; background: #f3efe6; color: #2b261f; }
  body {
    font-family: "Songti SC", "STSong", "Noto Serif CJK SC", "Source Han Serif SC", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "WenQuanYi Micro Hei", serif;
    line-height: 1.75;
  }
  main { max-width: 680px; margin: 0 auto; padding: 64px 24px 80px; }
  .kicker {
    margin: 0;
    letter-spacing: 0.18em;
    font-size: 12px;
    color: #8a8174;
    font-family: "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "WenQuanYi Micro Hei", sans-serif;
  }
  h1 { font-size: 36px; font-weight: 600; line-height: 1.35; margin: 10px 0 0; }
  .author { margin: 8px 0 0; color: #6f675c; }
  ol { list-style: none; padding: 0; margin: 40px 0 0; }
  li { margin: 0 0 28px; padding: 2px 0 2px 16px; border-left: 3px solid #e0b84a; }
  li.c-green { border-color: #6eae78; }
  li.c-blue { border-color: #6aa4d0; }
  li.c-pink { border-color: #d58aa0; }
  li.c-purple { border-color: #a894d0; }
  blockquote { margin: 0; font-size: 18px; white-space: pre-wrap; }
  .chapter {
    margin: 8px 0 0;
    font-size: 13px;
    color: #8a8174;
    font-family: "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "WenQuanYi Micro Hei", sans-serif;
  }
  .note { margin: 10px 0 0; font-size: 15px; color: #4e483f; white-space: pre-wrap; }
  .empty { margin-top: 36px; color: #6f675c; }
  footer {
    margin-top: 48px;
    padding-top: 16px;
    border-top: 1px solid #e4dccb;
    font-size: 12px;
    line-height: 1.7;
    color: #8a8174;
    font-family: "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "WenQuanYi Micro Hei", sans-serif;
  }
  footer p { margin: 0 0 6px; }
  footer strong { font-weight: 550; color: #5e574c; }
</style>
</head>
<body>
<main>
  <p class="kicker">读书笔记</p>
  <h1>{{.Title}}</h1>
  {{if .Author}}<p class="author">{{.Author}}</p>{{end}}
  {{if .Items}}
  <ol>
    {{range .Items}}
    <li class="c-{{.ColorClass}}">
      <blockquote>{{.Quote}}</blockquote>
      {{if .Chapter}}<p class="chapter">{{.Chapter}}</p>{{end}}
      {{if .Note}}<p class="note">{{.Note}}</p>{{end}}
    </li>
    {{end}}
  </ol>
  {{else}}
  <p class="empty">这一页没有放上划线。</p>
  {{end}}
  <footer>
    <p><strong>read with bookdesk / 书桌</strong></p>
    <p>这一页由书桌在本机导出。没有账号，没有统计，页面自己也不会再去请求别的服务器。笔记只有在导出时被勾选才会出现。</p>
  </footer>
</main>
</body>
</html>
`))

func renderPublicHTML(meta bookMeta, items []exportedItem) (string, error) {
	var buf bytes.Buffer
	if err := publicPageTmpl.Execute(&buf, pageFrom(meta, items)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderPublicMarkdown(meta bookMeta, items []exportedItem) string {
	view := pageFrom(meta, items)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(view.Title)
	b.WriteString("\n\n")
	if view.Author != "" {
		b.WriteString(view.Author)
		b.WriteString("\n\n")
	}
	if len(view.Items) == 0 {
		b.WriteString("这一页没有放上划线。\n\n")
	}
	for _, it := range view.Items {
		for _, line := range strings.Split(it.Quote, "\n") {
			b.WriteString("> ")
			b.WriteString(strings.TrimRight(line, " \t"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if it.Chapter != "" {
			b.WriteString(it.Chapter)
			b.WriteString("\n\n")
		}
		if it.Note != "" {
			b.WriteString(it.Note)
			if !strings.HasSuffix(it.Note, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(shareFooter)
	b.WriteString("\n")
	return b.String()
}

// externalResourceRefs 找出会发请求的标签和样式。正文里的网址不算。
func externalResourceRefs(html string) []string {
	var hits []string
	lower := strings.ToLower(html)
	for _, tag := range []string{"<script", "<link", "<iframe", "<object", "<embed", "<img", "<video", "<audio", "<source", "<base", "<form"} {
		if strings.Contains(lower, tag) {
			hits = append(hits, tag)
		}
	}
	if strings.Contains(lower, "@import") || strings.Contains(lower, "url(") {
		hits = append(hits, "css-url")
	}
	rest := lower
	for {
		i := strings.Index(rest, "<")
		if i < 0 {
			break
		}
		j := strings.Index(rest[i:], ">")
		if j < 0 {
			break
		}
		tag := rest[i : i+j]
		if strings.Contains(tag, " src=") || strings.Contains(tag, " href=") || strings.Contains(tag, " poster=") || strings.Contains(tag, " action=") {
			hits = append(hits, "attr")
		}
		rest = rest[i+j+1:]
	}
	return hits
}

func loadExport(bookID string) (bookMeta, []exportCandidate, error) {
	book, err := getBook(bookID)
	if err != nil {
		return bookMeta{}, nil, err
	}
	if book == nil {
		return bookMeta{}, nil, fmt.Errorf("找不到这本书")
	}
	highlights, err := listHighlights(&bookID)
	if err != nil {
		return bookMeta{}, nil, err
	}
	cards, err := listCards()
	if err != nil {
		return bookMeta{}, nil, err
	}
	return bookMeta{Title: book.Title, Author: deref(book.Author)}, buildExportCandidates(bookID, highlights, cards), nil
}

func renderChoice(meta bookMeta, items []exportCandidate, choice ExportChoice) (RenderedExport, error) {
	selected := applyExportSelection(items, choice)
	html, err := renderPublicHTML(meta, selected)
	if err != nil {
		return RenderedExport{}, err
	}
	noteCount := 0
	for _, it := range selected {
		if strings.TrimSpace(it.Note) != "" {
			noteCount++
		}
	}
	base := oneLine(meta.Title)
	if base == "" {
		base = "公开页"
	}
	return RenderedExport{
		HTML:          html,
		Markdown:      renderPublicMarkdown(meta, selected),
		QuoteCount:    int64(len(selected)),
		NoteCount:     int64(noteCount),
		SuggestedBase: safeFileName(base, "公开页"),
	}, nil
}

// Share 把图片和公开页写到用户选的位置，或放进系统剪贴板。
type Share struct{}

func (Share) ListBook(ctx context.Context, bookID string) (ShareBook, error) {
	meta, items, err := loadExport(bookID)
	if err != nil {
		return ShareBook{}, err
	}
	out := make([]ShareCandidate, 0, len(items))
	for _, it := range items {
		out = append(out, ShareCandidate{
			ID:        it.ID,
			Kind:      it.Kind,
			Quote:     it.Quote,
			Note:      it.Note,
			Chapter:   it.Chapter,
			Color:     it.Color,
			CreatedAt: it.CreatedAt,
		})
	}
	return ShareBook{ID: bookID, Title: meta.Title, Author: meta.Author, Items: out}, nil
}

func (Share) RenderExport(ctx context.Context, bookID string, choice ExportChoice) (RenderedExport, error) {
	meta, items, err := loadExport(bookID)
	if err != nil {
		return RenderedExport{}, err
	}
	return renderChoice(meta, items, choice)
}

func (Share) SavePNG(ctx context.Context, suggestedName, pngBase64 string) (string, error) {
	data, err := decodeBase64Payload(pngBase64)
	if err != nil {
		return "", err
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("这不是一张有效的 PNG")
	}
	name := safeFileName(suggestedName, "划线.png")
	if !strings.HasSuffix(strings.ToLower(name), ".png") {
		name += ".png"
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "保存分享卡片",
		DefaultPath: name,
		ButtonLabel: "保存",
		Filters:     []mygo.FileFilter{{Name: "PNG 图片", Extensions: []string{"png"}}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += ".png"
	}
	return path, os.WriteFile(path, data, 0o644)
}

func (Share) CopyPNG(ctx context.Context, pngBase64 string) error {
	data, err := decodeBase64Payload(pngBase64)
	if err != nil {
		return err
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("这不是一张有效的 PNG")
	}
	return mygo.Clipboard.WriteImage(data)
}

func (Share) SaveText(ctx context.Context, suggestedName, text string) (string, error) {
	if len(text) > 8<<20 {
		return "", fmt.Errorf("页面太大")
	}
	name := safeFileName(suggestedName, "公开页.html")
	ext := strings.ToLower(filepath.Ext(name))
	filterName := "HTML"
	if ext == ".md" || ext == ".markdown" {
		filterName = "Markdown"
	}
	if ext == "" {
		name += ".html"
		ext = ".html"
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "保存公开页",
		DefaultPath: name,
		ButtonLabel: "保存",
		Filters:     []mygo.FileFilter{{Name: filterName, Extensions: []string{strings.TrimPrefix(ext, ".")}}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += ext
	}
	return path, os.WriteFile(path, []byte(text), 0o644)
}

func safeFileName(name, fallback string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		name = fallback
	}
	r := []rune(name)
	if len(r) > 80 {
		name = string(r[:80])
	}
	return name
}

func decodeBase64Payload(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); i >= 0 && strings.Contains(strings.ToLower(s[:i]), "base64") {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if len(s) > 16<<20 {
		return nil, fmt.Errorf("图片太大")
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("图片数据无法读取")
	}
	if len(data) > 12<<20 {
		return nil, fmt.Errorf("图片太大")
	}
	return data, nil
}

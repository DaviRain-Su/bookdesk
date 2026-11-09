package main

import (
	"strings"
	"unicode"
)

// shareFooter 印在分享卡底部。不放链接，也不引导到任何网站。
const shareFooter = "read with bookdesk / 书桌"

type textMeasure func(string) float64

type shareLayoutLimits struct {
	Vertical       bool
	MaxQuoteWidth  float64
	MaxNoteWidth   float64
	MaxQuoteLines  int
	MaxNoteLines   int
	MaxColumns     int
	MaxRunes       int
	NoteMaxColumns int
	NoteMaxRunes   int
}

// ShareCardContent 是排好的文字，还没画成图片。测试只检查这一层。
type ShareCardContent struct {
	QuoteLines     []string
	QuoteTruncated bool
	NoteLines      []string
	NoteTruncated  bool
	QuoteColumns   []string
	NoteColumns    []string
	Credit         string
	Chapter        string
	Footer         string
	IncludeNote    bool
	Vertical       bool
}

func normalizeTemplate(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dark", "night", "夜间", "夜":
		return "dark"
	case "vertical", "竖排", "竖", "tategaki":
		return "vertical"
	default:
		return "light"
	}
}

func normalizeRatio(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "portrait", "3:4", "3/4", "xiaohongshu", "竖图":
		return "portrait"
	default:
		return "square"
	}
}

func cardPixelSize(ratio string) (int, int) {
	if normalizeRatio(ratio) == "portrait" {
		return 1080, 1440
	}
	return 1080, 1080
}

func bookCredit(title, author string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名书籍"
	}
	if !strings.HasPrefix(title, "《") || !strings.HasSuffix(title, "》") {
		title = strings.Trim(title, "《》")
		title = "《" + title + "》"
	}
	author = strings.TrimSpace(author)
	if author == "" || author == "未知作者" {
		return title
	}
	return title + "  ·  " + author
}

func layoutShareCardContent(in ShareCardInput, lim shareLayoutLimits, quoteMeasure, noteMeasure textMeasure) ShareCardContent {
	quote := strings.TrimSpace(in.Quote)
	if quote == "" {
		quote = "（这段话是空的）"
	}
	note := ""
	if in.IncludeNote {
		note = strings.TrimSpace(in.Note)
	}
	content := ShareCardContent{
		Credit:      bookCredit(in.Title, in.Author),
		Chapter:     strings.TrimSpace(in.Chapter),
		Footer:      shareFooter,
		IncludeNote: note != "",
		Vertical:    lim.Vertical,
	}
	if lim.Vertical {
		cols := lim.MaxColumns
		if cols < 1 {
			cols = 1
		}
		runes := lim.MaxRunes
		if runes < 1 {
			runes = 1
		}
		content.QuoteColumns, content.QuoteTruncated = layoutVerticalText(quote, cols, runes)
		if content.IncludeNote && lim.NoteMaxColumns > 0 {
			nr := lim.NoteMaxRunes
			if nr < 1 {
				nr = runes
			}
			content.NoteColumns, content.NoteTruncated = layoutVerticalText(note, lim.NoteMaxColumns, nr)
		}
		return content
	}
	if noteMeasure == nil {
		noteMeasure = quoteMeasure
	}
	content.QuoteLines, content.QuoteTruncated = wrapText(quote, lim.MaxQuoteWidth, quoteMeasure, lim.MaxQuoteLines)
	if content.IncludeNote {
		content.NoteLines, content.NoteTruncated = wrapText(note, lim.MaxNoteWidth, noteMeasure, lim.MaxNoteLines)
	}
	return content
}

func wrapText(text string, maxWidth float64, measure textMeasure, maxLines int) ([]string, bool) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	text = strings.ReplaceAll(text, "\r", "\n")
	if text == "" {
		return nil, false
	}
	if measure == nil {
		measure = func(s string) float64 { return float64(len([]rune(s))) }
	}
	if maxWidth <= 0 {
		maxWidth = 1
	}
	var all []string
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		all = append(all, wrapParagraph(para, maxWidth, measure)...)
	}
	if len(all) == 0 {
		return nil, false
	}
	if maxLines > 0 && len(all) > maxLines {
		kept := append([]string{}, all[:maxLines]...)
		kept[maxLines-1] = ellipsize(kept[maxLines-1], maxWidth, measure)
		return kept, true
	}
	return all, false
}

type shareToken struct {
	text  string
	space bool
}

func tokenizeShare(text string) []shareToken {
	rs := []rune(text)
	var out []shareToken
	for i := 0; i < len(rs); {
		r := rs[i]
		if r == ' ' || r == '\t' || r == '　' {
			j := i + 1
			for j < len(rs) && (rs[j] == ' ' || rs[j] == '\t' || rs[j] == '　') {
				j++
			}
			out = append(out, shareToken{space: true})
			i = j
			continue
		}
		if isShareWordRune(r) {
			j := i + 1
			for j < len(rs) && isShareWordRune(rs[j]) {
				j++
			}
			out = append(out, shareToken{text: string(rs[i:j])})
			i = j
			continue
		}
		out = append(out, shareToken{text: string(r)})
		i++
	}
	return out
}

func isShareWordRune(r rune) bool {
	if unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
		return false
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	return r == '\'' || r == '’' || r == '-'
}

func isClosingRune(r rune) bool {
	switch r {
	case '，', '。', '、', '；', '：', '！', '？', '）', '》', '」', '』', '】', '〕', '〉', '｝', '’', '”',
		',', '.', ';', ':', '!', '?', ')', ']', '}':
		return true
	default:
		return false
	}
}

func isOpeningRune(r rune) bool {
	switch r {
	case '（', '《', '「', '『', '【', '〔', '〈', '｛', '‘', '“', '(', '[', '{':
		return true
	default:
		return false
	}
}

func isClosingToken(s string) bool {
	rs := []rune(s)
	return len(rs) == 1 && isClosingRune(rs[0])
}

func wrapParagraph(text string, maxWidth float64, measure textMeasure) []string {
	tokens := tokenizeShare(text)
	var lines []string
	var cur strings.Builder
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.space {
			continue
		}
		sep := ""
		if cur.Len() > 0 && i > 0 && tokens[i-1].space {
			sep = " "
		}
		piece := tok.text
		if cur.Len() == 0 && measure(piece) > maxWidth {
			appendWideToken(&cur, &lines, piece, maxWidth, measure)
			continue
		}
		candidate := piece
		if cur.Len() > 0 {
			candidate = cur.String() + sep + piece
		}
		if measure(candidate) <= maxWidth {
			cur.WriteString(sep)
			cur.WriteString(piece)
			continue
		}
		if isClosingToken(piece) && cur.Len() > 0 {
			cur.WriteString(piece)
			continue
		}
		line := []rune(cur.String())
		if len(line) > 1 && isOpeningRune(line[len(line)-1]) {
			moved := line[len(line)-1]
			cur.Reset()
			cur.WriteString(string(line[:len(line)-1]))
			if cur.Len() > 0 {
				lines = append(lines, cur.String())
				cur.Reset()
			}
			cur.WriteRune(moved)
			i--
			continue
		}
		if cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			i--
			continue
		}
		cur.WriteString(piece)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func appendWideToken(cur *strings.Builder, lines *[]string, piece string, maxWidth float64, measure textMeasure) {
	for _, r := range piece {
		ch := string(r)
		if cur.Len() > 0 && measure(cur.String()+ch) > maxWidth && !isClosingRune(r) {
			*lines = append(*lines, cur.String())
			cur.Reset()
		}
		cur.WriteString(ch)
	}
}

func ellipsize(line string, maxWidth float64, measure textMeasure) string {
	const ell = "…"
	rs := []rune(strings.TrimRight(line, " \t"))
	for len(rs) > 0 && measure(string(rs)+ell) > maxWidth {
		rs = rs[:len(rs)-1]
	}
	if len(rs) == 0 {
		if measure(ell) <= maxWidth {
			return ell
		}
		return ""
	}
	out := string(rs) + ell
	if strings.HasSuffix(string(rs), ell) {
		return string(rs)
	}
	return out
}

// layoutVerticalText 把文字排成自上而下、自右向左的列。
// 返回的第 0 列是最右边、最先读到的一列。放不下时，最后一列以 … 收尾。
func layoutVerticalText(text string, maxCols, maxRunes int) ([]string, bool) {
	if maxCols < 1 {
		maxCols = 1
	}
	if maxRunes < 1 {
		maxRunes = 1
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	var cols [][]rune
	var col []rune
	truncated := false
	push := func() bool {
		if len(col) == 0 {
			return true
		}
		if len(cols) >= maxCols {
			truncated = true
			return false
		}
		cols = append(cols, col)
		col = nil
		return true
	}
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\n' {
			if !push() {
				break
			}
			continue
		}
		if len(col) >= maxRunes {
			if !push() {
				break
			}
		}
		if len(cols) >= maxCols {
			truncated = true
			break
		}
		col = append(col, r)
	}
	if !truncated && len(col) > 0 {
		if !push() {
			truncated = true
		}
	}
	if truncated {
		cols = applyVerticalEllipsis(cols, maxCols, maxRunes)
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = string(c)
	}
	return out, truncated
}

func applyVerticalEllipsis(cols [][]rune, maxCols, maxRunes int) [][]rune {
	if maxRunes < 1 {
		maxRunes = 1
	}
	if len(cols) == 0 {
		return [][]rune{{'…'}}
	}
	if len(cols) > maxCols {
		cols = cols[:maxCols]
	}
	last := append([]rune{}, cols[len(cols)-1]...)
	if len(last) == 0 {
		last = []rune{'…'}
	} else if last[len(last)-1] != '…' {
		if len(last) >= maxRunes {
			last[len(last)-1] = '…'
		} else {
			last = append(last, '…')
		}
	}
	cols[len(cols)-1] = last
	return cols
}

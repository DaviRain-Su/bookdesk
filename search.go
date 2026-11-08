package main

import (
	"context"
	"strings"
)

// Search 是跨书 / 书内搜索。
type Search struct{}

const (
	maxHits           = 100
	maxHitsPerBook    = 20
	maxHitsPerChapter = 6
	contextBytes      = 36
)

func runSearch(qRaw string, bookID *string) ([]SearchHit, error) {
	q := strings.TrimSpace(qRaw)
	if q == "" {
		return []SearchHit{}, nil
	}
	chapters, err := searchChapters(q, bookID)
	if err != nil {
		return nil, err
	}
	books, err := listBooks()
	if err != nil {
		return nil, err
	}
	titles := make(map[string]string, len(books))
	for _, b := range books {
		titles[b.ID] = b.Title
	}

	hits := make([]SearchHit, 0, 32)
	perBook := make(map[string]int)
	for _, ch := range chapters {
		if len(hits) >= maxHits {
			break
		}
		count := perBook[ch.BookID]
		if count >= maxHitsPerBook {
			continue
		}
		title := titles[ch.BookID]
		pos := 0
		chapterHits := 0
		for chapterHits < maxHitsPerChapter && len(hits) < maxHits {
			rel := indexOfIgnoreCase(ch.Text[pos:], q)
			if rel < 0 {
				break
			}
			matchStart := pos + rel
			matchEnd := matchStart + len(q)
			rawStart := 0
			if matchStart > contextBytes {
				rawStart = matchStart - contextBytes
			}
			rawEnd := len(ch.Text)
			if matchEnd+contextBytes < rawEnd {
				rawEnd = matchEnd + contextBytes
			}
			start := floorCharBoundary(ch.Text, rawStart)
			end := ceilCharBoundary(ch.Text, rawEnd)
			prefix := ""
			if start > 0 {
				prefix = "…"
			}
			suffix := ""
			if end < len(ch.Text) {
				suffix = "…"
			}
			snippet := prefix + ch.Text[start:end] + suffix
			hlStart := utf16Len(snippet, len(prefix)+(matchStart-start))
			hlEnd := utf16Len(snippet, len(prefix)+(matchEnd-start))
			charOffset := int64(matchStart)
			var pageLabel *string
			if label := chapterLabel(ch.Text); label != "" {
				pageLabel = &label
			}
			hits = append(hits, SearchHit{
				BookID: ch.BookID,
				Title:  title,
				Location: BookLocation{
					BookID:     ch.BookID,
					SpineIndex: ch.SpineIndex,
					Href:       ch.Href,
					PageLabel:  pageLabel,
					CharOffset: &charOffset,
				},
				Snippet:    snippet,
				Highlights: [][2]int64{{hlStart, hlEnd}},
			})
			chapterHits++
			pos = matchEnd
		}
		perBook[ch.BookID] = count + chapterHits
	}
	return hits, nil
}

// chapterLabel 取章节正文的第一行当分区标题。多数 EPUB 的第一行
// 就是 h1/h2 的章节名；取不到就返回空，前端退回「第 N 节」。
func chapterLabel(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 40 {
			return string(runes[:40]) + "…"
		}
		return line
	}
	return ""
}

func (Search) Query(ctx context.Context, q string) ([]SearchHit, error) {
	return runSearch(q, nil)
}

func (Search) QueryBook(ctx context.Context, bookID, q string) ([]SearchHit, error) {
	return runSearch(q, &bookID)
}

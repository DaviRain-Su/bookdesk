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
			hits = append(hits, SearchHit{
				BookID: ch.BookID,
				Title:  title,
				Location: BookLocation{
					BookID:     ch.BookID,
					SpineIndex: ch.SpineIndex,
					Href:       ch.Href,
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

func (Search) Query(ctx context.Context, q string) ([]SearchHit, error) {
	return runSearch(q, nil)
}

func (Search) QueryBook(ctx context.Context, bookID, q string) ([]SearchHit, error) {
	return runSearch(q, &bookID)
}

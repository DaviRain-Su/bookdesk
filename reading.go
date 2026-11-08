package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Reading 记录上次用的模式、上次读的书，以及每本书的阅读位置。
// 只写 settings 表，不新建数据库，也不碰 books/ 里的书。
type Reading struct{}

const (
	readingModeKey     = "reading.mode"
	readingLastBookKey = "reading.last_book"
	readingProgressKey = "reading.progress."
)

// ReaderModeEvent 是菜单「显示 → 全屏阅读 / 书桌」发给前端的切换指令。
type ReaderModeEvent struct {
	Mode string `json:"mode"`
}

// ReadingState 是启动时要恢复的阅读状态。
type ReadingState struct {
	LastMode   string `json:"lastMode"`
	LastBookID string `json:"lastBookId"`
}

// BookProgress 是一本书的阅读位置；书卡和全屏阅读共用这一份。
type BookProgress struct {
	BookID     string  `json:"bookId"`
	CFI        string  `json:"cfi"`
	SpineIndex int64   `json:"spineIndex"`
	Percent    float64 `json:"percent"`
	Chapter    string  `json:"chapter"`
	UpdatedAt  int64   `json:"updatedAt"`
}

func (Reading) GetState(ctx context.Context) (ReadingState, error) {
	state := ReadingState{LastMode: "desk"}
	mode, err := getSetting(readingModeKey)
	if err != nil {
		return state, err
	}
	if mode != nil && *mode == "reader" {
		state.LastMode = "reader"
	}
	book, err := getSetting(readingLastBookKey)
	if err != nil {
		return state, err
	}
	if book != nil {
		state.LastBookID = strings.TrimSpace(*book)
	}
	return state, nil
}

func (Reading) SetLastMode(ctx context.Context, mode string) error {
	if mode != "reader" {
		mode = "desk"
	}
	return setSetting(readingModeKey, mode)
}

func (Reading) SetLastBook(ctx context.Context, bookID string) error {
	bookID = strings.TrimSpace(bookID)
	if bookID == "" {
		return deleteSetting(readingLastBookKey)
	}
	return setSetting(readingLastBookKey, bookID)
}

func (Reading) GetProgress(ctx context.Context, bookID string) (*BookProgress, error) {
	bookID = strings.TrimSpace(bookID)
	if bookID == "" {
		return nil, nil
	}
	raw, err := getSetting(readingProgressKey + bookID)
	if err != nil || raw == nil {
		return nil, err
	}
	var p BookProgress
	if err := json.Unmarshal([]byte(*raw), &p); err != nil {
		// 旧版本或手改坏的进度直接当作没有，不挡住打开书。
		return nil, nil
	}
	p.BookID = bookID
	return &p, nil
}

func (Reading) SaveProgress(ctx context.Context, p BookProgress) error {
	bookID := strings.TrimSpace(p.BookID)
	if bookID == "" {
		return nil
	}
	p.BookID = bookID
	p.CFI = strings.TrimSpace(p.CFI)
	p.Chapter = strings.TrimSpace(p.Chapter)
	if runes := []rune(p.Chapter); len(runes) > 80 {
		p.Chapter = string(runes[:80])
	}
	if p.Percent < 0 {
		p.Percent = 0
	} else if p.Percent > 1 {
		p.Percent = 1
	}
	p.UpdatedAt = time.Now().UnixMilli()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return setSetting(readingProgressKey+bookID, string(raw))
}

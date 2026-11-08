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
	readingSettingsKey = "reading.settings"
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

// ReadingSettings 是全屏阅读的外观设置，全局共用一份。
// 存进 settings 表，不新建数据库；书卡不读这份设置。
type ReadingSettings struct {
	Theme       string  `json:"theme"`
	FontSize    int64   `json:"fontSize"`
	LineHeight  float64 `json:"lineHeight"`
	ColumnWidth int64   `json:"columnWidth"`
	Vertical    bool    `json:"vertical"`
	Convert     string  `json:"convert"`
	Zen         bool    `json:"zen"`
	Spread      bool    `json:"spread"`
}

func defaultReadingSettings() ReadingSettings {
	return ReadingSettings{
		Theme:       "paper",
		FontSize:    17,
		LineHeight:  1.95,
		ColumnWidth: 780,
		Convert:     "none",
	}
}

// normalizeReadingSettings 把坏数据挡在阅读器外面。
// 零值当作「没填」落回默认值，其余越界值夹到可用范围。
func normalizeReadingSettings(s ReadingSettings) ReadingSettings {
	d := defaultReadingSettings()
	switch s.Theme {
	case "paper", "day", "night":
	default:
		s.Theme = d.Theme
	}
	if s.FontSize == 0 {
		s.FontSize = d.FontSize
	} else if s.FontSize < 14 {
		s.FontSize = 14
	} else if s.FontSize > 24 {
		s.FontSize = 24
	}
	if s.LineHeight == 0 {
		s.LineHeight = d.LineHeight
	} else if s.LineHeight < 1.5 {
		s.LineHeight = 1.5
	} else if s.LineHeight > 2.4 {
		s.LineHeight = 2.4
	}
	if s.ColumnWidth == 0 {
		s.ColumnWidth = d.ColumnWidth
	} else if s.ColumnWidth < 560 {
		s.ColumnWidth = 560
	} else if s.ColumnWidth > 1100 {
		s.ColumnWidth = 1100
	}
	switch s.Convert {
	case "none", "simplified", "traditional":
	default:
		s.Convert = d.Convert
	}
	return s
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

func (Reading) GetSettings(ctx context.Context) (ReadingSettings, error) {
	settings := defaultReadingSettings()
	raw, err := getSetting(readingSettingsKey)
	if err != nil || raw == nil {
		return settings, err
	}
	var saved ReadingSettings
	if err := json.Unmarshal([]byte(*raw), &saved); err != nil {
		// 旧版本或手改坏的设置回到默认，不挡住打开书。
		return settings, nil
	}
	return normalizeReadingSettings(saved), nil
}

func (Reading) SaveSettings(ctx context.Context, settings ReadingSettings) error {
	settings = normalizeReadingSettings(settings)
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return setSetting(readingSettingsKey, string(raw))
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

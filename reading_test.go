package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

// resetTestDB 让每个测试用自己的临时目录；openDB 是 sync.Once，
// 前一个测试的临时目录被删掉后句柄不能继续用。
func resetTestDB() {
	if database != nil {
		_ = database.Close()
		database = nil
	}
	dbErr = nil
	ftsReady = false
	dbOnce = sync.Once{}
}

func TestReadingStateAndProgress(t *testing.T) {
	dir := t.TempDir()
	resetTestDB()
	userDataDir = dir
	booksDir = filepath.Join(dir, "books")
	imagesDir = filepath.Join(dir, "images")
	if err := ensureDir(booksDir); err != nil {
		t.Fatal(err)
	}
	if err := openDB(); err != nil {
		t.Fatalf("openDB: %v", err)
	}

	ctx := context.Background()
	state, err := Reading{}.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastMode != "desk" || state.LastBookID != "" {
		t.Fatalf("initial state = %+v", state)
	}

	if err := (Reading{}).SetLastMode(ctx, "reader"); err != nil {
		t.Fatal(err)
	}
	if err := (Reading{}).SetLastBook(ctx, "book-1"); err != nil {
		t.Fatal(err)
	}
	state, err = Reading{}.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastMode != "reader" || state.LastBookID != "book-1" {
		t.Fatalf("state = %+v", state)
	}

	progress, err := Reading{}.GetProgress(ctx, "book-1")
	if err != nil || progress != nil {
		t.Fatalf("progress before save = %+v, %v", progress, err)
	}
	if err := (Reading{}).SaveProgress(ctx, BookProgress{
		BookID:     "book-1",
		CFI:        "epubcfi(/6/4!/4/2)",
		SpineIndex: 3,
		Percent:    1.7,
		Chapter:    "第一章",
	}); err != nil {
		t.Fatal(err)
	}
	progress, err = Reading{}.GetProgress(ctx, "book-1")
	if err != nil || progress == nil {
		t.Fatalf("progress after save = %+v, %v", progress, err)
	}
	if progress.CFI != "epubcfi(/6/4!/4/2)" || progress.SpineIndex != 3 || progress.Percent != 1 {
		t.Fatalf("progress = %+v", progress)
	}
	if progress.UpdatedAt == 0 {
		t.Error("updatedAt not set")
	}

	// 模式只认 reader，其余都落回书桌，避免坏数据把启动卡在全屏。
	if err := (Reading{}).SetLastMode(ctx, "something"); err != nil {
		t.Fatal(err)
	}
	state, err = Reading{}.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastMode != "desk" {
		t.Fatalf("mode = %q", state.LastMode)
	}
}

func TestReadingSettings(t *testing.T) {
	dir := t.TempDir()
	resetTestDB()
	userDataDir = dir
	booksDir = filepath.Join(dir, "books")
	imagesDir = filepath.Join(dir, "images")
	if err := ensureDir(booksDir); err != nil {
		t.Fatal(err)
	}
	if err := openDB(); err != nil {
		t.Fatalf("openDB: %v", err)
	}

	ctx := context.Background()
	settings, err := Reading{}.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings != defaultReadingSettings() {
		t.Fatalf("default settings = %+v", settings)
	}

	want := ReadingSettings{
		Theme:       "night",
		FontSize:    20,
		LineHeight:  2.1,
		ColumnWidth: 960,
		Vertical:    true,
		Convert:     "traditional",
		Zen:         true,
		Spread:      true,
	}
	if err := (Reading{}).SaveSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	settings, err = Reading{}.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings != want {
		t.Fatalf("settings = %+v, want %+v", settings, want)
	}

	// 越界值夹到可用范围，未知主题和转换模式落回默认。
	if err := (Reading{}).SaveSettings(ctx, ReadingSettings{
		Theme:       "sepia",
		FontSize:    99,
		LineHeight:  0.4,
		ColumnWidth: 2000,
		Convert:     "klingon",
	}); err != nil {
		t.Fatal(err)
	}
	settings, err = Reading{}.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != "paper" || settings.Convert != "none" {
		t.Fatalf("fallback settings = %+v", settings)
	}
	if settings.FontSize != 24 {
		t.Errorf("font size = %d, want 24", settings.FontSize)
	}
	if settings.LineHeight != 1.5 {
		t.Errorf("line height = %v, want 1.5", settings.LineHeight)
	}
	if settings.ColumnWidth != 1100 {
		t.Errorf("column width = %d, want 1100", settings.ColumnWidth)
	}

	// 零值当作没填，不把字号压到最小值。
	if err := (Reading{}).SaveSettings(ctx, ReadingSettings{Theme: "day"}); err != nil {
		t.Fatal(err)
	}
	settings, err = Reading{}.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != "day" || settings.FontSize != defaultReadingSettings().FontSize ||
		settings.LineHeight != defaultReadingSettings().LineHeight ||
		settings.ColumnWidth != defaultReadingSettings().ColumnWidth {
		t.Fatalf("zero-value settings = %+v", settings)
	}

	// 坏 JSON 不挡住阅读。
	if err := setSetting(readingSettingsKey, "{not json"); err != nil {
		t.Fatal(err)
	}
	settings, err = Reading{}.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings != defaultReadingSettings() {
		t.Fatalf("malformed settings = %+v", settings)
	}
}

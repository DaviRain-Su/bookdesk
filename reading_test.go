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

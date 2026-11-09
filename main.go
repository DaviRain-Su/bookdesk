package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// 事件在包级声明，mygo generate 才能把它们写进 src/mygo.ts。
var (
	importProgressEvent = mygo.NewEvent[ImportProgress]("library:import-progress")
	acpUpdateEvent      = mygo.NewEvent[AcpUpdate]("acp:update")
	openFileEvent       = mygo.NewEvent[OpenFileEvent]("app:open-file")
	addBookEvent        = mygo.NewEvent[struct{}]("app:add-book")
	addFolderEvent      = mygo.NewEvent[struct{}]("app:add-folder")
	readerModeEvent     = mygo.NewEvent[ReaderModeEvent]("app:reader-mode")
)

var (
	mainWindow      *mygo.Window
	pendingOpenFile string
)

func emitAcpUpdate(win *mygo.Window, turnID, kind, text string) {
	if win == nil {
		return
	}
	_ = acpUpdateEvent.Emit(win, AcpUpdate{TurnID: turnID, Kind: kind, Text: text})
}

// migrateLegacyData 把旧版本「阅读白板」的数据目录和数据库搬到新位置，
// 用户的书和笔记不会丢。
func migrateLegacyData() {
	appData, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		return
	}
	newDir := filepath.Join(appData, "书桌")
	oldDir := filepath.Join(appData, "阅读白板")
	if !fileExists(newDir) && fileExists(oldDir) {
		_ = os.Rename(oldDir, newDir)
	}
	oldDB := filepath.Join(newDir, "reading-canvas.db")
	newDB := filepath.Join(newDir, "bookdesk.db")
	if !fileExists(newDB) && fileExists(oldDB) {
		_ = os.Rename(oldDB, newDB)
	}
}

func emitReaderMode(win *mygo.Window, mode string) {
	if win == nil {
		win = mainWindow
	}
	if win == nil {
		return
	}
	_ = readerModeEvent.Emit(win, ReaderModeEvent{Mode: mode})
}

func buildMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{
			Label: "文件",
			Role:  mygo.RoleFileMenu,
			Submenu: []*mygo.MenuItem{
				{
					ID:          "file.add-book",
					Label:       "添加书籍…",
					Accelerator: "Cmd+O",
					Click: func(item *mygo.MenuItem, win *mygo.Window) {
						if win == nil {
							win = mainWindow
						}
						if win != nil {
							_ = addBookEvent.Emit(win, struct{}{})
						}
					},
				},
				{
					ID:          "file.add-folder",
					Label:       "添加文件夹…",
					Accelerator: "Cmd+Shift+O",
					Click: func(item *mygo.MenuItem, win *mygo.Window) {
						if win == nil {
							win = mainWindow
						}
						if win != nil {
							_ = addFolderEvent.Emit(win, struct{}{})
						}
					},
				},
				mygo.Separator(),
				{Role: mygo.RoleClose},
				{Role: mygo.RoleQuit},
			},
		},
		{Role: mygo.RoleEditMenu},
		{
			Label: "显示",
			Submenu: []*mygo.MenuItem{
				{
					ID:          "view.reader",
					Label:       "全屏阅读",
					Accelerator: "Cmd+Shift+R",
					Click: func(item *mygo.MenuItem, win *mygo.Window) {
						emitReaderMode(win, "reader")
					},
				},
				{
					ID:          "view.desk",
					Label:       "书桌",
					Accelerator: "Cmd+Shift+D",
					Click: func(item *mygo.MenuItem, win *mygo.Window) {
						emitReaderMode(win, "desk")
					},
				},
				mygo.Separator(),
				// 不再放「强制重新载入」：它的 ⌘⇧R 和「全屏阅读」冲突。
				{Role: mygo.RoleReload},
				{Role: mygo.RoleToggleDevTools},
				mygo.Separator(),
				{Role: mygo.RoleResetZoom},
				{Role: mygo.RoleZoomIn},
				{Role: mygo.RoleZoomOut},
				mygo.Separator(),
				{Role: mygo.RoleToggleFullScreen},
			},
		},
		{Role: mygo.RoleWindowMenu},
	})
}

func onReady() {
	migrateLegacyData()
	if err := initPaths(); err != nil {
		log.Fatalf("初始化数据目录失败: %v", err)
	}
	if err := openDB(); err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	migrateTextIndex()

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "书桌",
		URL:             "/",
		Width:           1440,
		Height:          900,
		MinWidth:        960,
		MinHeight:       640,
		BackgroundColor: "#f5f4f0",
	})
	mainWindow = win
	win.OnFileDrop(func(e *mygo.FileDropEvent) {
		if len(e.Paths) > 0 {
			go func() {
				_, _ = importPathsFor(win, e.Paths)
			}()
		}
	})
	if pendingOpenFile != "" {
		path := pendingOpenFile
		pendingOpenFile = ""
		_ = openFileEvent.Emit(win, OpenFileEvent{Path: path})
	}
}

func main() {
	mygo.App.SetName("书桌")
	mygo.Bind(Library{}, Notes{}, Board{}, Search{}, Characters{}, Reading{}, Ai{})
	if err := mygo.Protocol.HandleFunc("books", handleBooksProtocol); err != nil {
		log.Fatalf("注册 books 协议失败: %v", err)
	}

	mygo.App.SetMenu(buildMenu())
	mygo.App.OnOpenFile(func(path string) {
		if path == "" {
			return
		}
		if mainWindow == nil {
			pendingOpenFile = path
			return
		}
		_ = openFileEvent.Emit(mainWindow, OpenFileEvent{Path: path})
	})
	mygo.App.WhenReady(onReady)

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

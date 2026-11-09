package main

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// Share 把阅读分享到读者自己的文件里：一张图片，或一份静态页面。
// 不联网，不上传。笔记只有在调用方明确要求时才会写进结果。
type Share struct{}

// RenderCard 把一条划线画成 PNG，给预览用。图片留在这次调用的返回值里。
func (Share) RenderCard(ctx context.Context, input ShareCardInput) (ShareCardImage, error) {
	pngBytes, err := renderShareCard(input)
	if err != nil {
		return ShareCardImage{}, err
	}
	w, h := cardPixelSize(input.Ratio)
	return ShareCardImage{
		PNGBase64: base64.StdEncoding.EncodeToString(pngBytes),
		Width:     int64(w),
		Height:    int64(h),
	}, nil
}

// SaveCardPNG 弹出保存对话框，把分享卡存成用户选定的 PNG。
func (Share) SaveCardPNG(ctx context.Context, input ShareCardInput) (ShareFileResult, error) {
	pngBytes, err := renderShareCard(input)
	if err != nil {
		return ShareFileResult{}, err
	}
	name := exportBaseName(input.Title) + "-划线.png"
	return saveExportFile(ctx, "保存分享卡", name, "图片只保存在你选的位置，不会上传。", "PNG 图片", "png", pngBytes)
}

// CopyCardPNG 把分享卡的图片放进剪贴板。除此之外不写文件，也不联网。
func (Share) CopyCardPNG(ctx context.Context, input ShareCardInput) error {
	pngBytes, err := renderShareCard(input)
	if err != nil {
		return err
	}
	return mygo.Clipboard.Write(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBytes))))
}

// ListBook 列出一本书可以放进公开页的划线。笔记文字只返回给本机界面，默认不导出。
func (Share) ListBook(ctx context.Context, bookID string) (BookShareList, error) {
	book, items, err := loadBookShare(bookID)
	if err != nil {
		return BookShareList{}, err
	}
	if items == nil {
		items = []BookShareItem{}
	}
	return buildBookShareList(book, items), nil
}

// SaveHTML 把读者勾选的内容写成一个不依赖网络的 HTML 文件。
func (Share) SaveHTML(ctx context.Context, input BookExportInput) (ShareFileResult, error) {
	book, items, err := loadBookShare(input.BookID)
	if err != nil {
		return ShareFileResult{}, err
	}
	doc, _ := renderBookExport(book, items, input.Items, time.Now())
	name := exportBaseName(book.Title) + "-书桌摘录.html"
	return saveExportFile(ctx, "导出公开页", name, "这是一个静态网页，可以放到你自己的网站上。未勾选的笔记不会写进去。", "HTML 网页", "html", []byte(doc))
}

// SaveMarkdown 把同一份勾选写成 Markdown。
func (Share) SaveMarkdown(ctx context.Context, input BookExportInput) (ShareFileResult, error) {
	book, items, err := loadBookShare(input.BookID)
	if err != nil {
		return ShareFileResult{}, err
	}
	_, doc := renderBookExport(book, items, input.Items, time.Now())
	name := exportBaseName(book.Title) + "-书桌摘录.md"
	return saveExportFile(ctx, "导出 Markdown", name, "未勾选的笔记不会写进去。", "Markdown", "md", []byte(doc))
}

func saveExportFile(ctx context.Context, title, name, message, filterName, ext string, data []byte) (ShareFileResult, error) {
	if !strings.HasSuffix(strings.ToLower(name), "."+ext) {
		name += "." + ext
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       title,
		DefaultPath: defaultSavePath(name),
		ButtonLabel: "保存",
		Message:     message,
		Filters:     []mygo.FileFilter{{Name: filterName, Extensions: []string{ext}}},
	})
	if err != nil {
		return ShareFileResult{}, err
	}
	if path == "" {
		return ShareFileResult{Cancelled: true}, nil
	}
	if !strings.HasSuffix(strings.ToLower(path), "."+ext) {
		path += "." + ext
	}
	if err := writeFile(path, data); err != nil {
		return ShareFileResult{}, err
	}
	return ShareFileResult{Path: path}, nil
}

func defaultSavePath(name string) string {
	for _, kind := range []mygo.PathName{mygo.PathDownloads, mygo.PathDesktop, mygo.PathHome} {
		dir, err := mygo.App.Path(kind)
		if err == nil && dir != "" {
			return filepath.Join(dir, name)
		}
	}
	return name
}

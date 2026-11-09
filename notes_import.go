package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
)

// 笔记导入只读用户自己选的文件：微信读书、Kindle、Obsidian 导出的划线
// 和想法都行。不登录任何客户端，不抓正文，也不碰 DRM。

type parsedNote struct {
	BookTitle string
	Author    string
	Chapter   string
	Quote     string
	Note      string
}

func (Notes) PickAndImportNotes(ctx context.Context) (ImportNotesResult, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx),
		Title:  "选择笔记文件",
		Filters: []mygo.FileFilter{
			{Name: "笔记文件", Extensions: []string{"md", "markdown", "txt", "csv", "json"}},
		},
		Multiple: true,
	})
	if err != nil {
		return ImportNotesResult{}, err
	}
	if len(paths) == 0 {
		return ImportNotesResult{Notes: []ImportedNote{}, Errors: []ImportError{}}, nil
	}
	return importNoteFiles(paths)
}

// ImportNotes 给自动化测试和以后可能的拖拽入口用。
func (Notes) ImportNotes(ctx context.Context, paths []string) (ImportNotesResult, error) {
	return importNoteFiles(paths)
}

func importNoteFiles(paths []string) (ImportNotesResult, error) {
	result := ImportNotesResult{Notes: []ImportedNote{}, Errors: []ImportError{}}
	// 只去重同一次导入里的重复条目；和书桌上已有卡片去重放在前端做，
	// 因为卡片是否已经在画布上，后端看不到。
	seen := make(map[string]bool)
	for _, path := range paths {
		notes, err := parseNotesFile(path)
		if err != nil {
			result.Errors = append(result.Errors, ImportError{Path: path, Message: err.Error()})
			continue
		}
		for _, note := range notes {
			result.Total++
			book := matchLibraryBook(note.BookTitle, note.Author)
			var bookID *string
			if book != nil {
				bookID = &book.ID
			}
			key := noteKey(bookID, note.BookTitle, note.Quote, note.Note)
			if key == "" || seen[key] {
				result.Skipped++
				continue
			}
			seen[key] = true
			imported := ImportedNote{
				BookID:    bookID,
				BookTitle: strings.TrimSpace(note.BookTitle),
				Chapter:   optionalText(note.Chapter),
				Quote:     strings.TrimSpace(note.Quote),
				Note:      optionalText(note.Note),
			}
			if author := strings.TrimSpace(note.Author); author != "" {
				imported.Author = &author
			}
			if bookID != nil && imported.Quote != "" {
				imported.Location = locateQuote(*bookID, imported.Quote)
			}
			result.Notes = append(result.Notes, imported)
		}
	}
	return result, nil
}

func parseNotesFile(path string) ([]parsedNote, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.TrimPrefix(string(data), "\ufeff")
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".txt":
		return parseNotesMarkdown(content), nil
	case ".csv":
		return parseNotesCSV(content)
	case ".json":
		return parseNotesJSON(content)
	default:
		return nil, fmt.Errorf("不支持的文件类型：%s", filepath.Ext(path))
	}
}

// parseNotesMarkdown 认一个很宽松的约定：
//
//	# 书名
//	## 章节
//	> 划线原文
//	自己的想法
//
// 没有 # 书名也能用；连续的 > 行会合成一条划线，空行不打断笔记。
func parseNotesMarkdown(content string) []parsedNote {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	notes := make([]parsedNote, 0, 16)
	book, chapter := "", ""
	var quoteLines, noteLines []string
	blankSinceQuote := false

	flush := func() {
		quote := strings.TrimSpace(strings.Join(quoteLines, "\n"))
		note := strings.TrimSpace(strings.Join(noteLines, "\n"))
		if quote != "" || note != "" {
			notes = append(notes, parsedNote{
				BookTitle: book,
				Chapter:   chapter,
				Quote:     quote,
				Note:      note,
			})
		}
		quoteLines, noteLines = nil, nil
		blankSinceQuote = false
	}

	frontMatter := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(raw, " \t"))
		if i == 0 && trimmed == "---" {
			frontMatter = true
			continue
		}
		if frontMatter {
			if trimmed == "---" {
				frontMatter = false
			}
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "# "):
			flush()
			book = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			chapter = ""
		case strings.HasPrefix(trimmed, "## "):
			flush()
			chapter = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "### "):
			flush()
			chapter = strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, ">"):
			// 空行之后又出现引用，说明上一条划线已经结束；
			// 空行之后是普通文字，则那是上一条划线的想法。
			if blankSinceQuote || len(noteLines) > 0 {
				flush()
			}
			line := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
			if line != "" {
				quoteLines = append(quoteLines, line)
			}
		case trimmed == "":
			if len(quoteLines) > 0 {
				blankSinceQuote = true
			}
		default:
			blankSinceQuote = false
			noteLines = append(noteLines, trimmed)
		}
	}
	flush()
	return notes
}

func parseNotesCSV(content string) ([]parsedNote, error) {
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败：%w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	index := make(map[string]int, len(records[0]))
	for i, cell := range records[0] {
		index[normalizeHeader(cell)] = i
	}
	column := func(names ...string) int {
		for _, name := range names {
			if i, ok := index[name]; ok {
				return i
			}
		}
		return -1
	}
	bookCol := column("书名", "书籍", "book", "booktitle", "title")
	authorCol := column("作者", "author")
	chapterCol := column("章节", "章节名", "chapter")
	quoteCol := column("划线", "原文", "引用", "高亮", "quote", "highlight", "text")
	noteCol := column("想法", "笔记", "评论", "批注", "note", "comment", "annotation")
	if quoteCol < 0 && noteCol < 0 {
		return nil, errors.New("CSV 需要「划线」或「想法」列；表头示例：书名,章节,划线,想法")
	}
	notes := make([]parsedNote, 0, len(records)-1)
	for _, record := range records[1:] {
		get := func(i int) string {
			if i < 0 || i >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[i])
		}
		note := parsedNote{
			BookTitle: get(bookCol),
			Author:    get(authorCol),
			Chapter:   get(chapterCol),
			Quote:     get(quoteCol),
			Note:      get(noteCol),
		}
		if note.Quote == "" && note.Note == "" {
			continue
		}
		notes = append(notes, note)
	}
	return notes, nil
}

func parseNotesJSON(content string) ([]parsedNote, error) {
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("JSON 解析失败：%w", err)
	}
	var items []map[string]any
	switch value := raw.(type) {
	case []any:
		for _, item := range value {
			if object, ok := item.(map[string]any); ok {
				items = append(items, object)
			}
		}
	case map[string]any:
		if list, ok := value["notes"].([]any); ok {
			for _, item := range list {
				if object, ok := item.(map[string]any); ok {
					items = append(items, object)
				}
			}
		} else {
			items = append(items, value)
		}
	default:
		return nil, errors.New(`JSON 需要对象数组，或 {"notes": [...]}`)
	}
	notes := make([]parsedNote, 0, len(items))
	for _, item := range items {
		get := func(names ...string) string {
			for _, name := range names {
				for key, value := range item {
					if strings.EqualFold(key, name) {
						if text, ok := value.(string); ok {
							return strings.TrimSpace(text)
						}
					}
				}
			}
			return ""
		}
		note := parsedNote{
			BookTitle: get("book", "bookTitle", "title", "书名", "书籍"),
			Author:    get("author", "作者"),
			Chapter:   get("chapter", "章节", "章节名"),
			Quote:     get("quote", "highlight", "text", "划线", "原文", "引用"),
			Note:      get("note", "comment", "annotation", "想法", "笔记", "批注"),
		}
		if note.Quote == "" && note.Note == "" {
			continue
		}
		notes = append(notes, note)
	}
	return notes, nil
}

func normalizeHeader(cell string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(cell, "\ufeff")))
}

func optionalText(text string) *string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return &text
}

// noteKey 用来去重：同一本书里的同一段划线只导入一次。
func noteKey(bookID *string, bookTitle, quote, note string) string {
	text := normalizeSpace(quote)
	if text == "" {
		text = normalizeSpace(note)
	}
	if text == "" {
		return ""
	}
	if bookID != nil {
		return "id:" + *bookID + "\x00" + text
	}
	return "title:" + normalizeTitle(bookTitle) + "\x00" + text
}

func normalizeSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// normalizeTitle 去掉书名里的书名号、标点和空白，只留用于比较的字。
func normalizeTitle(text string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(text) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		builder.WriteRune(unicode.ToLower(r))
	}
	return builder.String()
}

// matchLibraryBook 先按书名精确匹配，再退到唯一的包含匹配。
func matchLibraryBook(title, author string) *Book {
	needle := normalizeTitle(title)
	if needle == "" {
		return nil
	}
	books, err := listBooks()
	if err != nil {
		return nil
	}
	exact := make([]Book, 0, 2)
	for _, book := range books {
		if normalizeTitle(book.Title) == needle {
			exact = append(exact, book)
		}
	}
	if len(exact) == 1 {
		return &exact[0]
	}
	if len(exact) > 1 {
		if authorKey := normalizeTitle(author); authorKey != "" {
			for _, book := range exact {
				if book.Author != nil && normalizeTitle(*book.Author) == authorKey {
					found := book
					return &found
				}
			}
		}
		found := exact[0]
		return &found
	}
	loose := make([]Book, 0, 2)
	for _, book := range books {
		key := normalizeTitle(book.Title)
		if len([]rune(key)) < 2 {
			continue
		}
		if strings.Contains(needle, key) || strings.Contains(key, needle) {
			loose = append(loose, book)
		}
	}
	if len(loose) == 1 {
		found := loose[0]
		return &found
	}
	return nil
}

// locateQuote 在书的章节正文里找这条划线，找到就记下章节和字节偏移。
// 找不到位置不影响导入，卡片仍然会生成，只是没有「跳回原文」的位置。
func locateQuote(bookID, quote string) *BookLocation {
	text := strings.TrimSpace(quote)
	if text == "" {
		return nil
	}
	chapters, err := allChapters(bookID)
	if err != nil {
		return nil
	}
	probes := []string{text}
	runes := []rune(text)
	if len(runes) > 24 {
		probes = append(probes, string(runes[:24]))
	}
	if len(runes) > 12 {
		probes = append(probes, string(runes[:12]))
	}
	for _, probe := range probes {
		for _, chapter := range chapters {
			offset := indexOfIgnoreCase(chapter.Text, probe)
			if offset < 0 {
				continue
			}
			location := &BookLocation{
				BookID:     bookID,
				SpineIndex: chapter.SpineIndex,
				Href:       chapter.Href,
			}
			charOffset := int64(offset)
			location.CharOffset = &charOffset
			if label := chapterLabel(chapter.Text); label != "" {
				location.PageLabel = &label
			}
			return location
		}
	}
	return nil
}

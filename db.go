package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/sqlite"
)

var (
	dbOnce   sync.Once
	database *sqlite.DB
	dbErr    error
	ftsReady bool
)

func openDB() error {
	dbOnce.Do(func() {
		database, dbErr = sqlite.Open(context.Background(), filepath.Join(userDataDir, "bookdesk.db"), sqlite.OpenOptions{})
		if dbErr != nil {
			return
		}
		dbErr = migrate(database)
	})
	return dbErr
}

func db() (*sqlite.DB, error) {
	if err := openDB(); err != nil {
		return nil, err
	}
	return database, nil
}

func migrate(d *sqlite.DB) error {
	ctx := context.Background()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS books (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  author TEXT,
  language TEXT,
  cover_rel_path TEXT,
  epub_rel_path TEXT NOT NULL,
  imported_at INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS book_text (
  book_id TEXT NOT NULL,
  spine_index INTEGER NOT NULL,
  href TEXT,
  text TEXT NOT NULL,
  PRIMARY KEY (book_id, spine_index)
)`,
		`CREATE TABLE IF NOT EXISTS highlights (
  id TEXT PRIMARY KEY,
  book_id TEXT NOT NULL,
  cfi TEXT,
  spine_index INTEGER NOT NULL DEFAULT 0,
  href TEXT,
  char_offset INTEGER,
  text TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT 'yellow',
  created_at INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS cards (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  title TEXT,
  body TEXT,
  quote TEXT,
  quote_book_id TEXT,
  quote_cfi TEXT,
  quote_spine_index INTEGER,
  quote_href TEXT,
  quote_char_offset INTEGER,
  image_rel_path TEXT,
  source_book_id TEXT,
  character_name TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS whiteboards (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  viewport_json TEXT NOT NULL DEFAULT '{}',
  nodes_json TEXT NOT NULL DEFAULT '[]',
  edges_json TEXT NOT NULL DEFAULT '[]',
  updated_at INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS characters (
  id TEXT PRIMARY KEY,
  book_id TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  image_rel_path TEXT,
  created_at INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
)`,
		"CREATE INDEX IF NOT EXISTS idx_highlights_book ON highlights(book_id)",
		"CREATE INDEX IF NOT EXISTS idx_cards_source ON cards(source_book_id)",
		"CREATE INDEX IF NOT EXISTS idx_characters_book ON characters(book_id)",
		// 旧版本把默认画布叫「我的白板」，跟着新说法改一次。
		"UPDATE whiteboards SET name = '我的画布' WHERE name = '我的白板'",
	}
	for _, sql := range statements {
		if _, err := d.Execute(ctx, sql); err != nil {
			return err
		}
	}

	// 中文按二元组建 FTS5 索引；建不出来时退回 instr 子串搜索。
	if _, err := d.Execute(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS book_text_fts USING fts5(
  search_text,
  book_id UNINDEXED,
  spine_index UNINDEXED,
  tokenize = 'unicode61'
)`); err != nil {
		ftsReady = false
		return nil
	}
	// 版本 3：Go 版第一次启动时重建一次索引。旧版某些情况下会给同一章
	// 写入多条 FTS 记录，搜索结果会重复；重建后每个章节只有一条。
	version, _ := getSettingDB(d, "fts.version")
	if version == nil || *version != "3" {
		// 索引没建完就不要用 FTS，否则会静默漏结果。
		if err := rebuildFTS(d); err != nil {
			ftsReady = false
			return nil
		}
	}
	ftsReady = true
	return nil
}

// ---------- value helpers ----------

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return ""
	}
}

func asOptString(v any) *string {
	if v == nil {
		return nil
	}
	s := asString(v)
	return &s
}

func asInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		return 0
	}
}

func asOptInt(v any) *int64 {
	if v == nil {
		return nil
	}
	n := asInt(v)
	return &n
}

// SQLite 绑定只认 nil / int64 / float64 / string / []byte，指针要先拆开。
func optStringArg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func optIntArg(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// ---------- books ----------

func listBooks() ([]Book, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(),
		"SELECT id, title, author, language, cover_rel_path, epub_rel_path, imported_at FROM books ORDER BY imported_at DESC")
	if err != nil {
		return nil, err
	}
	out := make([]Book, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, Book{
			ID:           asString(row[0]),
			Title:        asString(row[1]),
			Author:       asOptString(row[2]),
			Language:     asOptString(row[3]),
			CoverRelPath: asOptString(row[4]),
			EpubRelPath:  asString(row[5]),
			ImportedAt:   asInt(row[6]),
		})
	}
	return out, nil
}

func getBook(id string) (*Book, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(),
		"SELECT id, title, author, language, cover_rel_path, epub_rel_path, imported_at FROM books WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	row := rows.Values[0]
	return &Book{
		ID:           asString(row[0]),
		Title:        asString(row[1]),
		Author:       asOptString(row[2]),
		Language:     asOptString(row[3]),
		CoverRelPath: asOptString(row[4]),
		EpubRelPath:  asString(row[5]),
		ImportedAt:   asInt(row[6]),
	}, nil
}

func insertBook(book Book) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(),
		"INSERT INTO books (id, title, author, language, cover_rel_path, epub_rel_path, imported_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		book.ID, book.Title, optStringArg(book.Author), optStringArg(book.Language), optStringArg(book.CoverRelPath), book.EpubRelPath, book.ImportedAt)
	return err
}

// ---------- chapters ----------

type ChapterRow struct {
	SpineIndex int64
	Href       string
	Text       string
}

func insertChapters(bookID string, chapters []ChapterRow) error {
	if len(chapters) == 0 {
		return nil
	}
	d, err := db()
	if err != nil {
		return err
	}
	ctx := context.Background()
	var batch []sqlite.Statement
	if ftsReady {
		batch = append(batch, sqlite.Statement{
			SQL:  "DELETE FROM book_text_fts WHERE book_id = ?",
			Args: []any{bookID},
		})
	}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := d.Transaction(ctx, batch)
		batch = batch[:0]
		return err
	}
	for _, ch := range chapters {
		batch = append(batch, sqlite.Statement{
			SQL:  "INSERT INTO book_text (book_id, spine_index, href, text) VALUES (?, ?, ?, ?)",
			Args: []any{bookID, ch.SpineIndex, ch.Href, ch.Text},
		})
		if ftsReady {
			batch = append(batch, sqlite.Statement{
				SQL:  "INSERT INTO book_text_fts (search_text, book_id, spine_index) VALUES (?, ?, ?)",
				Args: []any{searchTextFor(ch.Text), bookID, ch.SpineIndex},
			})
		}
		if len(batch) >= 64 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func chapterText(bookID string, spineIndex int64) (*ChapterText, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(),
		"SELECT spine_index, href, text FROM book_text WHERE book_id = ? AND spine_index = ?", bookID, spineIndex)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	row := rows.Values[0]
	return &ChapterText{
		SpineIndex: asInt(row[0]),
		Href:       asOptString(row[1]),
		Text:       asString(row[2]),
	}, nil
}

// ---------- full-text search ----------

func isWideRune(r rune) bool {
	return (r >= 0x3040 && r <= 0x30FF) || // 日文假名
		(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
		(r >= 0x4E00 && r <= 0x9FFF) || // CJK 基本区
		(r >= 0xF900 && r <= 0xFAFF) || // CJK 兼容区
		(r >= 0xAC00 && r <= 0xD7AF) || // 韩文音节
		(r >= 0x20000 && r <= 0x2FA1F) // CJK 扩展 B 及以后
}

func isWordRune(r rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= 0x00C0 && r <= 0x024F) // 带变音符号的拉丁字母
}

func flushWord(out *strings.Builder, word *strings.Builder) {
	if word.Len() == 0 {
		return
	}
	out.WriteString(word.String())
	out.WriteByte(' ')
	word.Reset()
}

func flushWideTokens(out *strings.Builder, wide *[]rune) {
	if len(*wide) == 0 {
		return
	}
	// 先放单字，再放二元组：单字查询能命中，二元组之间仍然相邻。
	for _, r := range *wide {
		out.WriteRune(r)
		out.WriteByte(' ')
	}
	if len(*wide) >= 2 {
		for i := 0; i < len(*wide)-1; i++ {
			out.WriteRune((*wide)[i])
			out.WriteRune((*wide)[i+1])
			out.WriteByte(' ')
		}
	}
	*wide = (*wide)[:0]
}

// searchTextFor 把正文变成 FTS5 检索文本：英文按词、CJK 按单字 + 相邻二元组。
func searchTextFor(text string) string {
	var out strings.Builder
	var wide []rune
	var word strings.Builder
	for _, r := range text {
		switch {
		case isWideRune(r):
			flushWord(&out, &word)
			wide = append(wide, r)
		case isWordRune(r):
			flushWideTokens(&out, &wide)
			if r >= 'A' && r <= 'Z' {
				word.WriteRune(r - 'A' + 'a')
			} else {
				word.WriteRune(r)
			}
		default:
			flushWord(&out, &word)
			flushWideTokens(&out, &wide)
		}
	}
	flushWord(&out, &word)
	flushWideTokens(&out, &wide)
	return out.String()
}

func flushQueryPhrase(parts *strings.Builder, wide *[]rune) {
	if len(*wide) == 0 {
		return
	}
	if parts.Len() > 0 {
		parts.WriteByte(' ')
	}
	parts.WriteByte('"')
	if len(*wide) == 1 {
		parts.WriteRune((*wide)[0])
	} else {
		for i := 0; i < len(*wide)-1; i++ {
			if i > 0 {
				parts.WriteByte(' ')
			}
			parts.WriteRune((*wide)[i])
			parts.WriteRune((*wide)[i+1])
		}
	}
	parts.WriteByte('"')
	*wide = (*wide)[:0]
}

// ftsQueryFor 只给纯中文查询生成 FTS 查询；中英混合或英文返回 false，走 instr。
func ftsQueryFor(q string) (string, bool) {
	var parts strings.Builder
	var wide []rune
	sawWide := false
	for _, r := range q {
		switch {
		case isWideRune(r):
			sawWide = true
			wide = append(wide, r)
		case isWordRune(r):
			// 中英混合查询保持原来的子串语义。
			return "", false
		default:
			flushQueryPhrase(&parts, &wide)
		}
	}
	flushQueryPhrase(&parts, &wide)
	if !sawWide {
		return "", false
	}
	trimmed := strings.TrimSpace(parts.String())
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func rebuildFTS(d *sqlite.DB) error {
	ctx := context.Background()
	if _, err := d.Execute(ctx, "DELETE FROM book_text_fts"); err != nil {
		return err
	}
	var batch []sqlite.Statement
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := d.Transaction(ctx, batch)
		batch = batch[:0]
		return err
	}
	// 按 rowid 分页，避免把整库正文一次读进内存。
	lastRowID := int64(0)
	for {
		rows, err := d.Query(ctx,
			"SELECT rowid, book_id, spine_index, text FROM book_text WHERE rowid > ? ORDER BY rowid LIMIT 200", lastRowID)
		if err != nil {
			return err
		}
		if len(rows.Values) == 0 {
			break
		}
		for _, row := range rows.Values {
			lastRowID = asInt(row[0])
			bookID := asString(row[1])
			spineIndex := asInt(row[2])
			text := asString(row[3])
			batch = append(batch, sqlite.Statement{
				SQL:  "INSERT INTO book_text_fts (search_text, book_id, spine_index) VALUES (?, ?, ?)",
				Args: []any{searchTextFor(text), bookID, spineIndex},
			})
			if len(batch) >= 200 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return setSettingDB(d, "fts.version", "3")
}

// ---------- highlights ----------

func insertHighlight(h Highlight) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(),
		"INSERT INTO highlights (id, book_id, cfi, spine_index, href, char_offset, text, color, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		h.ID, h.BookID, optStringArg(h.Location.CFI), h.Location.SpineIndex, optStringArg(h.Location.Href), optIntArg(h.Location.CharOffset), h.Text, h.Color, h.CreatedAt)
	return err
}

func scanHighlight(row []any) Highlight {
	return Highlight{
		ID:     asString(row[0]),
		BookID: asString(row[1]),
		Location: BookLocation{
			BookID:     asString(row[1]),
			CFI:        asOptString(row[2]),
			SpineIndex: asInt(row[3]),
			Href:       asOptString(row[4]),
			CharOffset: asOptInt(row[5]),
		},
		Text:      asString(row[6]),
		Color:     asString(row[7]),
		CreatedAt: asInt(row[8]),
	}
}

func listHighlights(bookID *string) ([]Highlight, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	var rows sqlite.Rows
	if bookID != nil {
		rows, err = d.Query(context.Background(),
			"SELECT id, book_id, cfi, spine_index, href, char_offset, text, color, created_at FROM highlights WHERE book_id = ? ORDER BY created_at ASC", *bookID)
	} else {
		rows, err = d.Query(context.Background(),
			"SELECT id, book_id, cfi, spine_index, href, char_offset, text, color, created_at FROM highlights ORDER BY created_at ASC")
	}
	if err != nil {
		return nil, err
	}
	out := make([]Highlight, 0, len(rows.Values))
	for _, row := range rows.Values {
		h := scanHighlight(row)
		if h.Color == "" {
			h.Color = "yellow"
		}
		out = append(out, h)
	}
	return out, nil
}

func findHighlightByCFI(bookID, cfi string) (*Highlight, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(),
		"SELECT id, book_id, cfi, spine_index, href, char_offset, text, color, created_at FROM highlights WHERE book_id = ? AND cfi = ? LIMIT 1", bookID, cfi)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	h := scanHighlight(rows.Values[0])
	if h.Color == "" {
		h.Color = "yellow"
	}
	return &h, nil
}

func deleteHighlight(id string) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), "DELETE FROM highlights WHERE id = ?", id)
	return err
}

// ---------- cards ----------

func upsertCard(card Card) error {
	d, err := db()
	if err != nil {
		return err
	}
	var quoteBookID, quoteCFI, quoteHref any
	var quoteSpineIndex, quoteCharOffset any
	if loc := card.QuoteLocation; loc != nil {
		quoteBookID = loc.BookID
		quoteCFI = optStringArg(loc.CFI)
		quoteSpineIndex = loc.SpineIndex
		quoteHref = optStringArg(loc.Href)
		quoteCharOffset = optIntArg(loc.CharOffset)
	}
	_, err = d.Execute(context.Background(),
		`INSERT INTO cards (id, type, title, body, quote, quote_book_id, quote_cfi, quote_spine_index, quote_href, quote_char_offset, image_rel_path, source_book_id, character_name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET type = excluded.type, title = excluded.title, body = excluded.body, quote = excluded.quote, quote_book_id = excluded.quote_book_id, quote_cfi = excluded.quote_cfi, quote_spine_index = excluded.quote_spine_index, quote_href = excluded.quote_href, quote_char_offset = excluded.quote_char_offset, image_rel_path = excluded.image_rel_path, source_book_id = excluded.source_book_id, character_name = excluded.character_name, updated_at = excluded.updated_at`,
		card.ID, card.Type, optStringArg(card.Title), optStringArg(card.Body), optStringArg(card.Quote), quoteBookID, quoteCFI, quoteSpineIndex, quoteHref, quoteCharOffset,
		optStringArg(card.ImageRelPath), optStringArg(card.SourceBookID), optStringArg(card.CharacterName), card.CreatedAt, card.UpdatedAt)
	return err
}

func listCards() ([]Card, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(),
		"SELECT id, type, title, body, quote, quote_book_id, quote_cfi, quote_spine_index, quote_href, quote_char_offset, image_rel_path, source_book_id, character_name, created_at, updated_at FROM cards ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(rows.Values))
	for _, row := range rows.Values {
		var loc *BookLocation
		if quoteBook := asOptString(row[5]); quoteBook != nil {
			loc = &BookLocation{
				BookID:     *quoteBook,
				CFI:        asOptString(row[6]),
				SpineIndex: asInt(row[7]),
				Href:       asOptString(row[8]),
				CharOffset: asOptInt(row[9]),
			}
		}
		out = append(out, Card{
			ID:            asString(row[0]),
			Type:          asString(row[1]),
			Title:         asOptString(row[2]),
			Body:          asOptString(row[3]),
			Quote:         asOptString(row[4]),
			QuoteLocation: loc,
			ImageRelPath:  asOptString(row[10]),
			SourceBookID:  asOptString(row[11]),
			CharacterName: asOptString(row[12]),
			CreatedAt:     asInt(row[13]),
			UpdatedAt:     asInt(row[14]),
		})
	}
	return out, nil
}

func deleteCard(id string) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), "DELETE FROM cards WHERE id = ?", id)
	return err
}

// ---------- characters ----------

func upsertCharacter(c Character) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(),
		`INSERT INTO characters (id, book_id, name, description, image_rel_path, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET book_id = excluded.book_id, name = excluded.name, description = excluded.description, image_rel_path = excluded.image_rel_path`,
		c.ID, c.BookID, c.Name, optStringArg(c.Description), optStringArg(c.ImageRelPath), c.CreatedAt)
	return err
}

func listCharacters(bookID *string) ([]Character, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	var rows sqlite.Rows
	if bookID != nil {
		rows, err = d.Query(context.Background(),
			"SELECT id, book_id, name, description, image_rel_path, created_at FROM characters WHERE book_id = ? ORDER BY created_at ASC", *bookID)
	} else {
		rows, err = d.Query(context.Background(),
			"SELECT id, book_id, name, description, image_rel_path, created_at FROM characters ORDER BY created_at ASC")
	}
	if err != nil {
		return nil, err
	}
	out := make([]Character, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, Character{
			ID:           asString(row[0]),
			BookID:       asString(row[1]),
			Name:         asString(row[2]),
			Description:  asOptString(row[3]),
			ImageRelPath: asOptString(row[4]),
			CreatedAt:    asInt(row[5]),
		})
	}
	return out, nil
}

func deleteCharacter(id string) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), "DELETE FROM characters WHERE id = ?", id)
	return err
}

// ---------- settings ----------

func getSettingDB(d *sqlite.DB, key string) (*string, error) {
	rows, err := d.Query(context.Background(), "SELECT value FROM settings WHERE key = ?", key)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	value := asString(rows.Values[0][0])
	return &value, nil
}

func setSettingDB(d *sqlite.DB, key, value string) error {
	_, err := d.Execute(context.Background(),
		`INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func getSetting(key string) (*string, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	return getSettingDB(d, key)
}

func setSetting(key, value string) error {
	d, err := db()
	if err != nil {
		return err
	}
	return setSettingDB(d, key, value)
}

func deleteSetting(key string) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), "DELETE FROM settings WHERE key = ?", key)
	return err
}

// deleteSettingsLike 按 LIKE 模式删除设置；pattern 里的 % _ \ 需要调用方自己转义。
func deleteSettingsLike(pattern string) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), `DELETE FROM settings WHERE key LIKE ? ESCAPE '\'`, pattern)
	return err
}

// ---------- chapters for AI context ----------

type ChapterRangeRow struct {
	SpineIndex int64
	Href       *string
	Text       string
}

func chapterRows(query string, args ...any) ([]ChapterRangeRow, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]ChapterRangeRow, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, ChapterRangeRow{
			SpineIndex: asInt(row[0]),
			Href:       asOptString(row[1]),
			Text:       asString(row[2]),
		})
	}
	return out, nil
}

// chaptersBefore 是当前阅读位置之前的章节（不含当前章节）。
func chaptersBefore(bookID string, spineIndex int64) ([]ChapterRangeRow, error) {
	return chapterRows(
		"SELECT spine_index, href, text FROM book_text WHERE book_id = ? AND spine_index < ? ORDER BY spine_index ASC",
		bookID, spineIndex)
}

func allChapters(bookID string) ([]ChapterRangeRow, error) {
	return chapterRows(
		"SELECT spine_index, href, text FROM book_text WHERE book_id = ? ORDER BY spine_index ASC", bookID)
}

// ---------- boards ----------

func listBoards() ([]BoardSummary, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(), "SELECT id, name, updated_at FROM whiteboards ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	out := make([]BoardSummary, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, BoardSummary{
			ID:        asString(row[0]),
			Name:      asString(row[1]),
			UpdatedAt: asInt(row[2]),
		})
	}
	return out, nil
}

func getBoard(id string) (*BoardData, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(context.Background(),
		"SELECT id, name, viewport_json, nodes_json, edges_json, updated_at FROM whiteboards WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	row := rows.Values[0]
	return &BoardData{
		ID:           asString(row[0]),
		Name:         asString(row[1]),
		ViewportJSON: asString(row[2]),
		NodesJSON:    asString(row[3]),
		EdgesJSON:    asString(row[4]),
		UpdatedAt:    asInt(row[5]),
	}, nil
}

func saveBoard(board BoardData) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(),
		`INSERT INTO whiteboards (id, name, viewport_json, nodes_json, edges_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, viewport_json = excluded.viewport_json, nodes_json = excluded.nodes_json, edges_json = excluded.edges_json, updated_at = excluded.updated_at`,
		board.ID, board.Name, board.ViewportJSON, board.NodesJSON, board.EdgesJSON, board.UpdatedAt)
	return err
}

func deleteBoard(id string) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), "DELETE FROM whiteboards WHERE id = ?", id)
	return err
}

func renameBoard(id, name string, updatedAt int64) error {
	d, err := db()
	if err != nil {
		return err
	}
	_, err = d.Execute(context.Background(), "UPDATE whiteboards SET name = ?, updated_at = ? WHERE id = ?", name, updatedAt, id)
	return err
}

// ---------- search ----------

type ChapterSearchRow struct {
	BookID     string
	SpineIndex int64
	Href       *string
	Text       string
}

func chaptersFromRefs(refs [][2]any) ([]ChapterSearchRow, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	out := make([]ChapterSearchRow, 0, len(refs))
	type chapterRef struct {
		book  string
		spine int64
	}
	seen := make(map[chapterRef]bool, len(refs))
	for _, ref := range refs {
		bookID := asString(ref[0])
		spineIndex := asInt(ref[1])
		// 索引万一有重复记录，同一章只取一次，免得结果里出现重复条目。
		key := chapterRef{book: bookID, spine: spineIndex}
		if seen[key] {
			continue
		}
		seen[key] = true
		rows, err := d.Query(context.Background(),
			"SELECT href, text FROM book_text WHERE book_id = ? AND spine_index = ?", bookID, spineIndex)
		if err != nil {
			continue
		}
		if len(rows.Values) == 0 {
			continue
		}
		row := rows.Values[0]
		out = append(out, ChapterSearchRow{
			BookID:     bookID,
			SpineIndex: spineIndex,
			Href:       asOptString(row[0]),
			Text:       asString(row[1]),
		})
	}
	return out, nil
}

func searchChaptersFTS(ftsQuery string, bookID *string) ([]ChapterSearchRow, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	var rows sqlite.Rows
	if bookID != nil {
		rows, err = d.Query(context.Background(),
			"SELECT book_id, spine_index FROM book_text_fts WHERE book_text_fts MATCH ? AND book_id = ? ORDER BY book_id, spine_index LIMIT 200", ftsQuery, *bookID)
	} else {
		rows, err = d.Query(context.Background(),
			"SELECT book_id, spine_index FROM book_text_fts WHERE book_text_fts MATCH ? ORDER BY book_id, spine_index LIMIT 200", ftsQuery)
	}
	if err != nil {
		return nil, err
	}
	refs := make([][2]any, 0, len(rows.Values))
	for _, row := range rows.Values {
		refs = append(refs, [2]any{row[0], row[1]})
	}
	return chaptersFromRefs(refs)
}

func searchChaptersInstr(q string, bookID *string) ([]ChapterSearchRow, error) {
	d, err := db()
	if err != nil {
		return nil, err
	}
	var rows sqlite.Rows
	if bookID != nil {
		rows, err = d.Query(context.Background(),
			"SELECT book_id, spine_index FROM book_text WHERE instr(lower(text), lower(?)) > 0 AND book_id = ? ORDER BY book_id, spine_index LIMIT 200", q, *bookID)
	} else {
		rows, err = d.Query(context.Background(),
			"SELECT book_id, spine_index FROM book_text WHERE instr(lower(text), lower(?)) > 0 ORDER BY book_id, spine_index LIMIT 200", q)
	}
	if err != nil {
		return nil, err
	}
	refs := make([][2]any, 0, len(rows.Values))
	for _, row := range rows.Values {
		refs = append(refs, [2]any{row[0], row[1]})
	}
	return chaptersFromRefs(refs)
}

func searchChapters(q string, bookID *string) ([]ChapterSearchRow, error) {
	if ftsReady {
		if ftsQuery, ok := ftsQueryFor(q); ok {
			if rows, err := searchChaptersFTS(ftsQuery, bookID); err == nil {
				return rows, nil
			}
			// FTS 查询失败时退回子串搜索，至少还能搜到。
		}
	}
	return searchChaptersInstr(q, bookID)
}

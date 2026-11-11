package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// unitMeasure 把 ASCII 算半宽、其余字符算全宽，用来测换行，不依赖字体。
func unitMeasure(s string) float64 {
	w := 0.0
	for _, r := range s {
		if r <= 0x7f {
			w += 0.5
		} else {
			w += 1
		}
	}
	return w
}

func TestWrapEnglishWordsAndCJK(t *testing.T) {
	short, truncated := wrapText("Hello", 20, unitMeasure, 0)
	if truncated || len(short) != 1 || short[0] != "Hello" {
		t.Fatalf("short = %#v truncated=%v", short, truncated)
	}

	lines, truncated := wrapText("Hello world from the reading desk", 8, unitMeasure, 0)
	if truncated {
		t.Fatal("expected no truncation")
	}
	joined := strings.Join(lines, " ")
	if joined != "Hello world from the reading desk" {
		t.Fatalf("joined = %q lines=%#v", joined, lines)
	}
	for _, line := range lines {
		if unitMeasure(line) > 8 {
			t.Fatalf("line %q width %v", line, unitMeasure(line))
		}
		if strings.Contains(line, " ") {
			for _, word := range strings.Fields(line) {
				if !strings.Contains("Hello world from the reading desk", word) {
					t.Fatalf("broke a word: %q", line)
				}
			}
		}
	}

	cjk, _ := wrapText("甲乙丙丁戊己庚辛壬癸", 4, unitMeasure, 0)
	if len(cjk) < 2 {
		t.Fatalf("cjk lines = %#v", cjk)
	}
	for _, line := range cjk {
		if unitMeasure(line) > 4 {
			t.Fatalf("cjk line %q width %v", line, unitMeasure(line))
		}
	}
	if strings.Join(cjk, "") != "甲乙丙丁戊己庚辛壬癸" {
		t.Fatalf("cjk joined = %#v", cjk)
	}
}

func TestWrapKeepsClosingPunctuationOffTheNextLine(t *testing.T) {
	lines, truncated := wrapText("甲乙丙丁戊己庚辛，壬癸", 4, unitMeasure, 0)
	if truncated {
		t.Fatal("truncated")
	}
	for i, line := range lines {
		rs := []rune(line)
		if len(rs) == 0 {
			t.Fatal("empty line")
		}
		if isClosingRune(rs[0]) {
			t.Fatalf("line %d starts with closing punct: %q", i, line)
		}
		if i != len(lines)-1 && isOpeningRune(rs[len(rs)-1]) {
			t.Fatalf("line %d ends with opening punct: %q", i, line)
		}
	}
	if !strings.Contains(strings.Join(lines, "|"), "，") {
		t.Fatalf("lost comma: %#v", lines)
	}

	quoted, _ := wrapText("甲乙「丙丁戊」", 3, unitMeasure, 0)
	for i, line := range quoted {
		rs := []rune(line)
		if i != len(quoted)-1 && len(rs) > 0 && isOpeningRune(rs[len(rs)-1]) {
			t.Fatalf("opening punct left at line end: %#v", quoted)
		}
	}
	if strings.Join(quoted, "") != "甲乙「丙丁戊」" {
		t.Fatalf("quoted = %#v", quoted)
	}
}

func TestWrapTruncatesLongQuote(t *testing.T) {
	text := "one two three four five six seven eight nine ten"
	lines, truncated := wrapText(text, 6, unitMeasure, 2)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if !strings.HasSuffix(lines[1], "…") {
		t.Fatalf("last line = %q", lines[1])
	}
	if unitMeasure(lines[0]) > 6 || unitMeasure(lines[1]) > 6 {
		t.Fatalf("overflow %#v", lines)
	}
	if strings.Contains(lines[0], "…") {
		t.Fatalf("ellipsis on the first line: %#v", lines)
	}
	full, fullCut := wrapText(text, 6, unitMeasure, 0)
	if fullCut || len(full) <= 2 {
		t.Fatalf("unbounded = %#v cut=%v", full, fullCut)
	}
}

func TestSanitizeShareTextAndVerticalPieces(t *testing.T) {
	if got := sanitizeShareText("你\u200b\u200c\u200d\ufeff好\u0000\u007f"); got != "你好" {
		t.Fatalf("sanitize = %q", got)
	}
	lines, _ := wrapText("你\u200b好", 10, unitMeasure, 0)
	if strings.Join(lines, "") != "你好" {
		t.Fatalf("wrapped = %#v", lines)
	}
	cols, _ := layoutVerticalText("a\u0007b\u200b", 1, 4)
	if len(cols) != 1 || cols[0] != "ab" {
		t.Fatalf("vertical sanitize = %#v", cols)
	}

	pieces := groupVerticalColumn("「Hello（世界）。")
	if len(pieces) != 7 {
		t.Fatalf("pieces = %+v", pieces)
	}
	if pieces[0].Latin || pieces[0].Form != '﹁' || pieces[0].Text != "「" {
		t.Fatalf("open quote = %+v", pieces[0])
	}
	if !pieces[1].Latin || pieces[1].Text != "Hello" || pieces[1].Cells != 5 {
		t.Fatalf("latin = %+v", pieces[1])
	}
	if pieces[2].Form != '︵' {
		t.Fatalf("paren = %+v", pieces[2])
	}
	if pieces[3].Text != "世" || pieces[4].Text != "界" || pieces[5].Form != '︶' {
		t.Fatalf("cjk = %+v %+v %+v", pieces[3], pieces[4], pieces[5])
	}
	if !pieces[6].TopRight || pieces[6].Text != "。" {
		t.Fatalf("stop = %+v", pieces[6])
	}
	parens := groupVerticalColumn("（）")
	if parens[0].Form != '︵' || parens[1].Form != '︶' {
		t.Fatalf("parens = %+v", parens)
	}
}

func TestSplitLatinBreaksAtSpaces(t *testing.T) {
	measure := func(s string) float64 { return float64(len([]rune(s))) }
	got := splitLatinToFit("Hello world", 8, 1, measure)
	if got != "Hello" {
		t.Fatalf("split = %q, want Hello", got)
	}
	if splitLatinToFit("Hello world", 20, 1, measure) != "Hello world" {
		t.Fatalf("fitted run was split: %q", splitLatinToFit("Hello world", 20, 1, measure))
	}
}

func TestLatinRunUsesRenderedCells(t *testing.T) {
	measure := func(s string) float64 { return float64(len([]rune(s))) * 0.5 }
	pieces := groupVerticalPieces("Hello, world!", 2, measure)
	if len(pieces) != 1 || !pieces[0].Latin {
		t.Fatalf("pieces = %+v", pieces)
	}
	if pieces[0].Cells >= len([]rune(pieces[0].Text)) {
		t.Fatalf("cells %d still one per letter", pieces[0].Cells)
	}
	cols, truncated := layoutVerticalTextMeasured("Hello, world!世", 2, 6, 2, measure)
	if truncated || len(cols) != 1 || cols[0] != "Hello, world!世" {
		t.Fatalf("word split across columns: trunc=%v cols=%#v", truncated, cols)
	}
	packed := groupVerticalPieces(cols[0], 2, measure)
	if len(packed) != 2 || packed[0].Text != "Hello, world!" || packed[0].Cells >= 13 || packed[1].Text != "世" {
		t.Fatalf("packed = %+v", packed)
	}
	moved, _ := layoutVerticalTextMeasured("甲乙丙丁Hello", 2, 6, 2, measure)
	if len(moved) != 2 || moved[0] != "甲乙丙丁" || moved[1] != "Hello" {
		t.Fatalf("did not keep the word together: %#v", moved)
	}
}

func TestPremultipliedBlend(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{246, 241, 230, 255})
	pm := byte(200 * 128 / 255)
	blendPixel(img, 0, 0, color.RGBA{pm, pm, pm, 128})
	got := img.RGBAAt(0, 0)
	want := 100 + 246*127/255
	if absInt(int(got.R)-want) > 1 || absInt(int(got.G)-want) > 2 || absInt(int(got.B)-(100+230*127/255)) > 2 {
		t.Fatalf("blend = %v, want R about %d", got, want)
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestMissingGlyphsAreNotDrawn(t *testing.T) {
	if _, err := loadShareFonts(); err != nil {
		t.Skip(err)
	}
	plain, err := renderShareCard(ShareCardInput{
		Quote: "你好", Title: "书", Author: "作者", Template: "light", Ratio: "square", Color: "yellow",
	})
	if err != nil {
		t.Fatal(err)
	}
	withMissing, err := renderShareCard(ShareCardInput{
		Quote: "你好\U0001F600\uE000", Title: "书", Author: "作者", Template: "light", Ratio: "square", Color: "yellow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, withMissing) {
		t.Fatal("missing glyph was drawn onto the card")
	}
}

func TestMissingGlyphsAreNotMeasured(t *testing.T) {
	fonts, err := loadShareFonts()
	if err != nil {
		t.Skip(err)
	}
	faces, err := fonts.open(40)
	if err != nil {
		t.Fatal(err)
	}
	defer faces.Close()
	if faces.Width("\u200b\ufeff") != 0 {
		t.Fatal("zero-width characters took space")
	}
	plain := faces.Width("AB")
	withGap := faces.Width("A\u200b\U0001F600B")
	if withGap != plain {
		t.Fatalf("emoji or zero-width changed width: plain %v with %v", plain, withGap)
	}
}

func TestLongChapterStaysInsideMargin(t *testing.T) {
	if _, err := loadShareFonts(); err != nil {
		t.Skip(err)
	}
	raw, err := renderShareCard(ShareCardInput{
		Quote:    "短",
		Title:    "书",
		Author:   "作者",
		Chapter:  strings.Repeat("很长的章节名称", 40),
		Template: "light",
		Ratio:    "square",
		Color:    "yellow",
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	for y := 16; y < b.Dy(); y++ {
		for x := b.Dx() - 24; x < b.Dx(); x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 < 230 || g>>8 < 220 || bl>>8 < 200 {
				t.Fatalf("chapter ran to the edge at %d,%d (%d,%d,%d)", x, y, r>>8, g>>8, bl>>8)
			}
		}
	}
}

func TestVerticalColumnsAndTruncation(t *testing.T) {
	cols, truncated := layoutVerticalText("天地玄黄宇宙洪荒", 2, 4)
	if truncated {
		t.Fatal("exact text should fit")
	}
	if len(cols) != 2 || cols[0] != "天地玄黄" || cols[1] != "宇宙洪荒" {
		t.Fatalf("cols = %#v", cols)
	}

	longCols, cut := layoutVerticalText("天地玄黄宇宙洪荒日月盈昃", 2, 4)
	if !cut {
		t.Fatal("expected truncation")
	}
	if len(longCols) != 2 {
		t.Fatalf("cols = %#v", longCols)
	}
	last := []rune(longCols[1])
	if last[len(last)-1] != '…' {
		t.Fatalf("last column = %q", longCols[1])
	}
	if strings.Contains(longCols[0], "…") {
		t.Fatalf("ellipsis in the first column: %#v", longCols)
	}
}

func TestShareCardLayoutOmitsPrivateNote(t *testing.T) {
	in := ShareCardInput{
		Quote:       "满纸荒唐言，一把辛酸泪。",
		Note:        "这是私密的想法不要出现",
		IncludeNote: false,
		Title:       "红楼梦",
		Author:      "曹雪芹",
		Chapter:     "第一回",
		Template:    "light",
		Ratio:       "square",
	}
	content := layoutShareCardContent(in, shareLayoutLimits{
		MaxQuoteWidth: 10,
		MaxNoteWidth:  10,
		MaxQuoteLines: 6,
		MaxNoteLines:  4,
	}, unitMeasure, unitMeasure)
	if content.IncludeNote || len(content.NoteLines) != 0 || len(content.NoteColumns) != 0 {
		t.Fatalf("note leaked: %+v", content)
	}
	if content.Footer != shareFooter {
		t.Fatalf("footer = %q", content.Footer)
	}
	if !strings.Contains(content.Credit, "红楼梦") || !strings.Contains(content.Credit, "曹雪芹") {
		t.Fatalf("credit = %q", content.Credit)
	}
	if content.Chapter != "第一回" {
		t.Fatalf("chapter = %q", content.Chapter)
	}
	if strings.Join(content.QuoteLines, "") == "" {
		t.Fatal("empty quote")
	}

	in.IncludeNote = true
	withNote := layoutShareCardContent(in, shareLayoutLimits{
		MaxQuoteWidth: 10,
		MaxNoteWidth:  8,
		MaxQuoteLines: 6,
		MaxNoteLines:  3,
	}, unitMeasure, unitMeasure)
	if !withNote.IncludeNote || len(withNote.NoteLines) == 0 {
		t.Fatalf("note missing: %+v", withNote)
	}
	if !strings.Contains(strings.Join(withNote.NoteLines, ""), "私密") {
		t.Fatalf("note lines = %#v", withNote.NoteLines)
	}

	vertical := layoutShareCardContent(in, shareLayoutLimits{
		Vertical:       true,
		MaxColumns:     3,
		MaxRunes:       6,
		NoteMaxColumns: 2,
		NoteMaxRunes:   6,
	}, unitMeasure, unitMeasure)
	if !vertical.Vertical || len(vertical.QuoteColumns) == 0 || len(vertical.NoteColumns) == 0 {
		t.Fatalf("vertical = %+v", vertical)
	}
	if !strings.HasPrefix(vertical.QuoteColumns[0], "满纸") {
		t.Fatalf("first column = %q", vertical.QuoteColumns[0])
	}
}

func TestRenderShareCardPNG(t *testing.T) {
	if _, err := loadShareFonts(); err != nil {
		t.Skip(err)
	}
	base := ShareCardInput{
		Quote:    "满纸荒唐言",
		Title:    "红楼梦",
		Author:   "曹雪芹",
		Chapter:  "第一回",
		Color:    "pink",
		Template: "light",
		Ratio:    "square",
		Note:     "这是私密的想法不要出现",
	}
	plain, err := renderShareCard(base)
	if err != nil {
		t.Fatal(err)
	}
	assertPNGSize(t, plain, 1080, 1080)
	hidden := base
	hidden.IncludeNote = false
	hiddenPNG, err := renderShareCard(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, hiddenPNG) {
		t.Fatal("a note that was not ticked changed the image")
	}
	shown := base
	shown.IncludeNote = true
	shownPNG, err := renderShareCard(shown)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(plain, shownPNG) {
		t.Fatal("ticking the note did not change the image")
	}
	portrait := base
	portrait.Ratio = "3:4"
	portrait.Template = "dark"
	portrait.Quote = strings.Repeat("都云作者痴，谁解其中味。", 18)
	tall, err := renderShareCard(portrait)
	if err != nil {
		t.Fatal(err)
	}
	assertPNGSize(t, tall, 1080, 1440)

	vertical := base
	vertical.Template = "vertical"
	vertical.Ratio = "portrait"
	vertical.Quote = "世事洞明皆学问，人情练达即文章。满纸荒唐言，一把辛酸泪。"
	vert, err := renderShareCard(vertical)
	if err != nil {
		t.Fatal(err)
	}
	assertPNGSize(t, vert, 1080, 1440)
	if bytes.Equal(vert, plain) {
		t.Fatal("vertical card matched the light card")
	}
}

func assertPNGSize(t *testing.T, data []byte, w, h int) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png: %v", err)
	}
	if cfg.Width != w || cfg.Height != h {
		t.Fatalf("size = %dx%d, want %dx%d", cfg.Width, cfg.Height, w, h)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ink := 0
	for y := 0; y < cfg.Height; y += 3 {
		for x := 0; x < cfg.Width; x += 3 {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
				ink++
			}
		}
	}
	if ink < 400 {
		t.Fatalf("image looks blank, ink samples = %d", ink)
	}
}

func TestWriteSampleShareCards(t *testing.T) {
	if _, err := loadShareFonts(); err != nil {
		t.Skip(err)
	}
	dir := os.Getenv("BOOKDESK_SAMPLE_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	longCN := "世事洞明皆学问，人情练达即文章。满纸荒唐言，一把辛酸泪。都云作者痴，谁解其中味。开谈不说红楼梦，读尽诗书也枉然。字字看来皆是血，十年辛苦不寻常。无材可去补苍天，枉入红尘若许年。"
	longEN := "It is a truth universally acknowledged, that a single man in possession of a good fortune, must be in want of a wife. However little known the feelings or views of such a man may be on his first entering a neighbourhood, this truth is so well fixed in the minds of the surrounding families, that he is considered the rightful property of some one or other of their daughters."
	samples := []struct {
		name string
		in   ShareCardInput
	}{
		{"cjk-short-light-square", ShareCardInput{Quote: "满纸荒唐言，一把辛酸泪。", Title: "红楼梦", Author: "曹雪芹", Chapter: "第一回", Color: "yellow", Template: "light", Ratio: "square"}},
		{"cjk-long-dark-square", ShareCardInput{Quote: longCN, Title: "红楼梦", Author: "曹雪芹", Chapter: "第一回", Color: "blue", Template: "dark", Ratio: "square"}},
		{"cjk-long-vertical-portrait", ShareCardInput{Quote: longCN, Note: "这一回先把石头的来历说完，后面才是人间。", IncludeNote: true, Title: "红楼梦", Author: "曹雪芹", Chapter: "第一回", Color: "pink", Template: "vertical", Ratio: "portrait"}},
		{"cjk-short-note-portrait", ShareCardInput{Quote: "都云作者痴，谁解其中味。", Note: "读到这里停了一下。", IncludeNote: true, Title: "红楼梦", Author: "曹雪芹", Chapter: "第一回", Color: "green", Template: "light", Ratio: "3:4"}},
		{"en-short-light-square", ShareCardInput{Quote: "It is a truth universally acknowledged.", Title: "Pride and Prejudice", Author: "Jane Austen", Chapter: "Chapter 1", Color: "purple", Template: "light", Ratio: "square"}},
		{"en-long-dark-portrait", ShareCardInput{Quote: longEN, Title: "Pride and Prejudice", Author: "Jane Austen", Chapter: "Chapter 1", Color: "yellow", Template: "dark", Ratio: "portrait"}},
	}
	for _, sample := range samples {
		pngBytes, err := renderShareCard(sample.in)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, sample.name+".png")
		if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	author := "曹雪芹"
	book := Book{ID: "honglou", Title: "红楼梦", Author: &author}
	items := []BookShareItem{
		{ID: "hl:1", Quote: "满纸荒唐言，一把辛酸泪。", Chapter: "第一回", Note: "这是私密的想法不要出现", Color: "yellow"},
		{ID: "hl:2", Quote: "都云作者痴，谁解其中味。", Chapter: "第一回", Color: "green"},
	}
	page, md := renderBookExport(book, items, []BookExportChoice{{ID: "hl:1"}, {ID: "hl:2"}}, when)
	if err := os.WriteFile(filepath.Join(dir, "public-page.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "public-page.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "私密") || strings.Contains(md, "私密") {
		t.Fatal("sample page included a private note")
	}
}

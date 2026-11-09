package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type sharePalette struct {
	bg, ink, muted, noteBg, noteInk, footer, rule, accent, seal, sealInk color.RGBA
}

func highlightAccent(name string) color.RGBA {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "green":
		return color.RGBA{63, 143, 90, 255}
	case "blue":
		return color.RGBA{61, 120, 180, 255}
	case "pink":
		return color.RGBA{196, 91, 120, 255}
	case "purple":
		return color.RGBA{122, 94, 171, 255}
	default:
		return color.RGBA{196, 148, 46, 255}
	}
}

func accentHex(name string) string {
	c := highlightAccent(name)
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func paletteFor(template string, accent color.RGBA) sharePalette {
	switch normalizeTemplate(template) {
	case "dark":
		return sharePalette{
			bg: color.RGBA{22, 21, 19, 255}, ink: color.RGBA{243, 237, 226, 255},
			muted: color.RGBA{176, 168, 154, 255}, noteBg: color.RGBA{36, 34, 30, 255},
			noteInk: color.RGBA{226, 206, 164, 255}, footer: color.RGBA{138, 132, 120, 255},
			rule: color.RGBA{58, 54, 47, 255}, accent: accent,
			seal: color.RGBA{156, 59, 59, 255}, sealInk: color.RGBA{243, 237, 226, 255},
		}
	case "vertical":
		return sharePalette{
			bg: color.RGBA{244, 236, 216, 255}, ink: color.RGBA{31, 26, 22, 255},
			muted: color.RGBA{110, 96, 80, 255}, noteBg: color.RGBA{236, 226, 204, 255},
			noteInk: color.RGBA{90, 52, 44, 255}, footer: color.RGBA{130, 116, 98, 255},
			rule: color.RGBA{176, 146, 110, 255}, accent: accent,
			seal: color.RGBA{156, 59, 59, 255}, sealInk: color.RGBA{247, 241, 228, 255},
		}
	default:
		return sharePalette{
			bg: color.RGBA{246, 241, 230, 255}, ink: color.RGBA{36, 32, 28, 255},
			muted: color.RGBA{110, 102, 90, 255}, noteBg: color.RGBA{238, 230, 214, 255},
			noteInk: color.RGBA{68, 60, 50, 255}, footer: color.RGBA{154, 146, 132, 255},
			rule: color.RGBA{214, 204, 186, 255}, accent: accent,
			seal: accent, sealInk: color.RGBA{246, 241, 230, 255},
		}
	}
}

// shareFontSet 按顺序试字形：先用同时有中英文的字体，否则拉丁字体画英文、中文字体画汉字。
type shareFontSet struct {
	fonts []*opentype.Font
}

type shareFaces struct {
	faces []font.Face
	size  float64
}

var (
	shareFontOnce sync.Once
	shareFonts    *shareFontSet
	shareFontErr  error
)

func shareFontCandidates() []string {
	if p := strings.TrimSpace(os.Getenv("BOOKDESK_SHARE_FONT")); p != "" {
		return []string{p}
	}
	return []string{
		"/System/Library/Fonts/Supplemental/Songti.ttc",
		"/System/Library/Fonts/Songti.ttc",
		"/Library/Fonts/Songti.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		"/System/Library/Fonts/STHeiti Medium.ttc",
		"/System/Library/Fonts/PingFang.ttc",
		"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSerifCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/liberation/LiberationSerif-Regular.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf",
		"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
	}
}

func loadShareFonts() (*shareFontSet, error) {
	shareFontOnce.Do(func() {
		var both, latin, cjk *opentype.Font
		bothScore, latinScore, cjkScore := -1<<30, -1<<30, -1<<30
		for _, path := range shareFontCandidates() {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			col, err := opentype.ParseCollection(data)
			if err != nil {
				continue
			}
			for i := 0; i < col.NumFonts(); i++ {
				f, err := col.Font(i)
				if err != nil {
					continue
				}
				hasCJK := fontHasRune(f, '书') && fontHasRune(f, '读')
				hasLatin := fontHasRune(f, 'A') && fontHasRune(f, 'g')
				if !hasCJK && !hasLatin {
					continue
				}
				score := fontPreference(f)
				switch {
				case hasCJK && hasLatin && score > bothScore:
					both, bothScore = f, score
				case hasLatin && !hasCJK && score > latinScore:
					latin, latinScore = f, score
				case hasCJK && !hasLatin && score > cjkScore:
					cjk, cjkScore = f, score
				}
			}
		}
		set := &shareFontSet{}
		if both != nil {
			set.fonts = []*opentype.Font{both}
		} else {
			if latin != nil {
				set.fonts = append(set.fonts, latin)
			}
			if cjk != nil {
				set.fonts = append(set.fonts, cjk)
			}
		}
		if cjk == nil && both == nil {
			shareFontErr = fmt.Errorf("找不到可以绘制中文的字体，无法生成分享卡")
			return
		}
		shareFonts = set
	})
	if shareFontErr != nil {
		return nil, shareFontErr
	}
	return shareFonts, nil
}

func fontHasRune(f *opentype.Font, r rune) bool {
	idx, err := f.GlyphIndex(nil, r)
	return err == nil && idx != 0
}

func fontPreference(f *opentype.Font) int {
	name, _ := f.Name(nil, sfnt.NameIDFull)
	lower := strings.ToLower(name)
	score := 0
	if strings.Contains(lower, "regular") || strings.Contains(name, "常规") || strings.Contains(lower, "medium") || strings.Contains(name, "宋体") || strings.Contains(lower, "songti") || strings.Contains(lower, "serif") {
		score += 10
	}
	if strings.Contains(lower, "bold") || strings.Contains(lower, "black") || strings.Contains(lower, "thin") || strings.Contains(name, "细") {
		score -= 4
	}
	if strings.Contains(lower, "mono") || strings.Contains(name, "等宽") {
		score -= 20
	}
	return score
}

func (s *shareFontSet) open(size float64) (*shareFaces, error) {
	out := &shareFaces{size: size}
	for _, f := range s.fonts {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingNone,
		})
		if err != nil {
			continue
		}
		out.faces = append(out.faces, face)
	}
	if len(out.faces) == 0 {
		return nil, fmt.Errorf("字体打不开")
	}
	return out, nil
}

func (s *shareFaces) Close() {
	for _, face := range s.faces {
		_ = face.Close()
	}
}

func (s *shareFaces) faceFor(r rune) (font.Face, bool) {
	for _, face := range s.faces {
		if _, ok := face.GlyphAdvance(r); ok {
			return face, true
		}
	}
	return nil, false
}

func (s *shareFaces) Width(text string) float64 {
	text = sanitizeShareText(text)
	width := fixed.Int26_6(0)
	var prev font.Face
	var prevR rune
	for _, r := range text {
		face, ok := s.faceFor(r)
		if !ok {
			continue
		}
		if prev == face {
			width += face.Kern(prevR, r)
		}
		adv, ok := face.GlyphAdvance(r)
		if !ok || adv <= 0 {
			continue
		}
		width += adv
		prev, prevR = face, r
	}
	return float64(width.Round())
}

func (s *shareFaces) ascent() int {
	max := 1
	for _, face := range s.faces {
		if a := face.Metrics().Ascent.Ceil(); a > max {
			max = a
		}
	}
	return max
}

func (s *shareFaces) lineHeight(extra int) int {
	max := 1
	for _, face := range s.faces {
		m := face.Metrics()
		h := m.Ascent.Ceil() + m.Descent.Ceil() + extra
		if h > max {
			max = h
		}
	}
	return max
}

func renderShareCard(in ShareCardInput) ([]byte, error) {
	fonts, err := loadShareFonts()
	if err != nil {
		return nil, err
	}
	w, h := cardPixelSize(in.Ratio)
	vertical := normalizeTemplate(in.Template) == "vertical"
	quoteSize, noteSize, metaSize, footerSize := 46.0, 28.0, 26.0, 20.0
	if vertical {
		quoteSize, noteSize = 40, 30
	}
	qFace, err := fonts.open(quoteSize)
	if err != nil {
		return nil, err
	}
	defer qFace.Close()
	nFace, err := fonts.open(noteSize)
	if err != nil {
		return nil, err
	}
	defer nFace.Close()
	mFace, err := fonts.open(metaSize)
	if err != nil {
		return nil, err
	}
	defer mFace.Close()
	fFace, err := fonts.open(footerSize)
	if err != nil {
		return nil, err
	}
	defer fFace.Close()
	kFace, err := fonts.open(22)
	if err != nil {
		return nil, err
	}
	defer kFace.Close()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	pal := paletteFor(in.Template, highlightAccent(in.Color))
	fillRect(img, 0, 0, w, h, pal.bg)

	var content ShareCardContent
	if vertical {
		content = layoutVerticalCard(in, w, h, qFace, nFace)
		paintVerticalCard(img, content, pal, qFace, nFace, mFace, fFace, kFace)
	} else {
		content = layoutHorizontalCard(in, w, h, qFace, nFace, mFace, fFace)
		paintHorizontalCard(img, content, pal, qFace, nFace, mFace, fFace, kFace)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func layoutHorizontalCard(in ShareCardInput, w, h int, qFace, nFace, mFace, fFace *shareFaces) ShareCardContent {
	padX := 88
	textW := float64(w - padX*2)
	top := 96
	qLH := qFace.lineHeight(18)
	nLH := nFace.lineHeight(10)
	metaLH := mFace.lineHeight(8)
	chapter := strings.TrimSpace(in.Chapter)
	footBaseline := h - 52
	bottom := (h - footBaseline) + fFace.ascent() + 18 + 20 + metaLH
	if chapter != "" {
		bottom += metaLH
	}
	include := in.IncludeNote && strings.TrimSpace(in.Note) != ""
	maxNote := 4
	if normalizeRatio(in.Ratio) == "portrait" {
		maxNote = 5
	}
	noteH := 0
	if include {
		noteLines, _ := wrapText(strings.TrimSpace(in.Note), textW-36, nFace.Width, maxNote)
		if len(noteLines) > 0 {
			noteH = len(noteLines)*nLH + 44 + 28
		}
	}
	maxQuote := (h - top - bottom - noteH) / qLH
	if maxQuote < 2 {
		maxQuote = 2
	}
	return layoutShareCardContent(in, shareLayoutLimits{
		MaxQuoteWidth: textW,
		MaxNoteWidth:  textW - 36,
		MaxQuoteLines: maxQuote,
		MaxNoteLines:  maxNote,
	}, qFace.Width, nFace.Width)
}

func layoutVerticalCard(in ShareCardInput, w, h int, qFace, nFace *shareFaces) ShareCardContent {
	qLH := qFace.lineHeight(8)
	if qLH < 36 {
		qLH = 36
	}
	nLH := nFace.lineHeight(6)
	if nLH < 28 {
		nLH = 28
	}
	top := 72
	band := 148
	colPitch := 64
	right := w - 86
	left := 72
	maxRunes := (h - band - top) / qLH
	if maxRunes < 4 {
		maxRunes = 4
	}
	maxCols := (right - left) / colPitch
	if maxCols < 1 {
		maxCols = 1
	}
	include := in.IncludeNote && strings.TrimSpace(in.Note) != ""
	noteCols := 0
	if include {
		noteCols = 2
		if maxCols > 6 {
			noteCols = 3
		}
		if maxCols-noteCols < 2 {
			noteCols = maxCols / 2
			if noteCols < 1 {
				noteCols = 1
			}
		}
		maxCols -= noteCols
		if maxCols < 1 {
			maxCols = 1
		}
	}
	noteRunes := (h - band - top) / nLH
	if noteRunes < 4 {
		noteRunes = 4
	}
	return layoutShareCardContent(in, shareLayoutLimits{
		Vertical:       true,
		MaxColumns:     maxCols,
		MaxRunes:       maxRunes,
		NoteMaxColumns: noteCols,
		NoteMaxRunes:   noteRunes,
	}, nil, nil)
}

func paintHorizontalCard(img *image.RGBA, content ShareCardContent, pal sharePalette, qFace, nFace, mFace, fFace, kFace *shareFaces) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	padX := 88
	fillRect(img, 0, 0, w, 10, pal.accent)
	drawText(img, kFace, pal.accent, "摘录", padX, 62)

	qLH := qFace.lineHeight(18)
	y := 96
	ascent := qFace.ascent()
	for _, line := range content.QuoteLines {
		drawText(img, qFace, pal.ink, line, padX, y+ascent)
		y += qLH
	}
	if content.IncludeNote && len(content.NoteLines) > 0 {
		nLH := nFace.lineHeight(10)
		y += 16
		boxH := len(content.NoteLines)*nLH + 36
		fillRect(img, padX, y, w-padX, y+boxH, pal.noteBg)
		fillRect(img, padX, y, padX+4, y+boxH, pal.accent)
		ny := y + 18
		na := nFace.ascent()
		for _, line := range content.NoteLines {
			drawText(img, nFace, pal.noteInk, line, padX+22, ny+na)
			ny += nLH
		}
	}

	footBaseline := h - 52
	drawText(img, fFace, pal.footer, content.Footer, padX, footBaseline)
	ruleY := footBaseline - fFace.ascent() - 16
	fillRect(img, padX, ruleY, w-padX, ruleY+1, pal.rule)
	metaLH := mFace.lineHeight(8)
	creditBaseline := ruleY - 18
	if content.Chapter != "" {
		ch, _ := wrapText(content.Chapter, float64(w-padX*2), mFace.Width, 1)
		if len(ch) > 0 {
			drawText(img, mFace, pal.muted, ch[0], padX, creditBaseline)
			creditBaseline -= metaLH
		}
	}
	creditLines, _ := wrapText(content.Credit, float64(w-padX*2), mFace.Width, 2)
	for i := len(creditLines) - 1; i >= 0; i-- {
		drawText(img, mFace, pal.ink, creditLines[i], padX, creditBaseline)
		creditBaseline -= metaLH
	}
}

func paintVerticalCard(img *image.RGBA, content ShareCardContent, pal sharePalette, qFace, nFace, mFace, fFace, kFace *shareFaces) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	fillRect(img, w-36, 32, w-35, h-32, pal.rule)
	fillRect(img, w-44, 32, w-43, h-32, pal.rule)

	qLH := qFace.lineHeight(8)
	if qLH < 36 {
		qLH = 36
	}
	nLH := nFace.lineHeight(6)
	if nLH < 28 {
		nLH = 28
	}
	colPitch := 64
	top := 72
	right := w - 86
	paintVerticalColumns(img, qFace, pal.ink, content.QuoteColumns, right, top, colPitch, qLH, qFace.ascent())
	if content.IncludeNote && len(content.NoteColumns) > 0 {
		noteRight := right - len(content.QuoteColumns)*colPitch - 36
		paintVerticalColumns(img, nFace, pal.noteInk, content.NoteColumns, noteRight, top, colPitch, nLH, nFace.ascent())
	}

	seal := 58
	sx, sy := 64, h-seal-58
	fillRect(img, sx, sy, sx+seal, sy+seal, pal.seal)
	sealText := "读"
	sw := int(kFace.Width(sealText))
	drawText(img, kFace, pal.sealInk, sealText, sx+(seal-sw)/2, sy+40)

	textX := sx + seal + 22
	textW := float64(w - textX - 56)
	footBaseline := h - 48
	drawText(img, fFace, pal.footer, content.Footer, textX, footBaseline)
	metaLH := mFace.lineHeight(6)
	creditBaseline := footBaseline - fFace.ascent() - 20
	if content.Chapter != "" {
		ch, _ := wrapText(content.Chapter, textW, mFace.Width, 1)
		if len(ch) > 0 {
			drawText(img, mFace, pal.muted, ch[0], textX, creditBaseline)
			creditBaseline -= metaLH
		}
	}
	creditLines, _ := wrapText(content.Credit, textW, mFace.Width, 2)
	for i := len(creditLines) - 1; i >= 0; i-- {
		drawText(img, mFace, pal.ink, creditLines[i], textX, creditBaseline)
		creditBaseline -= metaLH
	}
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1 <= x0 || y1 <= y0 {
		return
	}
	draw.Draw(img, image.Rect(x0, y0, x1, y1), image.NewUniform(c), image.Point{}, draw.Src)
}

func paintVerticalColumns(img *image.RGBA, faces *shareFaces, col color.Color, columns []string, right, top, colPitch, lineH, ascent int) {
	for i, column := range columns {
		x := right - (i+1)*colPitch
		row := 0
		for _, piece := range groupVerticalColumn(column) {
			if piece.Latin {
				drawRotatedLatin(img, faces, col, piece.Text, x, top+row*lineH, colPitch, piece.Cells*lineH, ascent)
				row += piece.Cells
				continue
			}
			r, _ := utf8.DecodeRuneInString(piece.Text)
			drawR := r
			if piece.Form != 0 {
				if _, ok := faces.faceFor(piece.Form); ok {
					drawR = piece.Form
				}
			}
			dx, dy := 0, 0
			if piece.TopRight && drawR == r {
				dx = colPitch / 3
				dy = -lineH / 3
			}
			if _, ok := faces.faceFor(drawR); ok {
				drawText(img, faces, col, string(drawR), x+dx, top+ascent+row*lineH+dy)
			}
			row += piece.Cells
		}
	}
}

// drawRotatedLatin 把一串拉丁字母顺时针转 90 度，竖排里就能从上往下读，而不是一个字母直立一格。
func drawRotatedLatin(img *image.RGBA, faces *shareFaces, col color.Color, text string, cellX, cellTop, cellW, cellH, ascent int) {
	text = strings.TrimSpace(sanitizeShareText(text))
	if text == "" || cellW <= 0 || cellH <= 0 {
		return
	}
	width := int(faces.Width(text)) + 2
	height := faces.lineHeight(0)
	if width < 2 {
		width = 2
	}
	if height < ascent+2 {
		height = ascent + 2
	}
	tmp := image.NewRGBA(image.Rect(0, 0, width, height))
	drawText(tmp, faces, col, text, 1, ascent)
	// 顺时针 90°：词首到列顶，字的上沿朝右，基线留在左侧。
	originY := cellTop
	rotW := height
	originX := cellX + (cellW-rotW)/2
	if originX < cellX {
		originX = cellX
	}
	for sy := 0; sy < height; sy++ {
		for sx := 0; sx < width; sx++ {
			px := tmp.RGBAAt(sx, sy)
			if px.A == 0 {
				continue
			}
			dx := originX + (height - 1 - sy)
			dy := originY + sx
			if dy >= cellTop+cellH {
				continue
			}
			blendPixel(img, dx, dy, px)
		}
	}
}

func blendPixel(img *image.RGBA, x, y int, src color.RGBA) {
	if !image.Pt(x, y).In(img.Bounds()) || src.A == 0 {
		return
	}
	if src.A == 255 {
		img.SetRGBA(x, y, src)
		return
	}
	dst := img.RGBAAt(x, y)
	a := uint32(src.A)
	inv := 255 - a
	img.SetRGBA(x, y, color.RGBA{
		R: uint8((uint32(src.R)*a + uint32(dst.R)*inv) / 255),
		G: uint8((uint32(src.G)*a + uint32(dst.G)*inv) / 255),
		B: uint8((uint32(src.B)*a + uint32(dst.B)*inv) / 255),
		A: 255,
	})
}

func drawText(img *image.RGBA, faces *shareFaces, col color.Color, text string, x, baseline int) {
	if text == "" || faces == nil || len(faces.faces) == 0 {
		return
	}
	text = sanitizeShareText(text)
	dot := fixed.P(x, baseline)
	var prev font.Face
	var prevR rune
	src := image.NewUniform(col)
	for _, r := range text {
		face, ok := faces.faceFor(r)
		if !ok {
			continue
		}
		if prev == face {
			dot.X += face.Kern(prevR, r)
		}
		d := &font.Drawer{Dst: img, Src: src, Face: face, Dot: dot}
		d.DrawString(string(r))
		dot = d.Dot
		prev, prevR = face, r
	}
}

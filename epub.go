package main

import (
	"archive/zip"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
)

// EPUB 解析：只取元数据、封面和每章正文，够阅读、搜索和 AI 上下文用。
// 解析器按标签扫描而不是严格 XML：很多 EPUB 的 XHTML 并不完全合法。

type epubChapter struct {
	// SpineIndex 是这一章在 EPUB spine 里的真实位置。目录页、封面页
	// 等不进索引的文档会被跳过，所以不能用切片下标代替。
	SpineIndex int64
	Href       string
	Text       string
}

type epubParsed struct {
	Title      string
	Author     *string
	Language   *string
	CoverBytes []byte
	CoverExt   string
	Chapters   []epubChapter
}

var (
	errNotEpub          = errors.New("not an epub")
	errEncrypted        = errors.New("encrypted")
	errMissingContainer = errors.New("missing container")
	errMissingOPF       = errors.New("missing opf")
	errBadPackage       = errors.New("bad package")
)

const maxEntryBytes = 64 * 1024 * 1024

type xmlTag struct {
	name         string
	attrs        string
	closing      bool
	selfClosing  bool
	start        int
	contentStart int
}

func nextTag(xml string, from int) (xmlTag, bool) {
	i := from
	for i < len(xml) {
		rel := strings.IndexByte(xml[i:], '<')
		if rel < 0 {
			return xmlTag{}, false
		}
		pos := i + rel
		if pos+1 >= len(xml) {
			return xmlTag{}, false
		}
		rest := xml[pos:]
		if strings.HasPrefix(rest, "<!--") {
			end := strings.Index(xml[pos+4:], "-->")
			if end < 0 {
				return xmlTag{}, false
			}
			i = pos + 4 + end + 3
			continue
		}
		if strings.HasPrefix(rest, "<?") {
			end := strings.Index(xml[pos+2:], "?>")
			if end < 0 {
				return xmlTag{}, false
			}
			i = pos + 2 + end + 2
			continue
		}
		if strings.HasPrefix(rest, "<![CDATA[") {
			end := strings.Index(xml[pos+9:], "]]>")
			if end < 0 {
				return xmlTag{}, false
			}
			i = pos + 9 + end + 3
			continue
		}
		gt := strings.IndexByte(xml[pos+1:], '>')
		if gt < 0 {
			return xmlTag{}, false
		}
		gtPos := pos + 1 + gt
		inner := xml[pos+1 : gtPos]
		closing := len(inner) > 0 && inner[0] == '/'
		if closing {
			inner = inner[1:]
		}
		selfClosing := false
		if len(inner) > 0 && inner[len(inner)-1] == '/' {
			selfClosing = true
			inner = inner[:len(inner)-1]
		}
		nameEnd := 0
		for nameEnd < len(inner) && !isSpace(inner[nameEnd]) {
			nameEnd++
		}
		return xmlTag{
			name:         inner[:nameEnd],
			attrs:        inner[nameEnd:],
			closing:      closing,
			selfClosing:  selfClosing,
			start:        pos,
			contentStart: gtPos + 1,
		}, true
	}
	return xmlTag{}, false
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func localName(name string) string {
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func getAttr(attrs, name string) (string, bool) {
	i := 0
	for i < len(attrs) {
		for i < len(attrs) && isSpace(attrs[i]) {
			i++
		}
		if i >= len(attrs) {
			break
		}
		keyStart := i
		for i < len(attrs) && attrs[i] != '=' && !isSpace(attrs[i]) {
			i++
		}
		key := attrs[keyStart:i]
		for i < len(attrs) && isSpace(attrs[i]) {
			i++
		}
		if i >= len(attrs) || attrs[i] != '=' {
			continue
		}
		i++
		for i < len(attrs) && isSpace(attrs[i]) {
			i++
		}
		if i >= len(attrs) {
			break
		}
		quote := attrs[i]
		if quote != '"' && quote != '\'' {
			continue
		}
		i++
		valStart := i
		for i < len(attrs) && attrs[i] != quote {
			i++
		}
		val := attrs[valStart:i]
		if i < len(attrs) {
			i++
		}
		if key == name {
			return val, true
		}
	}
	return "", false
}

func decodeEntity(ent string) (rune, bool) {
	if ent == "" {
		return 0, false
	}
	if ent[0] == '#' {
		base := 10
		digits := ent[1:]
		if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
			base = 16
			digits = digits[1:]
		}
		if digits == "" {
			return 0, false
		}
		value, err := strconv.ParseUint(digits, base, 32)
		if err != nil || value > 0x10FFFF {
			return 0, false
		}
		return rune(value), true
	}
	named := map[string]rune{
		"amp":    '&',
		"lt":     '<',
		"gt":     '>',
		"quot":   '"',
		"apos":   '\'',
		"nbsp":   0xA0,
		"mdash":  0x2014,
		"ndash":  0x2013,
		"hellip": 0x2026,
		"ldquo":  0x201C,
		"rdquo":  0x201D,
		"lsquo":  0x2018,
		"rsquo":  0x2019,
		"middot": 0xB7,
	}
	if r, ok := named[ent]; ok {
		return r, true
	}
	return 0, false
}

func appendDecoded(b *strings.Builder, s string) {
	i := 0
	for i < len(s) {
		if s[i] == '&' {
			if rel := strings.IndexByte(s[i:], ';'); rel >= 0 {
				semi := i + rel
				if r, ok := decodeEntity(s[i+1 : semi]); ok {
					b.WriteRune(r)
					i = semi + 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
}

func elementText(xml, wanted string) *string {
	i := 0
	for {
		tag, ok := nextTag(xml, i)
		if !ok {
			return nil
		}
		i = tag.contentStart
		if tag.closing || tag.selfClosing {
			continue
		}
		if localName(tag.name) != wanted {
			continue
		}
		// 找到这个元素对应的结束标签。
		j := tag.contentStart
		depth := 1
		textEnd := len(xml)
		for {
			t, ok := nextTag(xml, j)
			if !ok {
				break
			}
			if localName(t.name) == wanted {
				if t.closing {
					depth--
					if depth == 0 {
						textEnd = t.start
						break
					}
				} else if !t.selfClosing {
					depth++
				}
			}
			j = t.contentStart
		}
		var out strings.Builder
		k := tag.contentStart
		for k < textEnd {
			if xml[k] == '<' {
				t, ok := nextTag(xml, k)
				if !ok {
					break
				}
				k = t.contentStart
			} else {
				lt := strings.IndexByte(xml[k:], '<')
				end := textEnd
				if lt >= 0 && k+lt < textEnd {
					end = k + lt
				}
				appendDecoded(&out, xml[k:end])
				k = end
			}
		}
		trimmed := strings.TrimSpace(out.String())
		return &trimmed
	}
}

func isBlockTag(name string) bool {
	switch strings.ToLower(name) {
	case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr",
		"blockquote", "section", "article", "header", "footer", "pre",
		"figure", "figcaption", "dd", "dt":
		return true
	}
	return false
}

func htmlToText(html string) string {
	var out strings.Builder
	i := 0
	skip := ""
	for i < len(html) {
		if html[i] == '<' {
			tag, ok := nextTag(html, i)
			if !ok {
				break
			}
			ln := localName(tag.name)
			if skip != "" {
				if tag.closing && strings.EqualFold(ln, skip) {
					skip = ""
				}
			} else if !tag.closing && !tag.selfClosing &&
				(strings.EqualFold(ln, "head") || strings.EqualFold(ln, "script") || strings.EqualFold(ln, "style")) {
				skip = ln
			} else if !tag.closing && isBlockTag(ln) {
				out.WriteByte('\n')
			}
			i = tag.contentStart
		} else {
			rest := html[i:]
			lt := strings.IndexByte(rest, '<')
			if lt < 0 {
				lt = len(rest)
			}
			if skip == "" {
				appendDecoded(&out, rest[:lt])
			}
			i += lt
		}
	}
	return normalizeWhitespace(out.String())
}

// isNavDocument 判断一个 XHTML 是不是目录页。目录页不进搜索索引，
// 否则搜「第一章」这类词会先命中目录，而不是正文。
func isNavDocument(html string) bool {
	lower := strings.ToLower(html)
	i := 0
	for {
		pos := strings.IndexByte(lower[i:], '<')
		if pos < 0 {
			return false
		}
		tag, ok := nextTag(lower, i+pos)
		if !ok {
			return false
		}
		i = tag.contentStart
		if tag.closing {
			continue
		}
		ln := localName(tag.name)
		if ln != "nav" && ln != "body" && ln != "html" {
			continue
		}
		if v, ok := getAttr(tag.attrs, "epub:type"); ok && hasToken(v, "toc") {
			return true
		}
		if v, ok := getAttr(tag.attrs, "role"); ok && strings.EqualFold(strings.TrimSpace(v), "doc-toc") {
			return true
		}
	}
}

func hasToken(list, token string) bool {
	for _, part := range strings.Fields(list) {
		if strings.EqualFold(part, token) {
			return true
		}
	}
	return false
}

func normalizeWhitespace(raw string) string {
	out := make([]byte, 0, len(raw))
	lastSpace := false
	newlines := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\r' {
			continue
		}
		if c == '\n' {
			if len(out) == 0 {
				continue
			}
			for len(out) > 0 && out[len(out)-1] == ' ' {
				out = out[:len(out)-1]
			}
			newlines++
			if newlines <= 2 {
				out = append(out, '\n')
			}
			lastSpace = false
		} else if c == ' ' || c == '\t' {
			if !lastSpace && len(out) > 0 && out[len(out)-1] != '\n' {
				out = append(out, ' ')
			}
			lastSpace = true
		} else {
			out = append(out, c)
			lastSpace = false
			newlines = 0
		}
	}
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == ' ') {
		out = out[:len(out)-1]
	}
	return string(out)
}

func percentDecode(s string) string {
	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if s[i] == '%' && i+2 < len(s) {
			hi, err1 := strconv.ParseUint(string(s[i+1]), 16, 8)
			lo, err2 := strconv.ParseUint(string(s[i+2]), 16, 8)
			if err1 == nil && err2 == nil {
				out = append(out, byte(hi*16+lo))
				i += 3
				continue
			}
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

func normalizePath(p string) string {
	stack := make([]string, 0, 8)
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		stack = append(stack, seg)
	}
	return strings.Join(stack, "/")
}

func resolveHref(opfPath, href string) string {
	clean := href
	if i := strings.IndexByte(clean, '#'); i >= 0 {
		clean = clean[:i]
	}
	if i := strings.IndexByte(clean, '?'); i >= 0 {
		clean = clean[:i]
	}
	decoded := percentDecode(clean)
	if strings.HasPrefix(decoded, "/") {
		return normalizePath(decoded[1:])
	}
	dir := path.Dir(opfPath)
	if dir == "." {
		dir = ""
	}
	return normalizePath(dir + "/" + decoded)
}

type manifestItem struct {
	id         string
	href       string
	mediaType  string
	properties string
}

func coverExtension(mediaType, href string) string {
	switch mediaType {
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	}
	ext := strings.ToLower(path.Ext(href))
	switch ext {
	case ".png":
		return "png"
	case ".gif":
		return "gif"
	case ".webp":
		return "webp"
	case ".svg":
		return "svg"
	}
	return "jpg"
}

func readZipEntry(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxEntryBytes {
		return nil, errors.New("entry too large")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, maxEntryBytes))
}

func parseEpub(filePath string) (*epubParsed, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, errNotEpub
	}
	defer reader.Close()

	entries := make(map[string]*zip.File, len(reader.File))
	for _, f := range reader.File {
		entries[f.Name] = f
	}

	if enc, ok := entries["META-INF/encryption.xml"]; ok {
		if data, err := readZipEntry(enc); err == nil {
			if strings.Contains(string(data), "EncryptedData") {
				return nil, errEncrypted
			}
		}
	}

	containerFile, ok := entries["META-INF/container.xml"]
	if !ok {
		return nil, errMissingContainer
	}
	container, err := readZipEntry(containerFile)
	if err != nil {
		return nil, errMissingContainer
	}
	opfPath := ""
	i := 0
	for {
		tag, ok := nextTag(string(container), i)
		if !ok {
			break
		}
		i = tag.contentStart
		if !tag.closing && localName(tag.name) == "rootfile" {
			if p, ok := getAttr(tag.attrs, "full-path"); ok {
				opfPath = percentDecode(p)
				break
			}
		}
	}
	if opfPath == "" {
		return nil, errMissingContainer
	}

	opfFile, ok := entries[opfPath]
	if !ok {
		return nil, errMissingOPF
	}
	opfBytes, err := readZipEntry(opfFile)
	if err != nil {
		return nil, errMissingOPF
	}
	opf := string(opfBytes)

	title := "未命名书籍"
	if t := elementText(opf, "title"); t != nil {
		title = *t
	}
	author := elementText(opf, "creator")
	language := elementText(opf, "language")

	var manifest []manifestItem
	var spineIDs []string
	var coverID string
	var coverHrefDirect string
	// guide 里标成目录 / 封面 / 扉页的文档不进搜索索引。
	skipRefs := make(map[string]bool)

	i = 0
	for {
		tag, ok := nextTag(opf, i)
		if !ok {
			break
		}
		i = tag.contentStart
		if tag.closing {
			continue
		}
		ln := localName(tag.name)
		switch ln {
		case "item":
			id, ok := getAttr(tag.attrs, "id")
			if !ok {
				continue
			}
			href, ok := getAttr(tag.attrs, "href")
			if !ok {
				continue
			}
			mediaType, _ := getAttr(tag.attrs, "media-type")
			properties, _ := getAttr(tag.attrs, "properties")
			manifest = append(manifest, manifestItem{
				id:         id,
				href:       resolveHref(opfPath, href),
				mediaType:  mediaType,
				properties: properties,
			})
		case "itemref":
			idref, ok := getAttr(tag.attrs, "idref")
			if !ok {
				continue
			}
			linear, ok := getAttr(tag.attrs, "linear")
			if ok && strings.EqualFold(linear, "no") {
				continue
			}
			spineIDs = append(spineIDs, idref)
		case "meta":
			name, _ := getAttr(tag.attrs, "name")
			if strings.EqualFold(name, "cover") {
				coverID, _ = getAttr(tag.attrs, "content")
			}
		case "reference":
			kind, _ := getAttr(tag.attrs, "type")
			href, _ := getAttr(tag.attrs, "href")
			if strings.EqualFold(kind, "cover") {
				coverHrefDirect = href
			}
			switch strings.ToLower(kind) {
			case "toc", "cover", "title-page", "titlepage":
				if href != "" {
					skipRefs[resolveHref(opfPath, href)] = true
				}
			}
		}
	}

	var coverItem *manifestItem
	for idx := range manifest {
		if strings.Contains(manifest[idx].properties, "cover-image") {
			coverItem = &manifest[idx]
			break
		}
	}
	if coverItem == nil && coverID != "" {
		for idx := range manifest {
			if manifest[idx].id == coverID {
				coverItem = &manifest[idx]
				break
			}
		}
	}
	if coverItem == nil {
		for idx := range manifest {
			item := &manifest[idx]
			if strings.HasPrefix(item.mediaType, "image/") &&
				(strings.EqualFold(item.id, "cover") || strings.Contains(item.href, "cover")) {
				coverItem = item
				break
			}
		}
	}
	if coverItem == nil && coverHrefDirect != "" {
		resolved := resolveHref(opfPath, coverHrefDirect)
		for idx := range manifest {
			if manifest[idx].href == resolved {
				coverItem = &manifest[idx]
				break
			}
		}
	}

	var coverBytes []byte
	coverExt := ""
	if coverItem != nil {
		if entry, ok := entries[coverItem.href]; ok {
			if data, err := readZipEntry(entry); err == nil {
				coverBytes = data
				coverExt = coverExtension(coverItem.mediaType, coverItem.href)
			}
		}
	}

	var chapters []epubChapter
	for spineIndex, idref := range spineIDs {
		href := ""
		mediaType := ""
		properties := ""
		for _, item := range manifest {
			if item.id == idref {
				href = item.href
				mediaType = item.mediaType
				properties = item.properties
				break
			}
		}
		if href == "" {
			continue
		}
		if mediaType != "" && !strings.Contains(mediaType, "html") && !strings.Contains(mediaType, "xml") {
			continue
		}
		// 目录页、封面页、扉页只用于阅读，不参与搜索。
		if skipRefs[href] || hasToken(properties, "nav") || hasToken(properties, "cover") {
			continue
		}
		entry, ok := entries[href]
		if !ok {
			continue
		}
		data, err := readZipEntry(entry)
		if err != nil {
			continue
		}
		if isNavDocument(string(data)) {
			continue
		}
		chapters = append(chapters, epubChapter{
			SpineIndex: int64(spineIndex),
			Href:       href,
			Text:       htmlToText(string(data)),
		})
	}

	return &epubParsed{
		Title:      title,
		Author:     author,
		Language:   language,
		CoverBytes: coverBytes,
		CoverExt:   coverExt,
		Chapters:   chapters,
	}, nil
}

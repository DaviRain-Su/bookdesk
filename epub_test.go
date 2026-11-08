package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeTestEpub 在临时目录里生成一本两章、带封面、带中文与英文的 EPUB。
func writeTestEpub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "测试书.epub")
	container := `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`
	opf := `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>迁移测试书</dc:title>
    <dc:creator>测试作者</dc:creator>
    <dc:language>zh-CN</dc:language>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover-img" href="cover.png" media-type="image/png" properties="cover-image"/>
  </manifest>
  <spine><itemref idref="nav"/><itemref idref="ch1"/><itemref idref="ch2"/></spine>
</package>`
	nav := `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>目录</title></head>
<body><nav epub:type="toc"><ol>
<li><a href="ch1.xhtml">第一章 迁移之后</a></li>
<li><a href="ch2.xhtml">第二章 画布</a></li>
</ol></nav></body></html>`
	ch1 := `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第一章</title></head>
<body><h1>第一章 迁移之后</h1>
<p>Go 后端接手了书架。这里有一个测试词：量子纠缠。</p>
<p>还有一句英文：The quick brown fox jumps over the lazy dog.</p></body></html>`
	ch2 := `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第二章</title></head>
<body><h1>第二章 画布</h1>
<p>白板测试词出现在这里，白板测试词出现两次。</p></body></html>`
	// 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x60, 0x00, 0x00, 0x00,
		0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	write := func(name, content string, stored bool) {
		method := zip.Deflate
		if stored {
			method = zip.Store
		}
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	write("mimetype", "application/epub+zip", true)
	write("META-INF/container.xml", container, false)
	write("OEBPS/content.opf", opf, false)
	write("OEBPS/nav.xhtml", nav, false)
	write("OEBPS/ch1.xhtml", ch1, false)
	write("OEBPS/ch2.xhtml", ch2, false)
	w, err := z.CreateHeader(&zip.FileHeader{Name: "OEBPS/cover.png", Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSampleEpub(t *testing.T) {
	parsed, err := parseEpub(writeTestEpub(t))
	if err != nil {
		t.Fatalf("parseEpub: %v", err)
	}
	if parsed.Title != "迁移测试书" {
		t.Errorf("title = %q", parsed.Title)
	}
	if parsed.Author == nil || *parsed.Author != "测试作者" {
		t.Errorf("author = %v", parsed.Author)
	}
	if len(parsed.Chapters) != 2 {
		t.Fatalf("chapters = %d", len(parsed.Chapters))
	}
	// 目录页不进索引，但后面章节的 spine 下标不能因此错位。
	if parsed.Chapters[0].SpineIndex != 1 || parsed.Chapters[1].SpineIndex != 2 {
		t.Errorf("spine indices = %d, %d", parsed.Chapters[0].SpineIndex, parsed.Chapters[1].SpineIndex)
	}
	if parsed.CoverBytes == nil || parsed.CoverExt != "png" {
		t.Errorf("cover = %d bytes, ext %q", len(parsed.CoverBytes), parsed.CoverExt)
	}
	if want := "第二章 画布\n\n白板测试词出现在这里，白板测试词出现两次。"; parsed.Chapters[1].Text != want {
		t.Errorf("chapter 2 = %q", parsed.Chapters[1].Text)
	}
}

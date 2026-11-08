package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func nowSeconds() int64 { return time.Now().Unix() }

func nowMillis() int64 { return time.Now().UnixMilli() }

// newID 生成 "book-1a2b3c4d5e6f7788" 这样的本地 id。
func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// newUUID 给 Pi 的 --session-id 用，必须是标准 UUID。
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func ensureDir(dir string) error { return os.MkdirAll(dir, 0o755) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFileLimit(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

func writeFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return os.WriteFile(path, data, 0o644)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if dir := filepath.Dir(dest); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func baseName(path string) string { return filepath.Base(path) }

func extensionLower(path string) string { return strings.ToLower(filepath.Ext(path)) }

func eqlIgnoreCase(a, b string) bool { return strings.EqualFold(a, b) }

func containsIgnoreCase(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func indexOfIgnoreCase(haystack, needle string) int {
	if needle == "" {
		return -1
	}
	return strings.Index(strings.ToLower(haystack), strings.ToLower(needle))
}

// floorCharBoundary 把字节位置退到 UTF-8 字符边界。
func floorCharBoundary(text string, pos int) int {
	if pos > len(text) {
		pos = len(text)
	}
	for pos > 0 && pos < len(text) && text[pos]&0xC0 == 0x80 {
		pos--
	}
	return pos
}

// ceilCharBoundary 把字节位置进到 UTF-8 字符边界。
func ceilCharBoundary(text string, pos int) int {
	if pos > len(text) {
		pos = len(text)
	}
	for pos < len(text) && text[pos]&0xC0 == 0x80 {
		pos++
	}
	return pos
}

// utf16Len 数前 byteLen 个字节里有多少个 UTF-16 码元。
// epub.js 的 range 用 UTF-16 偏移，高亮区间要对齐它。
func utf16Len(text string, byteLen int) int64 {
	if byteLen > len(text) {
		byteLen = len(text)
	}
	var units int64
	for i := 0; i < byteLen; {
		r, size := decodeRune(text[i:])
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		i += size
	}
	return units
}

func decodeRune(s string) (rune, int) {
	if s == "" {
		return 0xFFFD, 1
	}
	c := s[0]
	switch {
	case c < 0x80:
		return rune(c), 1
	case c&0xE0 == 0xC0 && len(s) >= 2:
		return rune(c&0x1F)<<6 | rune(s[1]&0x3F), 2
	case c&0xF0 == 0xE0 && len(s) >= 3:
		return rune(c&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	case c&0xF8 == 0xF0 && len(s) >= 4:
		return rune(c&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
	}
	return 0xFFFD, 1
}

// splitCommand 按空白把设置里的命令拆成 argv。
func splitCommand(command string) []string {
	return strings.Fields(strings.TrimSpace(command))
}

package main

import (
	"bytes"
	"html"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// indexedTextLimit caps the text indexed for one entry (section 64.12,
// "Maximum extracted text per entry: 1 MiB").
const indexedTextLimit = 1 << 20

// nativeExtractReadLimit bounds how many bytes of a file a rescan reads for
// native extraction. Larger files are catalogued but not indexed natively.
const nativeExtractReadLimit = 16 << 20

// textExtensions are indexed natively even when the stored content type is
// generic (section 64.6). Rich-text formats that are not textual (for example
// .docx or .pdf) are excluded.
var textExtensions = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".text": true,
	".csv": true, ".tsv": true, ".json": true, ".jsonl": true, ".ndjson": true,
	".xml": true, ".html": true, ".htm": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".conf": true, ".log": true,
	".css": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true,
	".go": true, ".py": true, ".rb": true, ".sh": true, ".sql": true,
}

var (
	htmlScriptStyle = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)\s*>`)
	htmlAnyTag      = regexp.MustCompile(`(?s)<[^>]*>`)
)

// nativeExtraction returns the title and the searchable body extracted from one
// entry's stored bytes (section 64.6). It is pure: it reads no files, performs
// no I/O and never mutates state, so it is unit-testable in isolation. The body
// is empty for content with no native text, such as binary files and image-only
// PDFs.
//
// The title is the entry's base name. The body is the extracted text: PDF text
// layers are extracted, HTML is indexed with markup removed, and other
// text-like formats are indexed verbatim. The body is capped at
// indexedTextLimit bytes.
func nativeExtraction(entryPath, contentType string, data []byte) (string, string) {
	title := capIndexedText(path.Base(entryPath))
	if isPDF(entryPath, contentType) {
		return title, capIndexedText(pdfText(data))
	}
	if !isTextLike(entryPath, contentType) || !utf8.Valid(data) {
		return title, ""
	}
	text := string(data)
	if isHTML(entryPath, contentType) {
		text = htmlToText(text)
	}
	return title, capIndexedText(text)
}

// nativeExtractable reports whether native extraction can yield body text for
// an entry. Other entries (photos, audio, archives) are never read for
// indexing (section 64.6).
func nativeExtractable(entryPath, contentType string) bool {
	return isPDF(entryPath, contentType) || isTextLike(entryPath, contentType)
}

func isPDF(entryPath, contentType string) bool {
	return strings.EqualFold(path.Ext(entryPath), ".pdf") ||
		strings.EqualFold(strings.TrimSpace(contentType), "application/pdf")
}

func isHTML(entryPath, contentType string) bool {
	switch strings.ToLower(path.Ext(entryPath)) {
	case ".html", ".htm":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(contentType), "text/html")
}

// isTextLike reports whether the entry should be indexed as text. A text/*
// media type qualifies, as do a small set of textual application types and
// known text extensions (section 64.6).
func isTextLike(entryPath, contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript",
		"application/x-javascript", "application/x-yaml", "application/yaml",
		"application/x-ndjson":
		return true
	}
	return textExtensions[strings.ToLower(path.Ext(entryPath))]
}

// htmlToText removes markup, script and style content, then decodes entities
// and collapses whitespace so the index sees readable text (section 64.6).
func htmlToText(source string) string {
	source = htmlScriptStyle.ReplaceAllString(source, " ")
	source = htmlAnyTag.ReplaceAllString(source, " ")
	source = html.UnescapeString(source)
	return strings.Join(strings.Fields(source), " ")
}

// pdfText extracts the text layer from a PDF. A missing text layer, an
// encrypted or malformed document, or a parser panic yields no text and never
// fails the surrounding write or rescan (section 64.6: OCR is out of scope).
func pdfText(data []byte) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(plain, int64(indexedTextLimit)+1)); err != nil {
		return ""
	}
	if !utf8.Valid(buf.Bytes()) {
		return ""
	}
	return buf.String()
}

// capIndexedText truncates text to the per-entry index cap without splitting a
// UTF-8 encoding (section 64.12).
func capIndexedText(s string) string {
	if len(s) <= indexedTextLimit {
		return s
	}
	s = s[:indexedTextLimit]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeExtractionPerType(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		contentType string
		data        []byte
		wantTitle   string
		wantBody    string // when non-empty, the body must equal this exactly
		wantEmpty   bool   // when set, the body must be empty
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "markdown", path: "/docs/note.md", contentType: "text/markdown",
			data: []byte("# Hello\n\nworld"), wantTitle: "note.md",
			wantContain: []string{"Hello", "world"},
		},
		{
			name: "plain text declared", path: "/docs/readme.txt", contentType: "text/plain",
			data: []byte("plain text body"), wantTitle: "readme.txt", wantBody: "plain text body",
		},
		{
			name: "csv by extension", path: "/data/rows.csv", contentType: "application/octet-stream",
			data: []byte("a,b\n1,2"), wantTitle: "rows.csv", wantBody: "a,b\n1,2",
		},
		{
			name: "json by media type", path: "/data/blob", contentType: "application/json",
			data: []byte(`{"k":"v"}`), wantTitle: "blob", wantBody: `{"k":"v"}`,
		},
		{
			name: "html strips markup", path: "/page.html", contentType: "text/html",
			data:        []byte(`<html><head><title>T</title><style>body{color:red}</style></head><body><h1>Hello</h1><p>Bold <b>world</b></p><script>alert(1)</script></body></html>`),
			wantTitle:   "page.html",
			wantContain: []string{"Hello", "Bold world"},
			wantAbsent:  []string{"<", "color:red", "alert"},
		},
		{
			name: "binary is not indexed", path: "/bin/blob.bin", contentType: "application/octet-stream",
			data: []byte{0x00, 0x01, 0x02, 0xff}, wantTitle: "blob.bin", wantEmpty: true,
		},
		{
			name: "invalid utf8 text is not indexed", path: "/docs/bad.txt", contentType: "text/plain",
			data: []byte{0xff, 0xfe, 'a'}, wantTitle: "bad.txt", wantEmpty: true,
		},
		{
			name: "pdf text layer", path: "/bills/2026-03.pdf", contentType: "application/pdf",
			data: tinyPDF("Hello Clio"), wantTitle: "2026-03.pdf",
			wantContain: []string{"Hello Clio"},
		},
		{
			name: "pdf without text layer", path: "/bills/scan.pdf", contentType: "application/pdf",
			data: tinyPDF(""), wantTitle: "scan.pdf", wantEmpty: true,
		},
		{
			name: "malformed pdf yields no text", path: "/bills/broken.pdf", contentType: "application/pdf",
			data: []byte("%PDF-1.4 not really a pdf"), wantTitle: "broken.pdf", wantEmpty: true,
		},
		{
			name: "pdf detected by extension", path: "/scan.pdf", contentType: "application/octet-stream",
			data: tinyPDF("Invoice 42"), wantTitle: "scan.pdf", wantContain: []string{"Invoice 42"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, body := nativeExtraction(tc.path, tc.contentType, tc.data)
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
			if tc.wantBody != "" && body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			if tc.wantEmpty && strings.TrimSpace(body) != "" {
				t.Errorf("body = %q, want empty", body)
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(body, want) {
					t.Errorf("body %q does not contain %q", body, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(body, absent) {
					t.Errorf("body %q contains %q", body, absent)
				}
			}
		})
	}
}

func TestNativeExtractionCapsIndexedText(t *testing.T) {
	data := []byte(strings.Repeat("a", indexedTextLimit+4096))
	_, body := nativeExtraction("/docs/big.txt", "text/plain", data)
	if len(body) != indexedTextLimit {
		t.Fatalf("capped body length = %d, want %d", len(body), indexedTextLimit)
	}

	// A multi-byte rune straddling the cap must not leave a partial encoding.
	multi := []byte(strings.Repeat("€", indexedTextLimit))
	_, body = nativeExtraction("/docs/big.txt", "text/plain", multi)
	if len(body) > indexedTextLimit {
		t.Fatalf("capped body length = %d, exceeds %d", len(body), indexedTextLimit)
	}
	if !utf8.ValidString(body) {
		t.Fatal("capped body is not valid UTF-8")
	}
	if len(body) != (indexedTextLimit/3)*3 {
		t.Fatalf("capped body length = %d, want %d", len(body), (indexedTextLimit/3)*3)
	}
}

// tinyPDF builds a minimal single-page PDF with a text layer so extraction can
// be exercised without an external fixture.
func tinyPDF(text string) []byte {
	var b bytes.Buffer
	offsets := make([]int, 6)
	stream := "BT /F1 24 Tf 100 700 Td (" + strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text) + ") Tj ET"
	objects := []struct {
		number int
		body   string
	}{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"},
		{4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)},
		{5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
	}
	b.WriteString("%PDF-1.4\n")
	for _, object := range objects {
		offsets[object.number] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", object.number, object.body)
	}
	xref := b.Len()
	b.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for number := 1; number <= 5; number++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[number])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

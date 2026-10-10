package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfiguredUploadLimit(t *testing.T) {
	a := newTestApp(t)
	a.maxUpload = 32
	if w := testRequest(t, a, http.MethodPut, filesURL("default", "/ok.bin"), strings.Repeat("x", 32), "application/octet-stream"); w.Code != http.StatusCreated {
		t.Fatalf("at-limit PUT status = %d: %s", w.Code, w.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/big.bin"), strings.Repeat("x", 33), "application/octet-stream"), http.StatusRequestEntityTooLarge)

	a.webdavEnabled = true
	chunked := httptest.NewRequest(http.MethodPut, "/api/v1/default/files/dav/big.bin", io.MultiReader(strings.NewReader(strings.Repeat("x", 33))))
	chunked.ContentLength = -1
	w := httptest.NewRecorder()
	a.ServeHTTP(w, chunked)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("WebDAV chunked over-limit status = %d, want 413", w.Code)
	}
}

func TestUploadAboveLegacyCapWhenConfigured(t *testing.T) {
	a := newTestApp(t)
	a.maxUpload = 64 << 20
	size := 17 << 20 // 17 MiB: above the old 16 MiB fixed cap, below the configured limit
	entry := putFile(t, a, "default", "/photos/big.jpg", string(bytes.Repeat([]byte{7}, size)), "image/jpeg")
	if entry["size"] != float64(size) {
		t.Fatalf("size = %v, want %d", entry["size"], size)
	}
	copied := testRequest(t, a, http.MethodPost, "/api/v1/default/files/copy", map[string]any{"from": "/photos/big.jpg", "to": "/photos/copy.jpg"}, "application/json")
	if copied.Code != http.StatusCreated && copied.Code != http.StatusOK {
		t.Fatalf("copy status = %d: %s", copied.Code, copied.Body.String())
	}
	original, _ := contentEntryForPath(t, a, "default", "/photos/big.jpg")
	duplicate, _ := contentEntryForPath(t, a, "default", "/photos/copy.jpg")
	if duplicate.SHA256 != original.SHA256 || duplicate.ID == original.ID {
		t.Fatalf("copy = %+v, original = %+v", duplicate, original)
	}
}

func TestParseUploadLimit(t *testing.T) {
	if got, err := parseUploadLimit(""); err != nil || got != defaultUploadLimit {
		t.Errorf("empty = %d, %v", got, err)
	}
	if got, err := parseUploadLimit("2147483648"); err != nil || got != 2<<30 {
		t.Errorf("2 GiB = %d, %v", got, err)
	}
	for _, bad := range []string{"0", "-1", "lots", "1.5"} {
		if _, err := parseUploadLimit(bad); err == nil {
			t.Errorf("parseUploadLimit(%q) accepted", bad)
		}
	}
}

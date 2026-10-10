package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIgnoredContentName(t *testing.T) {
	for _, name := range []string{".DS_Store", "._photo.jpg", "Thumbs.db", "desktop.ini", ".clio-123.tmp", ".clio-upload-9.tmp", "__MACOSX"} {
		if !ignoredContentName(name) {
			t.Errorf("ignoredContentName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"photo.jpg", ".hidden", "notes.md", "_draft.md", "clio-notes.txt"} {
		if ignoredContentName(name) {
			t.Errorf("ignoredContentName(%q) = true, want false", name)
		}
	}
	if !ignoredContentPath("/trip/__MACOSX/a.jpg") || ignoredContentPath("/trip/a.jpg") {
		t.Error("ignoredContentPath does not check every segment")
	}
}

func TestRescanAndListingSkipIgnoredNames(t *testing.T) {
	a := newTestApp(t)
	root := filepath.Join(a.content, "default", "trip")
	if err := os.MkdirAll(filepath.Join(root, "__MACOSX"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", ".DS_Store", "._a.jpg", ".clio-42.tmp", "__MACOSX/._a.jpg"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.withProject("default").rescanContent(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if _, found := contentEntryForPath(t, a, "default", "/trip/a.jpg"); !found {
		t.Fatal("real file was not cataloged")
	}
	for _, p := range []string{"/trip/.DS_Store", "/trip/._a.jpg", "/trip/.clio-42.tmp", "/trip/__MACOSX/._a.jpg"} {
		if _, found := contentEntryForPath(t, a, "default", p); found {
			t.Errorf("%s was cataloged", p)
		}
	}
	listing := testRequest(t, a, http.MethodGet, filesURL("default", "/trip"), nil, "")
	if listing.Code != http.StatusOK {
		t.Fatalf("list status = %d", listing.Code)
	}
	for _, junk := range []string{".DS_Store", "._a.jpg", ".clio-42", "__MACOSX"} {
		if strings.Contains(listing.Body.String(), junk) {
			t.Errorf("API listing shows %s: %s", junk, listing.Body.String())
		}
	}
	human := testRequest(t, a, http.MethodGet, "/default/files/trip", nil, "")
	if strings.Contains(human.Body.String(), ".DS_Store") {
		t.Error("human listing shows .DS_Store")
	}
	// Listing must not adopt an ignored file into the catalog either.
	if _, found := contentEntryForPath(t, a, "default", "/trip/.DS_Store"); found {
		t.Error("listing adopted .DS_Store")
	}
}

func TestWritesRefuseIgnoredNames(t *testing.T) {
	a := newTestApp(t)
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/a/.DS_Store"), "x", "application/octet-stream"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/a/._x.jpg"), "x", "image/jpeg"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/__MACOSX"}, "application/json"), http.StatusUnprocessableEntity)
	putFile(t, a, "default", "/a/real.txt", "x", "text/plain")
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/a/real.txt", "to": "/a/.clio-x.tmp"}, "application/json"), http.StatusUnprocessableEntity)
}

func TestZipUploadSkipsMacOSMetadata(t *testing.T) {
	a := newTestApp(t)
	archive := buildZip(t, []zipEntry{
		{name: "photos/a.txt", body: "real"},
		{name: "photos/._a.txt", body: "junk"},
		{name: "__MACOSX/photos/._a.txt", body: "junk"},
		{name: "photos/.DS_Store", body: "junk"},
	})
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=/up", archive, "application/zip")
	if w.Code != http.StatusCreated {
		t.Fatalf("zip upload status = %d: %s", w.Code, w.Body.String())
	}
	if _, found := contentEntryForPath(t, a, "default", "/up/photos/a.txt"); !found {
		t.Fatal("real ZIP entry missing")
	}
	for _, p := range []string{"/up/photos/._a.txt", "/up/__MACOSX/photos/._a.txt", "/up/photos/.DS_Store"} {
		if _, err := os.Stat(filepath.Join(a.content, "default", filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Errorf("%s was written to disk", p)
		}
	}
}

func TestWebDAVRejectsIgnoredNames(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"
	if w := davRequest(t, a, http.MethodPut, mount+"/trip/._a.jpg", "junk", nil); w.Code != http.StatusForbidden {
		t.Errorf("PUT ._a.jpg status = %d, want 403", w.Code)
	}
	if w := davRequest(t, a, "MKCOL", mount+"/__MACOSX", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("MKCOL __MACOSX status = %d, want 403", w.Code)
	}
	if w := davRequest(t, a, "PROPFIND", mount+"/trip/.DS_Store", "", map[string]string{"Depth": "0"}); w.Code != http.StatusNotFound {
		t.Errorf("PROPFIND .DS_Store status = %d, want 404", w.Code)
	}
	if w := davRequest(t, a, http.MethodPut, mount+"/trip/a.jpg", "real", nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT a.jpg status = %d: %s", w.Code, w.Body.String())
	}
	// A PROPFIND listing omits junk that exists on disk.
	if err := os.WriteFile(filepath.Join(a.content, "default", "trip", ".DS_Store"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	listing := davRequest(t, a, "PROPFIND", mount+"/trip/", "", map[string]string{"Depth": "1"})
	if strings.Contains(listing.Body.String(), ".DS_Store") {
		t.Error("PROPFIND listing shows .DS_Store")
	}
}

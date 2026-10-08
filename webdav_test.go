package main

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// davRequest sends a raw WebDAV request with optional headers and body. The app
// is exercised through ServeHTTP like every other test (auth included).
func davRequest(t *testing.T, a *app, method, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	r := httptest.NewRequest(method, path, reader)
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func davEntryID(t *testing.T, a *app, path string) string {
	t.Helper()
	entry, found, err := a.withProject("default").contentEntryByPath(path)
	if err != nil {
		t.Fatalf("look up entry %s: %v", path, err)
	}
	if !found {
		t.Fatalf("no content entry at %s", path)
	}
	return entry.ID
}

const davLockBody = `<?xml version="1.0" encoding="utf-8"?>
<D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype><D:owner><D:href>test</D:href></D:owner></D:lockinfo>`

const davPropfindBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:"><D:allprop/></D:propfind>`

const davProppatchBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propertyupdate xmlns:D="DAV:" xmlns:Z="urn:clio:test"><D:set><D:prop><Z:author>clio</Z:author></D:prop></D:set></D:propertyupdate>`

// endlessReader yields an unbounded stream so a request can present an
// unknown-length (chunked-style) body to the WebDAV mount.
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestWebDAVPutUploadLimit verifies that a PUT body is bounded before the
// handler buffers it: a declared over-limit Content-Length and an
// unknown-length body that exceeds the limit both return 413, and a valid PUT
// still works (section 64.12).
func TestWebDAVPutUploadLimit(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"

	declared := httptest.NewRequest(http.MethodPut, mount+"/big.bin", strings.NewReader("tiny"))
	declared.ContentLength = fileUploadLimit + 1
	declaredRecorder := httptest.NewRecorder()
	a.ServeHTTP(declaredRecorder, declared)
	if declaredRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("declared over-limit PUT status = %d, want 413: %s", declaredRecorder.Code, declaredRecorder.Body.String())
	}

	chunked := httptest.NewRequest(http.MethodPut, mount+"/big.bin", endlessReader{})
	if chunked.ContentLength != -1 {
		t.Fatalf("test reader ContentLength = %d, want -1", chunked.ContentLength)
	}
	chunkedRecorder := httptest.NewRecorder()
	a.ServeHTTP(chunkedRecorder, chunked)
	if chunkedRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("unknown-length over-limit PUT status = %d, want 413: %s", chunkedRecorder.Code, chunkedRecorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "big.bin")); !os.IsNotExist(err) {
		t.Errorf("over-limit PUT wrote a file: %v", err)
	}

	if w := davRequest(t, a, http.MethodPut, mount+"/small.txt", "hello", nil); w.Code != http.StatusCreated {
		t.Fatalf("valid PUT status = %d: %s", w.Code, w.Body.String())
	}
}

// TestWebDAVGetAppliesDownloadProtections verifies that a WebDAV GET carries
// nosniff for every entry and an attachment disposition for non-page content
// (section 64.5).
func TestWebDAVGetAppliesDownloadProtections(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"
	if w := davRequest(t, a, http.MethodPut, mount+"/docs/a.html", "<b>x</b>", nil); w.Code != http.StatusCreated {
		t.Fatalf("put page status = %d: %s", w.Code, w.Body.String())
	}
	if w := davRequest(t, a, http.MethodPut, mount+"/docs/a.bin", "\x00\x01", nil); w.Code != http.StatusCreated {
		t.Fatalf("put file status = %d: %s", w.Code, w.Body.String())
	}

	file := davRequest(t, a, http.MethodGet, mount+"/docs/a.bin", "", nil)
	if file.Code != http.StatusOK {
		t.Fatalf("GET file status = %d", file.Code)
	}
	if got := file.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("file nosniff = %q, want nosniff", got)
	}
	if got := file.Header().Get("Content-Disposition"); got != "attachment" {
		t.Errorf("file disposition = %q, want attachment", got)
	}

	page := davRequest(t, a, http.MethodGet, mount+"/docs/a.html", "", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("GET page status = %d", page.Code)
	}
	if got := page.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("page nosniff = %q, want nosniff", got)
	}
	if got := page.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("page disposition = %q, want none", got)
	}
}

// TestWebDAVDisabledByDefault verifies the opt-in gate (section 64.10):
// without CLIO_WEBDAV_ENABLED the mount does not exist at either URL.
func TestWebDAVDisabledByDefault(t *testing.T) {
	a := newTestApp(t)
	for _, path := range []string{
		"/api/v1/default/files/dav/docs/a.txt",
		"/default/files/dav/docs/a.txt",
	} {
		w := davRequest(t, a, http.MethodPut, path, "hello", map[string]string{"Content-Type": "text/plain"})
		if w.Code != http.StatusNotFound {
			t.Errorf("PUT %s with WebDAV disabled status = %d, want 404", path, w.Code)
		}
	}
	if w := davRequest(t, a, "PROPFIND", "/api/v1/default/files/dav/", "", map[string]string{"Depth": "1"}); w.Code != http.StatusNotFound {
		t.Errorf("PROPFIND with WebDAV disabled status = %d, want 404", w.Code)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "docs", "a.txt")); !os.IsNotExist(err) {
		t.Errorf("disabled WebDAV wrote a file: %v", err)
	}

	// Enabling it makes the same mount respond.
	a.webdavEnabled = true
	if w := davRequest(t, a, http.MethodPut, "/api/v1/default/files/dav/docs/a.txt", "hello", nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT with WebDAV enabled status = %d: %s", w.Code, w.Body.String())
	}
}

// TestWebDAVMethodSet exercises the method set at both mount URLs (section
// 64.10): PUT, GET, HEAD, PROPFIND, MKCOL, COPY, MOVE, DELETE, LOCK, UNLOCK.
func TestWebDAVMethodSet(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"

	if w := davRequest(t, a, http.MethodOptions, mount+"/", "", nil); w.Code != http.StatusOK {
		t.Fatalf("OPTIONS status = %d, want 200", w.Code)
	} else if dav := w.Header().Get("DAV"); !strings.Contains(dav, "1") {
		t.Errorf("OPTIONS DAV header = %q", dav)
	}

	// PUT creates, then GET/HEAD read the bytes back.
	if w := davRequest(t, a, http.MethodPut, mount+"/docs/a.txt", "hello world", nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}
	if w := davRequest(t, a, http.MethodGet, mount+"/docs/a.txt", "", nil); w.Code != http.StatusOK || w.Body.String() != "hello world" {
		t.Fatalf("GET status = %d body = %q", w.Code, w.Body.String())
	}
	if w := davRequest(t, a, http.MethodHead, mount+"/docs/a.txt", "", nil); w.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d", w.Code)
	}
	if w := davRequest(t, a, "PROPFIND", mount+"/", "", map[string]string{"Depth": "1", "Content-Type": "application/xml"}); w.Code != 207 {
		t.Fatalf("PROPFIND status = %d: %s", w.Code, w.Body.String())
	}

	// MKCOL, COPY and MOVE.
	if w := davRequest(t, a, "MKCOL", mount+"/archive", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("MKCOL status = %d: %s", w.Code, w.Body.String())
	}
	copyHeaders := map[string]string{"Destination": mount + "/archive/a-copy.txt", "Overwrite": "T"}
	if w := davRequest(t, a, "COPY", mount+"/docs/a.txt", "", copyHeaders); w.Code != http.StatusCreated {
		t.Fatalf("COPY status = %d: %s", w.Code, w.Body.String())
	}
	moveHeaders := map[string]string{"Destination": mount + "/archive/a-moved.txt", "Overwrite": "T"}
	if w := davRequest(t, a, "MOVE", mount+"/archive/a-copy.txt", "", moveHeaders); w.Code != http.StatusCreated {
		t.Fatalf("MOVE status = %d: %s", w.Code, w.Body.String())
	}
	if w := davRequest(t, a, http.MethodGet, mount+"/archive/a-moved.txt", "", nil); w.Code != http.StatusOK || w.Body.String() != "hello world" {
		t.Fatalf("GET after move status = %d body = %q", w.Code, w.Body.String())
	}

	// DELETE, then LOCK/UNLOCK on a remaining resource.
	if w := davRequest(t, a, http.MethodDelete, mount+"/archive/a-moved.txt", "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d: %s", w.Code, w.Body.String())
	}
	lock := davRequest(t, a, "LOCK", mount+"/docs/a.txt", davLockBody, map[string]string{"Content-Type": "application/xml"})
	if lock.Code != http.StatusOK {
		t.Fatalf("LOCK status = %d: %s", lock.Code, lock.Body.String())
	}
	token := lock.Header().Get("Lock-Token")
	if token == "" {
		t.Fatal("LOCK response has no Lock-Token")
	}
	if w := davRequest(t, a, "UNLOCK", mount+"/docs/a.txt", "", map[string]string{"Lock-Token": token}); w.Code != http.StatusNoContent {
		t.Fatalf("UNLOCK status = %d: %s", w.Code, w.Body.String())
	}

	// PROPPATCH is accepted on a file and on a collection; with no dead-property
	// support the change is reported per-property inside a 207 multistatus.
	for _, path := range []string{mount + "/docs/a.txt", mount + "/archive/"} {
		w := davRequest(t, a, "PROPPATCH", path, davProppatchBody, map[string]string{"Content-Type": "application/xml"})
		if w.Code != 207 {
			t.Errorf("PROPPATCH %s status = %d, want 207: %s", path, w.Code, w.Body.String())
		}
	}

	// The human mount serves the same tree.
	if w := davRequest(t, a, http.MethodPut, "/default/files/dav/human.txt", "human", nil); w.Code != http.StatusCreated {
		t.Fatalf("human-mount PUT status = %d: %s", w.Code, w.Body.String())
	}
	if got := diskFile(t, a, "default", "/human.txt"); got != "human" {
		t.Errorf("human-mount stored bytes = %q", got)
	}
}

// TestWebDAVMovePreservesIDAndCopyAssignsNewID checks the identity rules
// through WebDAV (section 64.10): MOVE keeps the entry ID, COPY gets a new one.
func TestWebDAVMovePreservesIDAndCopyAssignsNewID(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"

	if w := davRequest(t, a, http.MethodPut, mount+"/docs/a.txt", "hello", nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}
	original := davEntryID(t, a, "/docs/a.txt")

	headers := map[string]string{"Destination": mount + "/docs/renamed.txt", "Overwrite": "T"}
	if w := davRequest(t, a, "MOVE", mount+"/docs/a.txt", "", headers); w.Code != http.StatusCreated {
		t.Fatalf("MOVE status = %d: %s", w.Code, w.Body.String())
	}
	if moved := davEntryID(t, a, "/docs/renamed.txt"); moved != original {
		t.Errorf("MOVE changed the entry ID: %s -> %s", original, moved)
	}

	copyHeaders := map[string]string{"Destination": mount + "/docs/copied.txt", "Overwrite": "T"}
	if w := davRequest(t, a, "COPY", mount+"/docs/renamed.txt", "", copyHeaders); w.Code != http.StatusCreated {
		t.Fatalf("COPY status = %d: %s", w.Code, w.Body.String())
	}
	copied := davEntryID(t, a, "/docs/copied.txt")
	if copied == original {
		t.Errorf("COPY reused the source entry ID %s", original)
	}
}

// TestWebDAVDirectoryMovePreservesDescendantIDs checks that moving a directory
// keeps every descendant entry ID (section 64.10).
func TestWebDAVDirectoryMovePreservesDescendantIDs(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"

	for path, body := range map[string]string{
		"/src/a.txt":     "a",
		"/src/sub/b.txt": "b",
	} {
		if w := davRequest(t, a, http.MethodPut, mount+path, body, nil); w.Code != http.StatusCreated {
			t.Fatalf("PUT %s status = %d: %s", path, w.Code, w.Body.String())
		}
	}
	first, second := davEntryID(t, a, "/src/a.txt"), davEntryID(t, a, "/src/sub/b.txt")

	headers := map[string]string{"Destination": mount + "/dst", "Overwrite": "T"}
	if w := davRequest(t, a, "MOVE", mount+"/src", "", headers); w.Code != http.StatusCreated {
		t.Fatalf("directory MOVE status = %d: %s", w.Code, w.Body.String())
	}
	if got := davEntryID(t, a, "/dst/a.txt"); got != first {
		t.Errorf("descendant ID changed: %s -> %s", first, got)
	}
	if got := davEntryID(t, a, "/dst/sub/b.txt"); got != second {
		t.Errorf("nested descendant ID changed: %s -> %s", second, got)
	}
}

// TestWebDAVDeleteReferencedReturnsConflict checks that WebDAV DELETE enforces
// the attachment-reference 409 for a file and for a directory subtree
// (sections 64.9 and 64.10).
func TestWebDAVDeleteReferencedReturnsConflict(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name":   "invoices",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment"}},
	})
	file := putFile(t, a, "default", "/docs/invoice.pdf", "PDF", "application/pdf")
	id, _ := file["id"].(string)
	createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": id})

	const mount = "/api/v1/default/files/dav"
	// The direct file delete is refused.
	if w := davRequest(t, a, http.MethodDelete, mount+"/docs/invoice.pdf", "", nil); w.Code != http.StatusConflict {
		t.Fatalf("referenced file DELETE status = %d, want 409: %s", w.Code, w.Body.String())
	}
	// A directory containing a referenced entry is refused too.
	if w := davRequest(t, a, http.MethodDelete, mount+"/docs", "", nil); w.Code != http.StatusConflict {
		t.Fatalf("referenced directory DELETE status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if got := diskFile(t, a, "default", "/docs/invoice.pdf"); got != "PDF" {
		t.Errorf("referenced file was deleted: %q", got)
	}
}

// TestWebDAVRequiresAuthentication verifies that the mount is protected by the
// existing auth policy (section 54).
func TestWebDAVRequiresAuthentication(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	a.auth = authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t), requireHTTPS: true}

	request := func(credentials bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/default/files/dav/private.txt", strings.NewReader("secret"))
		r.RemoteAddr = "127.0.0.1:1234"
		if credentials {
			r.SetBasicAuth("admin", "secret")
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	if w := request(false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated WebDAV PUT status = %d, want 401", w.Code)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "private.txt")); !os.IsNotExist(err) {
		t.Errorf("unauthenticated WebDAV wrote a file: %v", err)
	}
	if w := request(true); w.Code != http.StatusCreated {
		t.Fatalf("authenticated WebDAV PUT status = %d: %s", w.Code, w.Body.String())
	}
}

// TestWebDAVPropfindReturnsEntryMetadata is a light DAV sanity check that the
// multistatus lists a child with a proper href.
func TestWebDAVPropfindReturnsEntryMetadata(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	const mount = "/api/v1/default/files/dav"
	if w := davRequest(t, a, http.MethodPut, mount+"/docs/a.txt", "hi", nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}
	w := davRequest(t, a, "PROPFIND", mount+"/docs/", davPropfindBody, map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if w.Code != 207 {
		t.Fatalf("PROPFIND status = %d: %s", w.Code, w.Body.String())
	}
	var ms struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Fatalf("decode multistatus: %v; body=%s", err, w.Body.String())
	}
	found := false
	for _, response := range ms.Responses {
		if strings.HasSuffix(response.Href, "/docs/a.txt") {
			found = true
		}
	}
	if !found {
		t.Errorf("PROPFIND did not list the child: %s", w.Body.String())
	}
}

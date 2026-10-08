package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newBackupApp builds an app on a known directory pair so tests can pass the
// database and content paths to the backup and restore functions.
func newBackupApp(t *testing.T) (*app, cliPaths) {
	t.Helper()
	root := t.TempDir()
	paths := cliPaths{
		DataDir:    root,
		DBPath:     filepath.Join(root, "clio.db"),
		ContentDir: filepath.Join(root, "content"),
	}
	db, err := openDatabase(paths.DBPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := os.MkdirAll(paths.ContentDir, 0755); err != nil {
		t.Fatalf("create test content directory: %v", err)
	}
	return &app{db: db, content: paths.ContentDir, baseURL: "http://clio.test", contentMu: &sync.Mutex{}}, paths
}

// openAppAt opens the restored database the way the server does at startup and
// returns an app over it. It deliberately does not reconcile content, so the
// assertions see exactly what restore produced.
func openAppAt(t *testing.T, dbPath, contentDir string) *app {
	t.Helper()
	db, err := openDatabase(dbPath)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &app{db: db, content: contentDir, baseURL: "http://clio.test", contentMu: &sync.Mutex{}}
}

// entryRepresentation reads one content entry by path.
func entryRepresentation(t *testing.T, a *app, project, path string) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodGet, filesURL(project, path), nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d: %s", path, w.Code, w.Body.String())
	}
	var entry map[string]any
	testJSON(t, w, &entry)
	return entry
}

func readManifestFile(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, backupManifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func TestBackupRestoreReproducesIdentityTimestampsAndEnrichment(t *testing.T) {
	a, src := newBackupApp(t)
	createTestGroup(t, a, "bills")
	createTestTable(t, a, "bills", map[string]any{"name": "invoices", "fields": []any{
		map[string]any{"name": "vendor", "type": "string", "required": true},
	}})
	record := createTestRecord(t, a, "bills", "invoices", map[string]any{"vendor": "Acme"})

	entry := putFile(t, a, "default", "/notes/readme.md", "# Notes\n\nbackup world", "text/markdown")
	id := entry["id"].(string)

	// Agent-supplied text is durable state the filesystem cannot reproduce
	// (section 64.11): enrich the entry, then prove it survives a restore.
	got := testRequest(t, a, http.MethodGet, extractionURL("default", "/notes/readme.md"), nil, "")
	var current map[string]any
	testJSON(t, got, &current)
	fingerprint, _ := current["fingerprint"].(string)
	if fingerprint == "" {
		t.Fatal("entry has no fingerprint")
	}
	if w := putEnrichment(t, a, extractionURL("default", "/notes/readme.md"), map[string]any{
		"fingerprint": fingerprint,
		"text":        "zebra quokka agent extraction",
		"provider":    "ocr:tesseract",
	}); w.Code != http.StatusOK {
		t.Fatalf("enrichment PUT status = %d: %s", w.Code, w.Body.String())
	}

	beforeEntry := entryRepresentation(t, a, "default", "/notes/readme.md")

	dest := filepath.Join(t.TempDir(), "backup")
	if err := createBackup(dest, src.DBPath, src.ContentDir, "9.9.9"); err != nil {
		t.Fatalf("createBackup: %v", err)
	}
	manifest := readManifestFile(t, dest)
	if manifest["format"] != backupFormat || manifest["format_version"] != float64(backupFormatVersion) || manifest["product_version"] != "9.9.9" || manifest["created_at"] == "" {
		t.Errorf("manifest = %#v", manifest)
	}

	dstRoot := t.TempDir()
	dst := cliPaths{DataDir: dstRoot, DBPath: filepath.Join(dstRoot, "clio.db"), ContentDir: filepath.Join(dstRoot, "content")}
	report, err := restoreBackup(dest, dst.DataDir, dst.DBPath, dst.ContentDir, false)
	if err != nil {
		t.Fatalf("restoreBackup: %v", err)
	}
	if report.Projects != 1 || report.Entries == 0 {
		t.Errorf("restore report = %#v", report)
	}

	restored := openAppAt(t, dst.DBPath, dst.ContentDir)
	afterEntry := entryRepresentation(t, restored, "default", "/notes/readme.md")
	for _, key := range []string{"id", "created_at", "updated_at", "sha256", "indexed"} {
		if afterEntry[key] != beforeEntry[key] {
			t.Errorf("restored entry %s = %v, want %v", key, afterEntry[key], beforeEntry[key])
		}
	}

	// Record identifiers and timestamps come from the database too.
	gotRecord := testRequest(t, restored, http.MethodGet, "/api/v1/default/data/groups/bills/tables/invoices/records/"+record["id"].(string), nil, "")
	if gotRecord.Code != http.StatusOK {
		t.Fatalf("get restored record status = %d: %s", gotRecord.Code, gotRecord.Body.String())
	}
	var afterRecord map[string]any
	testJSON(t, gotRecord, &afterRecord)
	for _, key := range []string{"id", "created_at", "updated_at", "vendor"} {
		if afterRecord[key] != record[key] {
			t.Errorf("restored record %s = %v, want %v", key, afterRecord[key], record[key])
		}
	}

	// The agent text is searchable after the restore.
	res := search(t, restored, "default", "quokka")
	if len(res.Data) != 1 || res.Data[0]["id"] != id || res.Data[0]["source"] != "agent:ocr:tesseract" {
		t.Fatalf("agent search after restore = %#v", res.Data)
	}
	extraction := testRequest(t, restored, http.MethodGet, extractionByIDURL("default", id), nil, "")
	var ext map[string]any
	testJSON(t, extraction, &ext)
	if ext["source"] != "agent:ocr:tesseract" || ext["text"] != "zebra quokka agent extraction" {
		t.Errorf("restored extraction = %#v", ext)
	}
}

func TestRestoreReconcilesContentTree(t *testing.T) {
	a, src := newBackupApp(t)
	kept := putFile(t, a, "default", "/kept.md", "# kept body", "text/markdown")
	putFile(t, a, "default", "/gone.md", "# gone body", "text/markdown")

	// A raw file written behind the API is not yet cataloged; a deleted page
	// still has a database row. Backup copies the disk tree as it is.
	root := filepath.Join(src.ContentDir, "default")
	if err := os.WriteFile(filepath.Join(root, "raw.md"), []byte("# raw loose body"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "backup")
	if err := createBackup(dest, src.DBPath, src.ContentDir, "1.0.0"); err != nil {
		t.Fatalf("createBackup: %v", err)
	}
	dstRoot := t.TempDir()
	dst := cliPaths{DataDir: dstRoot, DBPath: filepath.Join(dstRoot, "clio.db"), ContentDir: filepath.Join(dstRoot, "content")}
	report, err := restoreBackup(dest, dst.DataDir, dst.DBPath, dst.ContentDir, false)
	if err != nil {
		t.Fatalf("restoreBackup: %v", err)
	}
	if report.Added != 1 || report.Removed != 1 || report.Refreshed != 0 {
		t.Errorf("reconcile summary = %#v, want added 1 removed 1 refreshed 0", report)
	}

	restored := openAppAt(t, dst.DBPath, dst.ContentDir)
	if e := entryRepresentation(t, restored, "default", "/kept.md"); e["id"] != kept["id"] {
		t.Errorf("kept entry id = %v, want %v", e["id"], kept["id"])
	}
	if e := entryRepresentation(t, restored, "default", "/raw.md"); e["kind"] != "page" || e["content_type"] != "text/markdown" {
		t.Errorf("adopted raw entry = %#v", e)
	}
	if res := search(t, restored, "default", "loose"); len(res.Data) != 1 || res.Data[0]["path"] != "/raw.md" {
		t.Errorf("adopted raw entry is not searchable: %#v", res.Data)
	}
	assertAPIError(t, testRequest(t, restored, http.MethodGet, filesURL("default", "/gone.md"), nil, ""), http.StatusNotFound)
}

func TestBackupRefusesNonEmptyDestination(t *testing.T) {
	a, src := newBackupApp(t)
	putFile(t, a, "default", "/x.md", "# x", "text/markdown")
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "unrelated.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := createBackup(dest, src.DBPath, src.ContentDir, "1.0.0"); err == nil {
		t.Fatal("createBackup over a non-empty destination should fail")
	}
}

func TestBackupRequiresExistingDatabase(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(t.TempDir(), "backup")
	if err := createBackup(dest, filepath.Join(root, "missing.db"), filepath.Join(root, "content"), "1.0.0"); err == nil {
		t.Fatal("createBackup without a database should fail")
	}
}

func TestRestoreValidatesManifest(t *testing.T) {
	target := func() (string, string, string) {
		root := t.TempDir()
		return t.TempDir(), filepath.Join(root, "clio.db"), filepath.Join(root, "content")
	}

	// No manifest at all.
	dataDir, dbPath, contentDir := target()
	if _, err := restoreBackup(t.TempDir(), dataDir, dbPath, contentDir, false); !errors.Is(err, errBackupInvalid) {
		t.Errorf("missing manifest error = %v, want errBackupInvalid", err)
	}

	cases := []struct {
		name     string
		manifest string
	}{
		{"wrong format", `{"format":"other","format_version":1,"database":"clio.db","content":"content"}`},
		{"wrong version", `{"format":"clio-backup","format_version":99,"database":"clio.db","content":"content"}`},
		{"unsafe database name", `{"format":"clio-backup","format_version":1,"database":"../clio.db","content":"content"}`},
		{"unsafe content name", `{"format":"clio-backup","format_version":1,"database":"clio.db","content":"/etc"}`},
		{"not json", `{`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, backupManifestName), []byte(test.manifest), 0644); err != nil {
				t.Fatal(err)
			}
			dataDir, dbPath, contentDir := target()
			if _, err := restoreBackup(src, dataDir, dbPath, contentDir, false); !errors.Is(err, errBackupInvalid) {
				t.Errorf("restore error = %v, want errBackupInvalid", err)
			}
		})
	}
}

func TestRestoreRefusesNonEmptyTargetUnlessForced(t *testing.T) {
	a, src := newBackupApp(t)
	putFile(t, a, "default", "/x.md", "# x", "text/markdown")
	dest := filepath.Join(t.TempDir(), "backup")
	if err := createBackup(dest, src.DBPath, src.ContentDir, "1.0.0"); err != nil {
		t.Fatalf("createBackup: %v", err)
	}

	targetRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetRoot, "preexisting.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	target := cliPaths{DataDir: targetRoot, DBPath: filepath.Join(targetRoot, "clio.db"), ContentDir: filepath.Join(targetRoot, "content")}
	if _, err := restoreBackup(dest, target.DataDir, target.DBPath, target.ContentDir, false); err == nil {
		t.Fatal("restore into a non-empty target should fail without force")
	}
	if _, err := restoreBackup(dest, target.DataDir, target.DBPath, target.ContentDir, true); err != nil {
		t.Fatalf("forced restore: %v", err)
	}
	restored := openAppAt(t, target.DBPath, target.ContentDir)
	if e := entryRepresentation(t, restored, "default", "/x.md"); e["path"] != "/x.md" {
		t.Errorf("forced restore entry = %#v", e)
	}
}

func TestBackupRestoreCommands(t *testing.T) {
	a, src := newBackupApp(t)
	putFile(t, a, "default", "/cli.md", "# cli body", "text/markdown")

	dest := filepath.Join(t.TempDir(), "cli-backup")
	if err := backupCommand([]string{dest}, func(key string) string {
		if key == "CLIO_DATA_DIR" {
			return src.DataDir
		}
		return ""
	}, io.Discard); err != nil {
		t.Fatalf("backupCommand: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, backupManifestName)); err != nil {
		t.Fatalf("backup manifest missing: %v", err)
	}

	dstRoot := t.TempDir()
	var out bytes.Buffer
	if err := restoreCommand([]string{dest, "--force"}, func(key string) string {
		if key == "CLIO_DATA_DIR" {
			return dstRoot
		}
		return ""
	}, &out); err != nil {
		t.Fatalf("restoreCommand: %v", err)
	}
	if !strings.Contains(out.String(), "restored from") {
		t.Errorf("restore output = %q", out.String())
	}
	restored := openAppAt(t, filepath.Join(dstRoot, "clio.db"), filepath.Join(dstRoot, "content"))
	if e := entryRepresentation(t, restored, "default", "/cli.md"); e["path"] != "/cli.md" {
		t.Errorf("restored entry = %#v", e)
	}

	// A CLIO_DB override is honoured, and argument validation rejects misuse.
	paths := resolveCLIPaths(func(key string) string {
		if key == "CLIO_DB" {
			return "/tmp/custom.db"
		}
		return ""
	})
	if paths.DBPath != "/tmp/custom.db" || paths.ContentDir != filepath.Join("./data", "content") {
		t.Errorf("resolved paths = %#v", paths)
	}
	if err := backupCommand(nil, func(string) string { return "" }, io.Discard); err == nil {
		t.Error("backupCommand without a destination should fail")
	}
	if err := restoreCommand([]string{dest, "extra"}, func(string) string { return "" }, io.Discard); err == nil {
		t.Error("restoreCommand with two sources should fail")
	}
}

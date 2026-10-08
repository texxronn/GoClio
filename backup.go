package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A backup captures the SQLite database and the content tree together, as
// required by section 57: content-entry IDs, entry timestamps and
// agent-supplied extracted text are durable state the filesystem alone cannot
// reproduce (section 64.11). The database is snapshotted with SQLite's
// VACUUM INTO, which writes one consistent, compact file (including the FTS5
// index) and needs no WAL sidecars. A manifest describes the archive.
const (
	backupFormat        = "clio-backup"
	backupFormatVersion = 1
	backupManifestName  = "manifest.json"
	backupDatabaseName  = "clio.db"
	backupContentName   = "content"
)

// errBackupInvalid marks a source directory that is not a readable Clio backup.
var errBackupInvalid = errors.New("invalid backup")

// backupManifest is the metadata written at the root of a backup directory. It
// is written last so an interrupted backup is never mistaken for a complete
// one.
type backupManifest struct {
	Format         string `json:"format"`
	FormatVersion  int    `json:"format_version"`
	ProductVersion string `json:"product_version"`
	CreatedAt      string `json:"created_at"`
	Database       string `json:"database"`
	Content        string `json:"content"`
}

// cliPaths is the data location a CLI subcommand operates on. It mirrors the
// resolution in main: CLIO_DATA_DIR, an optional CLIO_DB override, and the
// content tree at CLIO_DATA_DIR/content.
type cliPaths struct {
	DataDir    string
	DBPath     string
	ContentDir string
}

func resolveCLIPaths(getenv func(string) string) cliPaths {
	dataDir := envValue(getenv, "CLIO_DATA_DIR", "./data")
	dbPath := envValue(getenv, "CLIO_DB", filepath.Join(dataDir, "clio.db"))
	return cliPaths{DataDir: dataDir, DBPath: dbPath, ContentDir: filepath.Join(dataDir, "content")}
}

func envValue(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

// backupCommand implements `clio backup <dest>`.
func backupCommand(args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: clio backup <dest>")
	}
	paths := resolveCLIPaths(getenv)
	if err := createBackup(args[0], paths.DBPath, paths.ContentDir, version); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "backup written to %s\n", args[0])
	return nil
}

// restoreCommand implements `clio restore <src> [--force]`.
func restoreCommand(args []string, getenv func(string) string, stdout io.Writer) error {
	force := false
	positional := []string{}
	for _, arg := range args {
		switch arg {
		case "--force", "-force":
			force = true
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return errors.New("usage: clio restore <src> [--force]")
	}
	paths := resolveCLIPaths(getenv)
	report, err := restoreBackup(positional[0], paths.DataDir, paths.DBPath, paths.ContentDir, force)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "restored from %s: %d project(s), %d content entries (reconciled +%d -%d ~%d)\n",
		positional[0], report.Projects, report.Entries, report.Added, report.Removed, report.Refreshed)
	return nil
}

// createBackup writes a consistent backup of the database at dbPath and the
// content tree at contentDir into dest: a VACUUM INTO database snapshot, a
// recursive content copy, and a manifest. dest must not exist or must be an
// empty directory (section 57).
func createBackup(dest, dbPath, contentDir, productVersion string) error {
	if err := prepareBackupDir(dest); err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); err != nil {
		// Without this check SQLite would silently create an empty database,
		// producing a backup that does not hold any durable state.
		return fmt.Errorf("database %s: %w", dbPath, err)
	}
	if err := backupDatabase(dbPath, filepath.Join(dest, backupDatabaseName)); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	if err := copyTree(contentDir, filepath.Join(dest, backupContentName)); err != nil {
		return fmt.Errorf("copy content: %w", err)
	}
	manifest := backupManifest{
		Format:         backupFormat,
		FormatVersion:  backupFormatVersion,
		ProductVersion: productVersion,
		CreatedAt:      formatUTC(time.Now()),
		Database:       backupDatabaseName,
		Content:        backupContentName,
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, backupManifestName), append(raw, '\n'), 0644)
}

// prepareBackupDir creates dest, or accepts an existing empty directory. A
// destination that holds anything is refused so a backup never mixes with
// unrelated files.
func prepareBackupDir(dest string) error {
	info, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return os.MkdirAll(dest, 0755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("backup destination %s is not a directory", dest)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("backup destination %s is not empty", dest)
	}
	return nil
}

// backupDatabase snapshots dbPath into dest with VACUUM INTO. The snapshot is
// transactionally consistent, includes the FTS5 index, and is self-contained
// (no -wal/-shm sidecars). VACUUM INTO refuses to overwrite an existing file.
func backupDatabase(dbPath, dest string) error {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	dsn := "file:" + url.PathEscape(abs) + "?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL"
	db, err := sql.Open(querySQLiteDriver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	_, err = db.Exec(`VACUUM INTO ?`, dest)
	return err
}

// copyTree copies a directory tree to dest, creating dest if needed. Regular
// files and directories are reproduced; symbolic links and special files are
// skipped, matching reconciliation, which never follows symbolic links
// (section 64.2). A missing source yields an empty tree.
func copyTree(src, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("content root %s is not a directory", src)
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, fileInfo.Mode().Perm())
	})
}

// copyFile copies one regular file, preserving its permission bits.
func copyFile(src, dest string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readBackupManifest reads and validates the manifest of a backup directory. It
// rejects an unknown format or version and refuses manifest names that could
// escape the source directory.
func readBackupManifest(src string) (backupManifest, error) {
	var manifest backupManifest
	raw, err := os.ReadFile(filepath.Join(src, backupManifestName))
	if err != nil {
		return manifest, fmt.Errorf("%w: %v", errBackupInvalid, err)
	}
	if err := decodeJSON(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("%w: manifest is not valid JSON", errBackupInvalid)
	}
	if manifest.Format != backupFormat {
		return manifest, fmt.Errorf("%w: unexpected format %q", errBackupInvalid, manifest.Format)
	}
	if manifest.FormatVersion != backupFormatVersion {
		return manifest, fmt.Errorf("%w: unsupported format version %d", errBackupInvalid, manifest.FormatVersion)
	}
	for _, name := range []string{manifest.Database, manifest.Content} {
		if !safeBackupName(name) {
			return manifest, fmt.Errorf("%w: unsafe backup entry %q", errBackupInvalid, name)
		}
		if _, err := os.Stat(filepath.Join(src, name)); err != nil {
			return manifest, fmt.Errorf("%w: %s is missing", errBackupInvalid, name)
		}
	}
	return manifest, nil
}

// safeBackupName reports whether a manifest field is a plain relative name that
// cannot point outside the backup directory.
func safeBackupName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return false
	}
	return filepath.Base(name) == name
}

// restoreReport summarises a completed restore and its reconciliation pass.
type restoreReport struct {
	Projects  int
	Entries   int
	Added     int
	Removed   int
	Refreshed int
}

// restoreBackup replaces the database at dbPath and the content tree at
// contentDir with the backup at src, then reconciles the restored catalog with
// the restored files and rebuilds any missing native text (section 64.11).
// Without force it refuses to overwrite existing data; force permits an
// in-place restore.
func restoreBackup(src, dataDir, dbPath, contentDir string, force bool) (restoreReport, error) {
	var report restoreReport
	manifest, err := readBackupManifest(src)
	if err != nil {
		return report, err
	}
	if !force {
		occupied, err := targetHasData(dataDir, dbPath)
		if err != nil {
			return report, err
		}
		if occupied {
			return report, fmt.Errorf("target data directory %s already contains data; pass --force to overwrite", dataDir)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return report, err
	}
	// Restore content first so a database that lives inside the content
	// directory is not wiped after it is copied.
	if err := os.RemoveAll(contentDir); err != nil {
		return report, err
	}
	if err := copyTree(filepath.Join(src, manifest.Content), contentDir); err != nil {
		return report, fmt.Errorf("restore content: %w", err)
	}
	if err := replaceFile(filepath.Join(src, manifest.Database), dbPath); err != nil {
		return report, fmt.Errorf("restore database: %w", err)
	}
	// Reconcile the restored catalog with the restored files: an entry kept in
	// the database keeps its ID and timestamps, a raw file added before the
	// backup is adopted, and an entry whose file vanished is dropped.
	db, err := openDatabase(dbPath)
	if err != nil {
		return report, err
	}
	defer db.Close()
	a := &app{db: db, content: contentDir, contentMu: &sync.Mutex{}}
	if err := a.migrateContentLayout(); err != nil {
		return report, err
	}
	summary, err := a.reconcileAllContentSummary()
	if err != nil {
		return report, err
	}
	report.Added, report.Removed, report.Refreshed = summary.Added, summary.Removed, summary.Refreshed
	if err := db.QueryRow(`SELECT count(*) FROM projects`).Scan(&report.Projects); err != nil {
		return report, err
	}
	if err := db.QueryRow(`SELECT count(*) FROM content_entries`).Scan(&report.Entries); err != nil {
		return report, err
	}
	return report, nil
}

// targetHasData reports whether the restore target already holds data: an
// existing database (or its WAL sidecars), or any file in the data directory.
func targetHasData(dataDir, dbPath string) (bool, error) {
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		exists, err := pathExists(path)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	entries, err := os.ReadDir(dataDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// replaceFile atomically replaces dest with a copy of src, removing any stale
// SQLite WAL sidecars first.
func replaceFile(src, dest string) error {
	for _, path := range []string{dest, dest + "-wal", dest + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return copyFile(src, dest, info.Mode().Perm())
}

package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// contentEntry is the durable identity of one page or file (section 64.2).
// Directories are structural and have no entry.
type contentEntry struct {
	ID          string
	Project     string
	Path        string
	Kind        string
	ContentType string
	Size        int64
	SHA256      string
	Mtime       string
	CreatedAt   string
	UpdatedAt   string
}

// contentRoot is the filesystem subtree for the current project. The content
// root stores one subdirectory per project (sections 65.3 and 65.7).
func (a *app) contentRoot() string { return filepath.Join(a.content, a.projectName()) }

// contentRootChecked returns the current project's content subtree after
// verifying that neither the content directory nor the project subtree is a
// symbolic link and that the project name cannot escape the content directory
// (sections 64.2 and 65.2). Filesystem callers use it so a crafted project name
// or a symlinked root cannot redirect reads, writes or deletes.
func (a *app) contentRootChecked() (string, *apiError) {
	root := a.contentRoot()
	if !within(a.content, root) {
		return "", invalid("Invalid project content root")
	}
	for _, p := range []string{a.content, root} {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", errAPI(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", invalid("Symbolic links are not permitted in the content root")
		}
		if !info.IsDir() {
			return "", conflict("The content root is not a directory")
		}
	}
	return root, nil
}

// fileSHA256 returns the lowercase hex SHA-256 of a file's bytes (section
// 64.2). It streams the file so a rescan can compare content without holding
// the whole file in memory.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func contentKind(path string) string {
	if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".html") {
		return "page"
	}
	return "file"
}

// explicitMediaTypes pins the content types Clio stores most often (documents,
// images, audio) so they do not depend on the base image shipping a
// /etc/mime.types file (section 64.5).
var explicitMediaTypes = map[string]string{
	".flac": "audio/flac", ".mp3": "audio/mpeg", ".m4a": "audio/mp4",
	".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav",
	".pdf": "application/pdf",
}

func contentMediaType(path string) string {
	switch {
	case strings.HasSuffix(path, ".md"):
		return "text/markdown"
	case strings.HasSuffix(path, ".html"):
		return "text/html"
	}
	if mediaType, ok := explicitMediaTypes[strings.ToLower(filepath.Ext(path))]; ok {
		return mediaType
	}
	mediaType := mime.TypeByExtension(filepath.Ext(path))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return mediaType
}

func (a *app) contentEntryByPath(path string) (contentEntry, bool, error) {
	return a.contentEntryWhere(`project=? AND path=?`, a.project, path)
}

// contentEntryByID resolves an entry by its opaque ID within a project. IDs are
// globally unique, but resolution stays project-scoped like every other query
// (section 65.3).
func (a *app) contentEntryByID(id string) (contentEntry, bool, error) {
	return a.contentEntryWhere(`project=? AND id=?`, a.project, id)
}

func (a *app) contentEntryWhere(where string, args ...any) (contentEntry, bool, error) {
	var entry contentEntry
	err := a.db.QueryRow(
		`SELECT id,project,path,kind,content_type,size,sha256,mtime,created_at,updated_at FROM content_entries WHERE `+where,
		args...,
	).Scan(&entry.ID, &entry.Project, &entry.Path, &entry.Kind, &entry.ContentType, &entry.Size, &entry.SHA256, &entry.Mtime, &entry.CreatedAt, &entry.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return contentEntry{}, false, nil
	}
	if err != nil {
		return contentEntry{}, false, err
	}
	return entry, true, nil
}

// sqlExecer is satisfied by *sql.DB and *sql.Tx.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// saveContentEntry records the bytes of a page or file. The entry ID and
// created_at are preserved across replacement; the path stays mutable through
// move (section 64.2).
func (a *app) saveContentEntry(path string, data []byte, createdAt string) error {
	return saveContentEntryExec(a.db, a.project, path, data, createdAt, "")
}

// saveContentEntryTyped records bytes with an explicit content type. Pages keep
// the type implied by their extension (section 64.2); files may carry the
// declared upload type.
func (a *app) saveContentEntryTyped(path string, data []byte, createdAt, contentType string) error {
	return saveContentEntryExec(a.db, a.project, path, data, createdAt, contentType)
}

// contentWrite carries what the catalog and the native text index need for
// bytes about to be committed at a path. It is computed before the content
// lock is taken, so slow extraction never blocks other writers (section 64.6).
type contentWrite struct {
	Size        int64
	SHA256      string
	ContentType string
	Title       string
	Body        string
}

// prepareContentWrite derives a contentWrite from in-memory bytes.
func prepareContentWrite(clean string, data []byte, contentType string) contentWrite {
	if contentKind(clean) == "page" || contentType == "" {
		contentType = contentMediaType(clean)
	}
	sum := sha256.Sum256(data)
	title, body := extractNative(clean, contentType, data)
	return contentWrite{Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), ContentType: contentType, Title: title, Body: body}
}

// saveContentWrite upserts a content entry and its native index row. The
// entry ID and created_at survive replacement (section 64.2); any agent
// extraction is invalidated because the bytes changed (section 64.8).
func saveContentWrite(exec sqlExecer, project, clean string, cw contentWrite, createdAt string) error {
	now := formatUTC(time.Now())
	if createdAt == "" {
		createdAt = now
	}
	if _, err := exec.Exec(
		`INSERT INTO content_entries(id,project,path,kind,content_type,size,sha256,mtime,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'',?,?)
		 ON CONFLICT(project,path) DO UPDATE SET kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,mtime='',updated_at=excluded.updated_at`,
		newID(), project, clean, contentKind(clean), cw.ContentType, cw.Size, cw.SHA256, createdAt, now,
	); err != nil {
		return err
	}
	id, err := contentEntryIDExec(exec, project, clean)
	if err != nil {
		return err
	}
	// Rewritten bytes invalidate any agent extraction (section 64.8): a stale
	// extraction must not shadow the changed content. The native text is then
	// rebuilt below.
	if err = clearAgentSearch(exec, project, id); err != nil {
		return err
	}
	return indexNativeText(exec, project, id, clean, contentKind(clean), cw.Title, cw.Body)
}

func saveContentEntryExec(exec sqlExecer, project, path string, data []byte, createdAt, contentType string) error {
	return saveContentWrite(exec, project, path, prepareContentWrite(path, data, contentType), createdAt)
}

// contentEntryIDExec resolves the entry ID for a path after an upsert.
func contentEntryIDExec(exec sqlExecer, project, path string) (string, error) {
	var id string
	err := exec.QueryRow(`SELECT id FROM content_entries WHERE project=? AND path=?`, project, path).Scan(&id)
	return id, err
}

// deleteContentEntry removes an entry and its derived search row (section 64.6).
func (a *app) deleteContentEntry(path string) error {
	if _, err := a.db.Exec(`DELETE FROM content_search WHERE project=? AND path=?`, a.project, path); err != nil {
		return err
	}
	_, err := a.db.Exec(`DELETE FROM content_entries WHERE project=? AND path=?`, a.project, path)
	return err
}

// deleteContentEntriesUnder removes a subtree of entries and their search rows.
func (a *app) deleteContentEntriesUnder(path string) error {
	prefix := path + "/"
	if _, err := a.db.Exec(
		`DELETE FROM content_search WHERE project=? AND (path=? OR substr(path,1,length(?))=?)`,
		a.project, path, prefix, prefix,
	); err != nil {
		return err
	}
	_, err := a.db.Exec(
		`DELETE FROM content_entries WHERE project=? AND (path=? OR substr(path,1,length(?))=?)`,
		a.project, path, prefix, prefix,
	)
	return err
}

// contentRescan summarises a reconciliation pass (section 64.4).
type contentRescan struct {
	Added     int
	Removed   int
	Refreshed int
}

// reconcileContent brings the project's entries in line with its content
// subtree: an existing path keeps its ID, a new path receives a new ID, and a
// row whose path no longer exists is removed. Symbolic links are never
// followed (section 64.2).
func (a *app) reconcileContent() error {
	_, err := a.rescanContent()
	return err
}

// hashContentFile hashes a file during reconciliation. Tests replace it to
// count how many files a rescan reads.
var hashContentFile = fileSHA256

// rescanContent reconciles the catalog with the filesystem and reports how many
// entries were added, removed and refreshed (section 64.4). It trusts stored
// hashes and metadata for unchanged entries; see rescanContentMode.
func (a *app) rescanContent() (contentRescan, error) { return a.rescanContentMode(false) }

// rescanContentMode reconciles the catalog with the filesystem and reports how
// many entries were added, removed and refreshed (section 64.4). An entry whose
// size and modification time match the catalog is treated as unchanged: a
// normal rescan (full=false) neither re-reads nor re-extracts it when it also
// has a stored hash. With full=true every entry is re-hashed and its native
// index rebuilt. A refreshed entry is one whose stored bytes changed; its
// sha256 and native text are refreshed and stale agent text is invalidated.
// Native extracted text is rebuilt for entries that are new, refreshed, or
// (on a normal rescan) unchanged but not yet known to be indexed (section
// 64.6); agent-supplied rows (section 64.8) are left in place.
func (a *app) rescanContentMode(full bool) (contentRescan, error) {
	summary := contentRescan{}
	existing := map[string]contentEntry{}
	rows, err := a.db.Query(`SELECT id,path,kind,content_type,size,sha256,mtime,created_at FROM content_entries WHERE project=?`, a.project)
	if err != nil {
		return summary, err
	}
	for rows.Next() {
		var entry contentEntry
		if err = rows.Scan(&entry.ID, &entry.Path, &entry.Kind, &entry.ContentType, &entry.Size, &entry.SHA256, &entry.Mtime, &entry.CreatedAt); err != nil {
			rows.Close()
			return summary, err
		}
		existing[entry.Path] = entry
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return summary, err
	}
	rows.Close()

	// Index provenance by entry ID, so a rebuild neither forgets which entries
	// already have text nor clobbers agent-supplied text (sections 64.6, 64.8).
	indexed := map[string]bool{}
	indexRows, err := a.db.Query(`SELECT id FROM content_search WHERE project=?`, a.project)
	if err != nil {
		return summary, err
	}
	for indexRows.Next() {
		var id string
		if err = indexRows.Scan(&id); err != nil {
			indexRows.Close()
			return summary, err
		}
		indexed[id] = true
	}
	if err = indexRows.Err(); err != nil {
		indexRows.Close()
		return summary, err
	}
	indexRows.Close()

	root, rootErr := a.contentRootChecked()
	if rootErr != nil {
		return summary, errors.New(rootErr.Message)
	}
	seen := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if p != root && ignoredContentName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		clean := "/" + filepath.ToSlash(rel)
		seen[clean] = true
		kind, contentType := contentKind(clean), contentMediaType(clean)
		if entry, ok := existing[clean]; ok {
			// Change detection is byte-based, not type-based: an upload may
			// declare a custom content type, so the stored type must not be
			// treated as a change signal. Compare size, kind and modification
			// time; a normal rescan that finds all three unchanged and a
			// stored hash trusts them without reading the file. Otherwise
			// compare the content hash (backfilling it for entries that do not
			// have one yet). Only genuinely changed bytes refresh
			// content_type/size/sha256 and invalidate agent text; an unchanged
			// path preserves its declared type and agent row (sections 64.2,
			// 64.6 and 64.8).
			mtime := formatUTC(info.ModTime())
			changed := entry.Size != info.Size() || entry.Kind != kind
			if !changed && !full && entry.SHA256 != "" && entry.Mtime == mtime {
				// Same size, kind and modification time as when the bytes
				// were last hashed: trust the stored hash and index (section
				// 64.2). A normal rescan never re-reads or re-extracts an
				// unchanged entry; `full=true` does.
				return nil
			}
			if !changed {
				sum, hashErr := hashContentFile(p)
				if hashErr != nil {
					return hashErr
				}
				if entry.SHA256 == "" {
					// Backfill the hash and stamp the time; this is not a
					// content change.
					if _, err = a.db.Exec(`UPDATE content_entries SET sha256=?, mtime=? WHERE id=?`, sum, mtime, entry.ID); err != nil {
						return err
					}
					entry.SHA256 = sum
					entry.Mtime = mtime
				}
				changed = entry.SHA256 != sum
				if !changed && entry.Mtime != mtime {
					// The bytes match the stored hash but the file was touched;
					// stamp the new time so the next rescan can trust it.
					if _, err = a.db.Exec(`UPDATE content_entries SET mtime=? WHERE id=?`, mtime, entry.ID); err != nil {
						return err
					}
					entry.Mtime = mtime
				}
			}
			if !changed {
				// Unchanged: rebuild the derived text only when it is missing.
				if !indexed[entry.ID] {
					if err = a.indexContentFile(entry.ID, clean, kind, contentType, p); err != nil {
						return err
					}
				}
				return nil
			}
			sum, hashErr := hashContentFile(p)
			if hashErr != nil {
				return hashErr
			}
			if _, err = a.db.Exec(
				`UPDATE content_entries SET kind=?,content_type=?,size=?,sha256=?,mtime=?,updated_at=? WHERE id=?`,
				kind, contentType, info.Size(), sum, mtime, formatUTC(time.Now()), entry.ID,
			); err != nil {
				return err
			}
			// The bytes changed, so any agent extraction is stale and must not
			// shadow the new content (section 64.8).
			if err = clearAgentSearch(a.db, a.project, entry.ID); err != nil {
				return err
			}
			summary.Refreshed++
			return a.indexContentFile(entry.ID, clean, kind, contentType, p)
		}
		id := newID()
		now := formatUTC(time.Now())
		result, err := a.db.Exec(
			`INSERT OR IGNORE INTO content_entries(id,project,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, a.project, clean, kind, contentType, info.Size(), "", now, now,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 {
			summary.Added++
		} else if id, err = contentEntryIDExec(a.db, a.project, clean); err != nil {
			// Lost a race with another writer; adopt the winning ID.
			return err
		}
		if !indexed[id] {
			return a.indexContentFile(id, clean, kind, contentType, p)
		}
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return summary, walkErr
	}
	for path, entry := range existing {
		if seen[path] {
			continue
		}
		if _, err = a.db.Exec(`DELETE FROM content_entries WHERE id=?`, entry.ID); err != nil {
			return summary, err
		}
		summary.Removed++
	}
	// Drop index rows for entries that no longer exist (removed or renamed
	// on disk); the index is derived and must not outlive its entry.
	if _, err = a.db.Exec(
		`DELETE FROM content_search WHERE project=? AND id NOT IN (SELECT id FROM content_entries WHERE project=?)`,
		a.project, a.project,
	); err != nil {
		return summary, err
	}
	return summary, nil
}

// reconcileAllContent reconciles every project's content subtree.
func (a *app) reconcileAllContent() error {
	_, err := a.reconcileAllContentSummary()
	return err
}

// reconcileAllContentSummary reconciles every project's content subtree and
// totals the per-project rescan summary. Restore uses the totals to report the
// rescan-on-restore pass (section 64.11).
func (a *app) reconcileAllContentSummary() (contentRescan, error) {
	total := contentRescan{}
	rows, err := a.db.Query(`SELECT name FROM projects ORDER BY name`)
	if err != nil {
		return total, err
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return total, err
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return total, err
	}
	rows.Close()
	for _, name := range names {
		summary, err := a.withProject(name).rescanContent()
		if err != nil {
			return total, err
		}
		total.Added += summary.Added
		total.Removed += summary.Removed
		total.Refreshed += summary.Refreshed
	}
	return total, nil
}

// migrateContentLayout moves content published before per-project subtrees into
// the default project's subtree. It is a no-op once the layout is partitioned.
func (a *app) migrateContentLayout() error {
	defaultRoot := filepath.Join(a.content, defaultProject)
	if _, err := os.Stat(defaultRoot); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(a.content)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	// A top-level directory named after a project means the layout is already
	// partitioned (a project other than default wrote first).
	known, err := a.projectNames()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if known[entry.Name()] {
			return nil
		}
	}
	if err = os.MkdirAll(defaultRoot, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(a.content, entry.Name())
		to := filepath.Join(defaultRoot, entry.Name())
		if err = os.Rename(from, to); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) projectNames() (map[string]bool, error) {
	rows, err := a.db.Query(`SELECT name FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}

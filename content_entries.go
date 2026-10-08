package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
	CreatedAt   string
	UpdatedAt   string
}

// contentRoot is the filesystem subtree for the current project. The content
// root stores one subdirectory per project (sections 65.3 and 65.7).
func (a *app) contentRoot() string { return filepath.Join(a.content, a.projectName()) }

func contentKind(path string) string {
	if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".html") {
		return "page"
	}
	return "file"
}

func contentMediaType(path string) string {
	switch {
	case strings.HasSuffix(path, ".md"):
		return "text/markdown"
	case strings.HasSuffix(path, ".html"):
		return "text/html"
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
		`SELECT id,project,path,kind,content_type,size,sha256,created_at,updated_at FROM content_entries WHERE `+where,
		args...,
	).Scan(&entry.ID, &entry.Project, &entry.Path, &entry.Kind, &entry.ContentType, &entry.Size, &entry.SHA256, &entry.CreatedAt, &entry.UpdatedAt)
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

func saveContentEntryExec(exec sqlExecer, project, path string, data []byte, createdAt, contentType string) error {
	now := formatUTC(time.Now())
	if createdAt == "" {
		createdAt = now
	}
	if contentKind(path) == "page" || contentType == "" {
		contentType = contentMediaType(path)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if _, err := exec.Exec(
		`INSERT INTO content_entries(id,project,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(project,path) DO UPDATE SET kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,updated_at=excluded.updated_at`,
		newID(), project, path, contentKind(path), contentType, len(data), sha, createdAt, now,
	); err != nil {
		return err
	}
	id, err := contentEntryIDExec(exec, project, path)
	if err != nil {
		return err
	}
	// Rewritten bytes invalidate any agent extraction (section 64.8): a stale
	// extraction must not shadow the changed content. The native text is then
	// rebuilt below.
	if err = clearAgentSearch(exec, project, id); err != nil {
		return err
	}
	title, body := nativeExtraction(path, contentType, data)
	return indexNativeText(exec, project, id, path, contentKind(path), title, body)
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

// rescanContent reconciles the catalog with the filesystem and reports how many
// entries were added, removed and refreshed (section 64.4). A refreshed entry
// is one whose stored size or content type no longer matches the file on disk;
// its bytes are not re-hashed, so a stale sha256 is cleared. Native extracted
// text is rebuilt for entries that are new, refreshed or missing an index row
// (section 64.6); agent-supplied rows (section 64.8) are left in place.
func (a *app) rescanContent() (contentRescan, error) {
	summary := contentRescan{}
	existing := map[string]contentEntry{}
	rows, err := a.db.Query(`SELECT id,path,kind,content_type,size,created_at FROM content_entries WHERE project=?`, a.project)
	if err != nil {
		return summary, err
	}
	for rows.Next() {
		var entry contentEntry
		if err = rows.Scan(&entry.ID, &entry.Path, &entry.Kind, &entry.ContentType, &entry.Size, &entry.CreatedAt); err != nil {
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

	root := a.contentRoot()
	seen := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
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
			if entry.Size == info.Size() && entry.Kind == kind && entry.ContentType == contentType {
				// Unchanged: rebuild the derived text only when it is missing.
				if !indexed[entry.ID] {
					if err = a.indexContentFile(entry.ID, clean, kind, contentType, p); err != nil {
						return err
					}
				}
				return nil
			}
			_, err = a.db.Exec(
				`UPDATE content_entries SET kind=?,content_type=?,size=?,sha256='',updated_at=? WHERE id=?`,
				kind, contentType, info.Size(), formatUTC(time.Now()), entry.ID,
			)
			if err != nil {
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

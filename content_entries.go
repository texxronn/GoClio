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
	var entry contentEntry
	err := a.db.QueryRow(
		`SELECT id,project,path,kind,content_type,size,sha256,created_at,updated_at FROM content_entries WHERE project=? AND path=?`,
		a.project, path,
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
}

// saveContentEntry records the bytes of a page or file. The entry ID and
// created_at are preserved across replacement; the path stays mutable through
// move (section 64.2).
func (a *app) saveContentEntry(path string, data []byte, createdAt string) error {
	return saveContentEntryExec(a.db, a.project, path, data, createdAt)
}

func saveContentEntryExec(exec sqlExecer, project, path string, data []byte, createdAt string) error {
	now := formatUTC(time.Now())
	if createdAt == "" {
		createdAt = now
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	_, err := exec.Exec(
		`INSERT INTO content_entries(id,project,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(project,path) DO UPDATE SET kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,updated_at=excluded.updated_at`,
		newID(), project, path, contentKind(path), contentMediaType(path), len(data), sha, createdAt, now,
	)
	return err
}

func (a *app) deleteContentEntry(path string) error {
	_, err := a.db.Exec(`DELETE FROM content_entries WHERE project=? AND path=?`, a.project, path)
	return err
}

func (a *app) deleteContentEntriesUnder(path string) error {
	prefix := path + "/"
	_, err := a.db.Exec(`DELETE FROM content_entries WHERE project=? AND (path=? OR substr(path,1,length(?))=?)`, a.project, path, prefix, prefix)
	return err
}

// reconcileContent brings the project's entries in line with its content
// subtree: an existing path keeps its ID, a new path receives a new ID, and a
// row whose path no longer exists is removed. Symbolic links are never
// followed (section 64.2).
func (a *app) reconcileContent() error {
	existing := map[string]contentEntry{}
	rows, err := a.db.Query(`SELECT id,path,created_at FROM content_entries WHERE project=?`, a.project)
	if err != nil {
		return err
	}
	for rows.Next() {
		var entry contentEntry
		if err = rows.Scan(&entry.ID, &entry.Path, &entry.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		existing[entry.Path] = entry
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

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
		if _, ok := existing[clean]; ok {
			return nil
		}
		now := formatUTC(time.Now())
		_, err = a.db.Exec(
			`INSERT OR IGNORE INTO content_entries(id,project,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			newID(), a.project, clean, contentKind(clean), contentMediaType(clean), info.Size(), "", now, now,
		)
		return err
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return walkErr
	}
	for path, entry := range existing {
		if seen[path] {
			continue
		}
		if _, err = a.db.Exec(`DELETE FROM content_entries WHERE id=?`, entry.ID); err != nil {
			return err
		}
	}
	return nil
}

// reconcileAllContent reconciles every project's content subtree.
func (a *app) reconcileAllContent() error {
	rows, err := a.db.Query(`SELECT name FROM projects ORDER BY name`)
	if err != nil {
		return err
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range names {
		if err = a.withProject(name).reconcileContent(); err != nil {
			return err
		}
	}
	return nil
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

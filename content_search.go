package main

import (
	"io"
	"os"
	"path"
	"strings"
)

// nativeSearchSource marks text extracted by Clio itself (section 64.7).
const nativeSearchSource = "native"

// replaceSearch writes one text-index row for an entry, replacing any existing
// row for the same entry ID (sections 64.6 and 65.7). Content with no text
// leaves no row, so it is not searchable. The source records provenance:
// "native" for Clio's own extraction or "agent:<provider>" for sidecar
// enrichment (sections 64.7 and 64.8).
func replaceSearch(exec sqlExecer, project, id, entryPath, kind, source, title, body string) error {
	if _, err := exec.Exec(`DELETE FROM content_search WHERE project=? AND id=?`, project, id); err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return nil
	}
	_, err := exec.Exec(
		`INSERT INTO content_search(id,project,path,kind,source,title,body) VALUES(?,?,?,?,?,?,?)`,
		id, project, entryPath, kind, source, title, body,
	)
	return err
}

// replaceNativeSearch writes one native text-index row for an entry (section
// 64.6). Native extraction never produces an empty body here because
// replaceSearch drops empty text, so a format with no text leaves no row.
func replaceNativeSearch(exec sqlExecer, project, id, entryPath, kind, title, body string) error {
	return replaceSearch(exec, project, id, entryPath, kind, nativeSearchSource, title, body)
}

// replaceAgentSearch writes one agent-supplied text-index row for an entry
// (section 64.8), replacing any existing native or agent row so the agent text
// becomes the entry's searchable text.
func replaceAgentSearch(exec sqlExecer, project, id, entryPath, kind, source, title, body string) error {
	return replaceSearch(exec, project, id, entryPath, kind, source, title, body)
}

// clearAgentSearch drops agent-provided text for an entry whose bytes Clio has
// rewritten or refreshed, so a stale extraction cannot shadow changed content
// (section 64.8). Any native row is left for the caller to rebuild.
func clearAgentSearch(exec sqlExecer, project, id string) error {
	_, err := exec.Exec(
		`DELETE FROM content_search WHERE project=? AND id=? AND source<>?`,
		project, id, nativeSearchSource,
	)
	return err
}

// indexNativeText records native extracted text for an entry. Agent-supplied
// text (source "agent:<provider>") outranks native text until it is deleted
// (section 64.8), so an existing agent row is left in place.
func indexNativeText(exec sqlExecer, project, id, entryPath, kind, title, body string) error {
	var agent int
	err := exec.QueryRow(
		`SELECT count(*) FROM content_search WHERE project=? AND id=? AND source<>?`,
		project, id, nativeSearchSource,
	).Scan(&agent)
	if err != nil {
		return err
	}
	if agent > 0 {
		return nil
	}
	return replaceNativeSearch(exec, project, id, entryPath, kind, title, body)
}

// indexContentFile reads a file within its limits and indexes its native text.
// A file above the read limit, or one that cannot be read, yields no text.
func (a *app) indexContentFile(id, entryPath, kind, contentType, diskPath string) error {
	f, err := os.Open(diskPath)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, nativeExtractReadLimit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > nativeExtractReadLimit {
		return nil
	}
	title, body := nativeExtraction(entryPath, contentType, data)
	return indexNativeText(a.db, a.project, id, entryPath, kind, title, body)
}

// applyContentRename updates an entry's path and its index rows in one
// transaction: a regular file by ID, or a directory subtree by path prefix
// (section 64.2). Every descendant ID is preserved. A regular-file rename also
// refreshes the index row's kind and title from the new path, and re-extracts
// native text when the extension changes; an agent row survives unchanged bytes
// (sections 64.6 and 64.8).
func (a *app) applyContentRename(id, from, to, kind, contentType string, subtree bool) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	if subtree {
		shift := len(from) + 1
		if _, err = tx.Exec(
			`UPDATE content_entries SET path=? || substr(path,?) WHERE project=? AND (path=? OR substr(path,1,?)=?)`,
			to, shift, a.project, from, shift, from+"/",
		); err != nil {
			tx.Rollback()
			return err
		}
		// A directory move changes only the prefix, so a descendant's basename,
		// kind and title are unchanged.
		if _, err = tx.Exec(
			`UPDATE content_search SET path=? || substr(path,?) WHERE project=? AND (path=? OR substr(path,1,?)=?)`,
			to, shift, a.project, from, shift, from+"/",
		); err != nil {
			tx.Rollback()
			return err
		}
	} else {
		if _, err = tx.Exec(
			`UPDATE content_entries SET path=?,kind=?,content_type=? WHERE project=? AND id=?`,
			to, kind, contentType, a.project, id,
		); err != nil {
			tx.Rollback()
			return err
		}
		// Refresh the index row's path and its metadata from the new path so
		// search never reports a page as a file or matches the old basename.
		if _, err = tx.Exec(
			`UPDATE content_search SET path=?,kind=?,title=? WHERE project=? AND path=?`,
			to, kind, path.Base(to), a.project, from,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// When a regular-file rename changes the extension, re-extract native text
	// from the new path. indexNativeText leaves an existing agent row in place,
	// so enrichment survives an unchanged-bytes rename (section 64.8).
	if !subtree && !extensionEqual(from, to) {
		if diskPath, ae := a.contentPath(to); ae == nil {
			if err = a.indexContentFile(id, to, kind, contentType, diskPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// extensionEqual reports whether two content paths share a file extension,
// case-insensitively.
func extensionEqual(a, b string) bool {
	return strings.EqualFold(path.Ext(a), path.Ext(b))
}

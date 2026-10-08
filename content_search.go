package main

import (
	"io"
	"os"
	"strings"
)

// nativeSearchSource marks text extracted by Clio itself (section 64.7).
const nativeSearchSource = "native"

// replaceNativeSearch writes one native text-index row for an entry, replacing
// any existing row for the same entry ID (sections 64.6 and 65.7). Content with
// no extracted text (binary files and image-only PDFs) leaves no row, so it is
// not searchable natively.
func replaceNativeSearch(exec sqlExecer, project, id, entryPath, kind, title, body string) error {
	if _, err := exec.Exec(`DELETE FROM content_search WHERE project=? AND id=?`, project, id); err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return nil
	}
	_, err := exec.Exec(
		`INSERT INTO content_search(id,project,path,kind,source,title,body) VALUES(?,?,?,?,?,?,?)`,
		id, project, entryPath, kind, nativeSearchSource, title, body,
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
// (section 64.2). Every descendant ID is preserved.
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
		if _, err = tx.Exec(`UPDATE content_search SET path=? WHERE project=? AND path=?`, to, a.project, from); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

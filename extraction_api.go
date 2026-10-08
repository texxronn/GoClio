package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// maxProviderBytes bounds an agent provider identifier (section 64.8). The
	// stored source is "agent:<provider>".
	maxProviderBytes = 128
)

// providerPattern validates an agent provider identifier. It is deliberately
// permissive (the example is "ocr:tesseract") but excludes whitespace and
// control characters, so a provider can never corrupt its source label.
var providerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// agentSource is the index provenance recorded for agent-supplied text
// (sections 64.7 and 64.8).
func agentSource(provider string) string { return "agent:" + provider }

// extractionAPI serves the agent enrichment routes under
// /api/v1/{project}/files/extraction (sections 64.8 and 66.7). The target entry
// is addressed by ?path= or by ?id=; idFromPath carries an entry ID given as a
// path segment, /api/v1/{project}/files/{id}/extraction, for consistency with
// the rest of the files API.
func (a *app) extractionAPI(w http.ResponseWriter, r *http.Request, idFromPath string) {
	switch r.Method {
	case http.MethodGet:
		a.extractionGet(w, r, idFromPath)
	case http.MethodPut:
		a.extractionPut(w, r, idFromPath)
	case http.MethodDelete:
		a.extractionDelete(w, r, idFromPath)
	default:
		writeAPIError(w, methodNotAllowed())
	}
}

// extractionGet returns the entry's current extraction — agent-supplied text if
// present, otherwise native text — together with the provenance and the current
// fingerprint a writer must present (section 64.8). An entry with no extraction
// returns an empty source and text.
func (a *app) extractionGet(w http.ResponseWriter, r *http.Request, idFromPath string) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	entry, _, info, ae := a.resolveExtractionEntry(r, idFromPath)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	source, title, body, err := a.currentExtraction(entry.ID)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	writeJSON(w, 200, a.extractionRepresentation(entry, info, source, title, body))
}

// extractionPut stores agent-supplied text (section 64.8). The request must
// carry a fingerprint that matches the current on-disk bytes, so a stale
// extraction cannot shadow changed content; a mismatch is 409.
func (a *app) extractionPut(w http.ResponseWriter, r *http.Request, idFromPath string) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	entry, _, info, ae := a.resolveExtractionEntry(r, idFromPath)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	input, ae := readJSON(r, bodyLimit)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	fingerprint := strings.TrimSpace(str(input, "fingerprint"))
	if fingerprint == "" {
		writeAPIError(w, invalid("fingerprint is required"))
		return
	}
	provider, ae := validProvider(str(input, "provider"))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	text, ok := input["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		writeAPIError(w, invalid("text is required"))
		return
	}
	if !fingerprintMatches(fingerprint, entry, info) {
		writeAPIError(w, conflict("The fingerprint does not match the current file"))
		return
	}
	title := strings.TrimSpace(str(input, "title"))
	if title == "" {
		title = capIndexedText(path.Base(entry.Path))
	}
	body := capIndexedText(text)
	source := agentSource(provider)
	if err := replaceAgentSearch(a.db, a.project, entry.ID, entry.Path, entry.Kind, source, title, body); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	writeJSON(w, 200, a.extractionRepresentation(entry, info, source, title, body))
}

// extractionDelete removes the agent-provided text and rebuilds the entry's
// native extraction from disk, so the entry falls back to native text (section
// 64.8). It is idempotent and always 204 once the entry resolves.
func (a *app) extractionDelete(w http.ResponseWriter, r *http.Request, idFromPath string) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	entry, target, _, ae := a.resolveExtractionEntry(r, idFromPath)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if err := clearAgentSearch(a.db, a.project, entry.ID); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if err := a.indexContentFile(entry.ID, entry.Path, entry.Kind, entry.ContentType, target); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveExtractionEntry resolves the target of an enrichment request, then
// requires the entry to be a regular file on disk. The entry is named by a
// path-segment ID (idFromPath), or by exactly one of ?id= or ?path= (section
// 64.8). A raw file without an entry is adopted, matching the files API
// (section 64.4), so it can still be enriched.
func (a *app) resolveExtractionEntry(r *http.Request, idFromPath string) (contentEntry, string, os.FileInfo, *apiError) {
	var (
		entry contentEntry
		found bool
		err   error
		clean string
	)
	if idFromPath == "" {
		q := r.URL.Query()
		hasPath, hasID := q.Has("path"), q.Has("id")
		if hasPath == hasID {
			return contentEntry{}, "", nil, invalid("Provide exactly one of path or id")
		}
		if hasID {
			idFromPath = strings.TrimSpace(q.Get("id"))
			if idFromPath == "" {
				return contentEntry{}, "", nil, invalid("id is required")
			}
		} else {
			var ae *apiError
			clean, ae = canonicalContentPath(q.Get("path"))
			if ae != nil {
				return contentEntry{}, "", nil, ae
			}
			entry, found, err = a.contentEntryByPath(clean)
			if err != nil {
				return contentEntry{}, "", nil, errAPI(err)
			}
		}
	}
	if idFromPath != "" {
		entry, found, err = a.contentEntryByID(idFromPath)
		if err != nil {
			return contentEntry{}, "", nil, errAPI(err)
		}
		if !found {
			return contentEntry{}, "", nil, missing("File")
		}
		clean = entry.Path
	}
	target, ae := a.contentPath(clean)
	if ae != nil {
		return contentEntry{}, "", nil, ae
	}
	info, statErr := os.Lstat(target)
	if os.IsNotExist(statErr) {
		return contentEntry{}, "", nil, missing("File")
	}
	if statErr != nil {
		return contentEntry{}, "", nil, errAPI(statErr)
	}
	if !info.Mode().IsRegular() {
		return contentEntry{}, "", nil, missing("File")
	}
	if !found {
		entry, ae = a.entryForExistingFile(clean, info)
		if ae != nil {
			return contentEntry{}, "", nil, ae
		}
	}
	return entry, target, info, nil
}

// currentExtraction returns the entry's searchable text and its provenance,
// preferring agent-supplied text over native text (section 64.8). An entry
// with no index row returns empty values.
func (a *app) currentExtraction(id string) (string, string, string, error) {
	var source, title, body string
	err := a.db.QueryRow(
		`SELECT source,title,body FROM content_search WHERE project=? AND id=? ORDER BY (source=?) ASC LIMIT 1`,
		a.project, id, nativeSearchSource,
	).Scan(&source, &title, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", err
	}
	return source, title, body, nil
}

// extractionRepresentation is the wire form of an entry's extraction (section
// 64.8): the current text and source, plus the fingerprint to present on a
// write-back.
func (a *app) extractionRepresentation(entry contentEntry, info os.FileInfo, source, title, text string) map[string]any {
	return map[string]any{
		"id":           entry.ID,
		"path":         entry.Path,
		"kind":         entry.Kind,
		"content_type": entry.ContentType,
		"fingerprint":  entryFingerprint(info),
		"source":       source,
		"title":        title,
		"text":         text,
		"indexed":      source != "",
	}
}

// validProvider validates an agent provider identifier (section 64.8).
func validProvider(raw string) (string, *apiError) {
	provider := strings.TrimSpace(raw)
	if provider == "" {
		return "", invalid("provider is required")
	}
	if len(provider) > maxProviderBytes || !providerPattern.MatchString(provider) {
		return "", invalid("provider is invalid")
	}
	return provider, nil
}

// entryFingerprint is the canonical fingerprint of an entry's on-disk bytes:
// size and modified time (section 64.8). It is stable to the second, matching
// the RFC 3339 form in the specification.
func entryFingerprint(info os.FileInfo) string {
	return fmt.Sprintf("%d:%s", info.Size(), info.ModTime().UTC().Format(time.RFC3339))
}

// fingerprintMatches reports whether an agent-supplied fingerprint matches the
// current entry (section 64.8): size and modified time, or the entry's sha256
// when it is known. Times are compared to the second so a round-tripped
// fingerprint is accepted regardless of sub-second precision or timezone.
func fingerprintMatches(supplied string, entry contentEntry, info os.FileInfo) bool {
	supplied = strings.TrimSpace(supplied)
	if supplied == "" {
		return false
	}
	if entry.SHA256 != "" && supplied == entry.SHA256 {
		return true
	}
	sizePart, mtimePart, ok := strings.Cut(supplied, ":")
	if !ok {
		return false
	}
	size, err := strconv.ParseInt(sizePart, 10, 64)
	if err != nil {
		return false
	}
	mtime, err := time.Parse(time.RFC3339, mtimePart)
	if err != nil {
		return false
	}
	return size == info.Size() && mtime.UTC().Truncate(time.Second).Equal(info.ModTime().UTC().Truncate(time.Second))
}

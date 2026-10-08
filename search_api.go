package main

import (
	"html"
	"net/http"
	"strings"
)

const (
	// maxSearchQueryBytes bounds a search query (section 64.7). An empty or
	// oversized query is rejected with 422.
	maxSearchQueryBytes = 1024
	// snippetStart and snippetEnd delimit matched terms in a search snippet
	// (section 64.7). They are private-use mathematical brackets that do not
	// occur in normal text; Clio adds them around matches. The API returns the
	// snippet as plain text, and the UI escapes it before turning the sentinels
	// into highlight elements, so indexed content can never inject markup.
	snippetStart    = "\u27e6"
	snippetEnd      = "\u27e7"
	snippetEllipsis = "..."
	snippetTokens   = 32
)

// searchAPI serves GET /api/v1/{project}/search?q=... (sections 64.7 and 66.7):
// a project-scoped, relevance-ordered, paged search over the extracted text of
// pages and files.
func (a *app) searchAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, methodNotAllowed())
		return
	}
	q := queryValues(r)
	raw := first(q, "q")
	if strings.TrimSpace(raw) == "" {
		writeAPIError(w, invalid("q is required"))
		return
	}
	if len(raw) > maxSearchQueryBytes {
		writeAPIError(w, invalid("q is too long"))
		return
	}
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	match := buildMatchQuery(strings.Fields(raw), false)
	data, total, err := a.searchContent(match, limit, offset)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	writeJSON(w, 200, map[string]any{"data": data, "page": pageInfo(limit, offset, len(data), total)})
}

// buildMatchQuery turns literal, whitespace-separated terms into a safe FTS5
// MATCH expression (section 64.7). Every term is quoted so FTS5 operators such
// as NEAR, OR, NOT and the prefix operator "*" are treated literally and cannot
// inject query syntax; an embedded double quote is escaped by doubling. Terms
// are AND-combined (FTS5's implicit operator is spelled out for clarity). When
// prefix is true a term also matches as a token prefix, for as-you-type search;
// the API exposes literal terms only, as section 64.7 requires. The expression
// searches only the title and body columns so a term in a path or an ID never
// produces a hit.
func buildMatchQuery(terms []string, prefix bool) string {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted := `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
		if prefix {
			quoted += "*"
		}
		parts = append(parts, "{title body} : "+quoted)
	}
	return "(" + strings.Join(parts, ") AND (") + ")"
}

// searchContent runs a built MATCH expression and returns the page of results
// plus the total number of matches (section 64.7). `content_type` comes from the
// entry; `source` and the FTS5 rank come from the index row. Ordering is by
// relevance (bm25 with a title boost) and then by path and ID so paging is
// deterministic (sections 23 and 64.7).
func (a *app) searchContent(match string, limit, offset int) ([]map[string]any, int, error) {
	var total int
	err := a.db.QueryRow(
		`SELECT count(*) FROM content_search JOIN content_entries
		 ON content_entries.project=content_search.project AND content_entries.id=content_search.id
		 WHERE content_search.project=? AND content_search MATCH ?`,
		a.project, match,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := a.db.Query(
		`SELECT content_search.id, content_search.path, content_search.kind,
		        content_entries.content_type, content_search.source,
		        snippet(content_search, -1, ?, ?, ?, ?) AS snippet,
		        -bm25(content_search, 0.0, 0.0, 0.0, 0.0, 0.0, 5.0, 1.0) AS score
		 FROM content_search JOIN content_entries
		 ON content_entries.project=content_search.project AND content_entries.id=content_search.id
		 WHERE content_search.project=? AND content_search MATCH ?
		 ORDER BY score DESC, content_search.path ASC, content_search.id ASC
		 LIMIT ? OFFSET ?`,
		snippetStart, snippetEnd, snippetEllipsis, snippetTokens,
		a.project, match, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	data := []map[string]any{}
	for rows.Next() {
		var id, path, kind, contentType, source, snippet string
		var score float64
		if err = rows.Scan(&id, &path, &kind, &contentType, &source, &snippet, &score); err != nil {
			return nil, 0, err
		}
		data = append(data, map[string]any{
			"id":           id,
			"path":         path,
			"kind":         kind,
			"content_type": contentType,
			"source":       source,
			"snippet":      snippet,
			"score":        score,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return data, total, nil
}

// renderSnippetMarkup escapes an untrusted snippet and then substitutes the
// private sentinels with <mark> elements (section 64.7). Escaping first is what
// stops indexed content from injecting markup; only Clio's sentinels become
// markup. It is the built-in UI's rendering step.
func renderSnippetMarkup(snippet string) string {
	escaped := html.EscapeString(snippet)
	escaped = strings.ReplaceAll(escaped, snippetStart, "<mark>")
	return strings.ReplaceAll(escaped, snippetEnd, "</mark>")
}

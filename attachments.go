package main

import (
	"fmt"
	"strings"
)

// attachmentField locates one attachment field definition so a file delete can
// find the records that reference it (section 64.9).
type attachmentField struct{ group, table, name string }

// validateAttachment checks that an attachment value names a content entry in
// the same project. An attachment stores a content-entry ID, not a path
// (sections 13.3 and 64.9).
func (a *app) validateAttachment(f map[string]any, value any) *apiError {
	name := fmt.Sprint(f["name"])
	id, ok := value.(string)
	if !ok || id == "" {
		return invalid("Invalid attachment value for " + name)
	}
	_, found, err := a.contentEntryByID(id)
	if err != nil {
		return errAPI(err)
	}
	if !found {
		return invalid("Attachment for " + name + " does not reference an existing file in this project")
	}
	return nil
}

// normalizeAccept validates the optional `accept` UI hint of an attachment
// field and normalizes a lone string to a one-element list (section 64.9).
// The hint never replaces validation.
func normalizeAccept(raw any, name string) ([]any, *apiError) {
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, invalid("accept must not be empty for field " + name)
		}
		return []any{value}, nil
	case []any:
		if len(value) == 0 {
			return nil, invalid("accept must not be empty for field " + name)
		}
		out := make([]any, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, invalid("accept entries must be non-empty strings for field " + name)
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, invalid("accept must be a string or an array of strings for field " + name)
	}
}

// attachmentFields lists the attachment fields declared in the project.
func (a *app) attachmentFields() ([]attachmentField, error) {
	rows, err := a.db.Query(`SELECT group_name,table_name,name,definition FROM fields_meta WHERE project=?`, a.project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := []attachmentField{}
	for rows.Next() {
		var group, table, name, raw string
		if err = rows.Scan(&group, &table, &name, &raw); err != nil {
			return nil, err
		}
		def := map[string]any{}
		if err = decodeJSON([]byte(raw), &def); err != nil {
			return nil, err
		}
		if def["type"] == "attachment" {
			fields = append(fields, attachmentField{group, table, name})
		}
	}
	return fields, rows.Err()
}

// attachmentDeleteConflict returns a 409 when any record in the project holds
// one of the given content-entry IDs in an attachment field. Deleting the
// entry (or a directory containing one) is refused until the values are
// cleared; there is no cascade deletion (section 64.9).
func (a *app) attachmentDeleteConflict(ids []string) *apiError {
	if len(ids) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	fields, err := a.attachmentFields()
	if err != nil {
		return errAPI(err)
	}
	for _, f := range fields {
		expression := "json_extract(data, " + indexJSONPathSQL(f.name) + ")"
		rows, err := a.db.Query(
			`SELECT `+expression+` FROM records WHERE project=? AND group_name=? AND table_name=? AND `+expression+` IS NOT NULL`,
			a.project, f.group, f.table,
		)
		if err != nil {
			return errAPI(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return errAPI(err)
			}
			if wanted[id] {
				rows.Close()
				return conflict("File is referenced by an attachment field")
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return errAPI(err)
		}
		rows.Close()
	}
	return nil
}

// contentEntryIDsUnder returns the IDs of the entry at path and every entry in
// its subtree, so a directory delete can check attachment references.
func (a *app) contentEntryIDsUnder(path string) ([]string, error) {
	prefix := path + "/"
	rows, err := a.db.Query(
		`SELECT id FROM content_entries WHERE project=? AND (path=? OR substr(path,1,length(?))=?)`,
		a.project, path, prefix, prefix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

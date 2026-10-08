package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"error"`
	Message string `json:"message"`
}

func invalid(message string) *apiError  { return &apiError{422, "validation_error", message} }
func missing(what string) *apiError     { return &apiError{404, "not_found", what + " not found"} }
func conflict(message string) *apiError { return &apiError{409, "conflict", message} }
func methodNotAllowed() *apiError       { return &apiError{405, "method_not_allowed", "Method not allowed"} }

var reservedRoot = map[string]bool{"api": true, "health": true, "help": true, "assets": true, "t": true, "collections": true, "favicon.svg": true}
var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var fieldTypes = map[string]bool{"string": true, "text": true, "integer": true, "decimal": true, "boolean": true, "date": true, "datetime": true, "enum": true, "url": true, "reference": true}

func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tracked := &responseStatusWriter{ResponseWriter: w}
	w = tracked
	defer func() {
		if v := recover(); v != nil {
			log.Printf("request panic: method=%s", r.Method)
			writeJSON(w, 500, map[string]any{"error": "internal_error", "message": "An internal error occurred"})
		}
		if tracked.status >= http.StatusBadRequest {
			log.Printf("HTTP failure: method=%s status=%d", r.Method, tracked.status)
		}
	}()
	if !a.authorizeRequest(w, r) {
		return
	}
	if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
		writeAPIError(w, &apiError{400, "bad_request", "Malformed query parameters"})
		return
	}
	if r.URL.Path == "/health" || r.URL.Path == "/api/v1/health" {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		if r.URL.Path == "/health" {
			a.healthHTML(w)
		} else {
			a.healthJSON(w)
		}
		return
	}
	if r.URL.Path == "/help" || r.URL.Path == "/api/v1/help" {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		if r.URL.Path == "/help" {
			writeHTML(w, 200, a.helpHTML())
		} else {
			writeText(w, 200, a.helpText(), "text/markdown; charset=utf-8")
		}
		return
	}
	if r.URL.Path == "/assets/clio-markdown.js" {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		writeText(w, 200, markdownAsset, "text/javascript; charset=utf-8")
		return
	}
	if r.URL.Path == "/assets/clio.js" || r.URL.Path == "/assets/clio/v1/clio.js" {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		asset, err := clioJSAsset.ReadFile("assets/clio.js")
		if err != nil {
			writeAPIError(w, errAPI(err))
			return
		}
		writeText(w, 200, string(asset), "text/javascript; charset=utf-8")
		return
	}
	if r.URL.Path == "/favicon.svg" {
		if r.Method != http.MethodGet {
			writeAPIError(w, methodNotAllowed())
			return
		}
		asset, err := clioJSAsset.ReadFile("assets/favicon.svg")
		if err != nil {
			writeAPIError(w, errAPI(err))
			return
		}
		writeText(w, http.StatusOK, string(asset), "image/svg+xml; charset=utf-8")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		a.api(w, r)
		return
	}
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	if r.URL.Path == "/t" || strings.HasPrefix(r.URL.Path, "/t/") {
		a.tableUI(w, r)
		return
	}
	if r.URL.Path == "/collections" || strings.HasPrefix(r.URL.Path, "/collections/") {
		a.collectionBrowserUI(w, r)
		return
	}
	if reservedRoot[firstSegment(r.URL.Path)] {
		writeAPIError(w, missing("Resource"))
		return
	}
	a.contentUI(w, r)
}

type responseStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseStatusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (a *app) api(w http.ResponseWriter, r *http.Request) {
	segments := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/"))
	if len(segments) == 0 {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	switch segments[0] {
	case "projects":
		a.projectsAPI(w, r, segments)
	case "metadata":
		a.metadataAPI(w, r, segments)
	case "groups":
		a.groupsAPI(w, r, segments)
	case "directories":
		a.directoriesAPI(w, r)
	case "pages":
		a.pagesAPI(w, r)
	default:
		writeAPIError(w, missing("Endpoint"))
	}
}

func (a *app) groupsAPI(w http.ResponseWriter, r *http.Request, s []string) {
	method := r.Method
	if len(s) == 1 {
		switch method {
		case "GET":
			items, err := a.listGroups()
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, 200, items)
		case "POST":
			input, e := readJSON(r, bodyLimit)
			if e != nil {
				writeErr(w, e)
				return
			}
			item, e := a.createGroup(input)
			if e != nil {
				writeErr(w, e)
				return
			}
			writeJSON(w, 201, item)
		default:
			writeAPIError(w, methodNotAllowed())
		}
		return
	}
	group, e := validIdentifier(s[1], "group")
	if e != nil {
		writeErr(w, e)
		return
	}
	if len(s) == 2 {
		if method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		item, e := a.group(group)
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, item)
		return
	}
	if s[2] != "tables" {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	if len(s) == 3 {
		switch method {
		case "GET":
			if e = a.requireGroup(group); e != nil {
				writeErr(w, e)
				return
			}
			items, e := a.listTables(group)
			if e != nil {
				writeErr(w, e)
				return
			}
			writeJSON(w, 200, items)
		case "POST":
			input, x := readJSON(r, bodyLimit)
			if x != nil {
				writeErr(w, x)
				return
			}
			item, x := a.createTable(group, input)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 201, item)
		default:
			writeAPIError(w, methodNotAllowed())
		}
		return
	}
	if len(s) < 4 {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	table, e := validIdentifier(s[3], "table")
	if e != nil {
		writeErr(w, e)
		return
	}
	if len(s) == 4 {
		switch method {
		case "GET":
			item, x := a.table(group, table)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, item)
		case "PATCH":
			input, x := readJSON(r, bodyLimit)
			if x != nil {
				writeErr(w, x)
				return
			}
			item, x := a.updateTable(group, table, input)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, item)
		case "DELETE":
			if x := a.deleteTable(group, table); x != nil {
				writeErr(w, x)
				return
			}
			w.WriteHeader(204)
		default:
			writeAPIError(w, methodNotAllowed())
		}
		return
	}
	if s[4] != "records" {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	if len(s) == 5 {
		switch method {
		case "GET":
			q := queryValues(r)
			result, x := a.queryRecords(group, table, q)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, result)
		case "POST":
			input, x := readJSON(r, bodyLimit)
			if x != nil {
				writeErr(w, x)
				return
			}
			item, x := a.createRecord(group, table, input)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 201, item)
		default:
			writeAPIError(w, methodNotAllowed())
		}
		return
	}
	if len(s) == 6 {
		id := s[5]
		switch method {
		case "GET":
			numeric, x := decimalFormat(queryValues(r))
			if x != nil {
				writeErr(w, x)
				return
			}
			item, x := a.getRecordFormatted(group, table, id, numeric)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, item)
		case "PATCH":
			input, x := readJSON(r, bodyLimit)
			if x != nil {
				writeErr(w, x)
				return
			}
			item, x := a.patchRecord(group, table, id, input)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, item)
		case "DELETE":
			if x := a.deleteRecord(group, table, id); x != nil {
				writeErr(w, x)
				return
			}
			w.WriteHeader(204)
		default:
			writeAPIError(w, methodNotAllowed())
		}
		return
	}
	writeAPIError(w, missing("Endpoint"))
}

func (a *app) metadataAPI(w http.ResponseWriter, r *http.Request, s []string) {
	if r.Method != "GET" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	if len(s) == 1 {
		groups, e := a.listGroups()
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"base_url": a.baseURL, "api_version": "v1", "groups": groups})
		return
	}
	if len(s) == 2 && s[1] == "groups" {
		groups, e := a.listGroups()
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, groups)
		return
	}
	if len(s) >= 3 && s[1] == "groups" {
		group, e := validIdentifier(s[2], "group")
		if e != nil {
			writeErr(w, e)
			return
		}
		if len(s) == 3 {
			v, x := a.group(group)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, v)
			return
		}
		if len(s) == 4 && s[3] == "tables" {
			v, x := a.listTables(group)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, v)
			return
		}
		if len(s) == 5 && s[3] == "tables" {
			table, x := validIdentifier(s[4], "table")
			if x != nil {
				writeErr(w, x)
				return
			}
			v, x := a.table(group, table)
			if x != nil {
				writeErr(w, x)
				return
			}
			writeJSON(w, 200, v)
			return
		}
	}
	writeAPIError(w, missing("Metadata"))
}

func (a *app) createGroup(input map[string]any) (map[string]any, *apiError) {
	name, e := validIdentifier(str(input, "name"), "group")
	if e != nil {
		return nil, e
	}
	if _, ok := input["label"]; ok && input["label"] != nil {
		if _, ok := input["label"].(string); !ok {
			return nil, invalid("label must be a string")
		}
	}
	if _, ok := input["description"]; ok && input["description"] != nil {
		if _, ok := input["description"].(string); !ok {
			return nil, invalid("description must be a string")
		}
	}
	label := str(input, "label")
	if label == "" {
		label = name
	}
	desc := str(input, "description")
	order := 0
	if v, ok := input["order"]; ok {
		n, x := integerExact(v)
		if x != nil || n < -2147483648 || n > 2147483647 {
			return nil, invalid("order must be a 32-bit integer")
		}
		order = n
	}
	_, err := a.db.Exec(`INSERT INTO groups_meta(name,label,description,sort_order) VALUES(?,?,?,?)`, name, label, desc, order)
	if err != nil {
		if isConstraint(err) {
			return nil, conflict("Group already exists")
		}
		return nil, errAPI(err)
	}
	return a.group(name)
}

func (a *app) createTable(group string, input map[string]any) (map[string]any, *apiError) {
	if e := a.requireGroup(group); e != nil {
		return nil, e
	}
	name, e := validIdentifier(str(input, "name"), "table")
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"label", "description"} {
		if v, ok := input[key]; ok && v != nil {
			if _, ok := v.(string); !ok {
				return nil, invalid(key + " must be a string")
			}
		}
	}
	label := str(input, "label")
	if label == "" {
		label = name
	}
	desc := str(input, "description")
	kind := str(input, "kind")
	if kind == "" {
		kind = "record"
	}
	if kind != "record" && kind != "timeseries" {
		return nil, invalid("kind must be record or timeseries")
	}
	fields, e := normalizeFields(input["fields"])
	if e != nil {
		return nil, e
	}
	indexes := []schemaIndex{}
	if raw, ok := input["indexes"]; ok {
		indexes, e = normalizeIndexes(raw, fields)
		if e != nil {
			return nil, e
		}
	}
	timestamp := str(input, "timestamp_field")
	if kind == "timeseries" {
		if timestamp == "" {
			return nil, invalid("timeseries tables require a timestamp_field defined in fields")
		}
		f := fieldByName(fields, timestamp)
		if f == nil {
			return nil, invalid("timeseries tables require a timestamp_field defined in fields")
		}
		if f["type"] != "date" && f["type"] != "datetime" {
			return nil, invalid("timestamp_field must have date or datetime type")
		}
		if f["required"] != true {
			return nil, invalid("timestamp_field must be required")
		}
		if role, ok := f["role"].(string); ok && role != "timestamp" {
			return nil, invalid("timestamp_field role must be timestamp")
		}
	} else if timestamp != "" {
		return nil, invalid("timestamp_field is only valid for timeseries tables")
	}
	principalTimestamps := 0
	for _, f := range fields {
		if f["role"] == "timestamp" {
			principalTimestamps++
			if (f["type"] != "date" && f["type"] != "datetime") || f["required"] != true {
				return nil, invalid("A timestamp role requires a required date or datetime field")
			}
			if kind == "timeseries" && f["name"] != timestamp {
				return nil, invalid("A timeseries timestamp role must match timestamp_field")
			}
		}
		if f["type"] == "reference" {
			if _, x := a.table(f["group"].(string), f["table"].(string)); x != nil {
				return nil, x
			}
			if value, ok := f["default"]; ok && value != nil {
				if x := a.validateReference(f, value); x != nil {
					return nil, x
				}
			}
		}
	}
	if principalTimestamps > 1 {
		return nil, invalid("A table may have only one principal timestamp field")
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	encodedIndexes, _ := encodeIndexes(indexes)
	_, err = tx.Exec(`INSERT INTO tables_meta(group_name,name,label,description,kind,timestamp_field,indexes) VALUES(?,?,?,?,?,?,?)`, group, name, label, desc, kind, nullString(timestamp), encodedIndexes)
	if err == nil {
		err = saveFields(tx, group, name, fields)
	}
	if err == nil {
		err = reconcileTableIndexes(tx, group, name, kind, nullString(timestamp), fields, indexes)
	}
	if err != nil {
		tx.Rollback()
		if isConstraint(err) {
			return nil, conflict("Table already exists or an index constraint is violated")
		}
		return nil, errAPI(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return a.table(group, name)
}

func normalizeFields(raw any) ([]map[string]any, *apiError) {
	items, ok := raw.([]any)
	if !ok {
		return nil, invalid("fields must be an array")
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		source, ok := item.(map[string]any)
		if !ok {
			return nil, invalid("Each field must be an object")
		}
		name, e := validIdentifier(str(source, "name"), "field")
		if e != nil {
			return nil, e
		}
		if contains([]string{"id", "created_at", "updated_at"}, name) {
			return nil, invalid("Field name is reserved for record metadata: " + name)
		}
		if seen[name] {
			return nil, invalid("Duplicate field: " + name)
		}
		seen[name] = true
		typ := str(source, "type")
		if !fieldTypes[typ] {
			return nil, invalid("Unsupported field type: " + typ)
		}
		f := map[string]any{}
		for k, v := range source {
			f[k] = v
		}
		f["name"] = name
		f["type"] = typ
		fieldOrder := i
		if value, ok := f["order"]; ok {
			parsed, e := integerExact(value)
			if e != nil || parsed < -2147483648 || parsed > 2147483647 {
				return nil, invalid("order must be a 32-bit integer for field " + name)
			}
			fieldOrder = parsed
		}
		f["order"] = fieldOrder
		if v, ok := f["label"]; !ok || v == nil {
			f["label"] = name
		} else if _, ok := v.(string); !ok {
			return nil, invalid("label must be text for field " + name)
		}
		if v, ok := f["description"]; ok && v != nil {
			if _, ok := v.(string); !ok {
				return nil, invalid("description must be text for field " + name)
			}
		}
		for _, flag := range []string{"required", "readonly", "hidden", "unique"} {
			if v, ok := f[flag]; ok {
				if _, ok := v.(bool); !ok {
					return nil, invalid(flag + " must be boolean for field " + name)
				}
			} else {
				f[flag] = false
			}
		}
		if role, ok := f["role"]; ok {
			if role != "timestamp" {
				return nil, invalid("role must be timestamp for field " + name)
			}
			if typ != "date" && typ != "datetime" {
				return nil, invalid("timestamp role requires a date or datetime field: " + name)
			}
		}
		if typ == "enum" {
			values, ok := f["values"].([]any)
			if !ok || len(values) == 0 {
				return nil, invalid("Enum field " + name + " requires non-empty values")
			}
			seenValues := map[string]bool{}
			for _, v := range values {
				s, ok := v.(string)
				if !ok || seenValues[s] {
					return nil, invalid("Enum values must be unique strings for field " + name)
				}
				seenValues[s] = true
			}
		}
		if typ == "reference" {
			g, ge := validIdentifier(str(f, "group"), "reference group")
			t, te := validIdentifier(str(f, "table"), "reference table")
			if ge != nil {
				return nil, invalid("Reference field " + name + " requires group and table")
			}
			if te != nil {
				return nil, invalid("Reference field " + name + " requires group and table")
			}
			f["group"] = g
			f["table"] = t
		}
		if p, ok := f["pattern"]; ok {
			pattern, ok := p.(string)
			if !ok {
				return nil, invalid("pattern must be text for field " + name)
			}
			if _, e := regexp.Compile(pattern); e != nil {
				return nil, invalid("Invalid pattern for field " + name)
			}
		}
		for _, k := range []string{"min_length", "max_length"} {
			if v, ok := f[k]; ok {
				n, e := integerExact(v)
				if e != nil || n < 0 {
					return nil, invalid(k + " must be a non-negative integer for field " + name)
				}
				f[k] = n
			}
		}
		if mn, mok := f["min_length"].(int); mok {
			if mx, xok := f["max_length"].(int); xok && mn > mx {
				return nil, invalid("min_length must not exceed max_length for field " + name)
			}
		}
		if (has(f, "min_length") || has(f, "max_length")) && !contains([]string{"string", "text", "url", "enum"}, typ) {
			return nil, invalid("Length validation requires a string field: " + name)
		}
		if has(f, "min") || has(f, "max") {
			if !contains([]string{"string", "text", "url", "enum", "integer", "decimal", "date", "datetime"}, typ) {
				return nil, invalid("min/max validation is not supported for field type " + typ)
			}
			for _, key := range []string{"min", "max"} {
				if value, ok := f[key]; ok {
					if typ == "decimal" {
						if _, isString := value.(string); !isString {
							canonical, err := decimalString(value)
							if err != nil {
								return nil, invalid(key + " has an invalid value for field " + name)
							}
							value = canonical
						}
					}
					normalized, e := coerce(f, value)
					if e != nil {
						return nil, invalid(key + " has an invalid value for field " + name)
					}
					f[key] = normalized
				}
			}
			if has(f, "min") && has(f, "max") {
				if c, e := compareBoundary(f["min"], f["max"], typ); e != nil || c > 0 {
					return nil, invalid("min must not exceed max for field " + name)
				}
			}
		}
		if d, ok := f["default"]; ok && d != nil {
			if typ == "integer" {
				if _, ok := d.(json.Number); !ok {
					return nil, invalid("Integer default must be a JSON integer for field " + name)
				}
			}
			if typ == "decimal" {
				if _, ok := d.(string); !ok {
					return nil, invalid("Decimal default must be a JSON string for field " + name)
				}
			}
			v, e := coerce(f, d)
			if e != nil {
				return nil, e
			}
			if e = validateConstraints(f, v); e != nil {
				return nil, e
			}
			f["default"] = v
		} else if f["required"] == true && has(f, "default") {
			return nil, invalid("A required field cannot have a null default: " + name)
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["order"].(int) < out[j]["order"].(int)
	})
	return out, nil
}

// normalizeRemoveFields validates the optional remove_fields property of a
// table metadata PATCH. It returns nil when the property is absent.
func normalizeRemoveFields(raw any) ([]string, *apiError) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, invalid("remove_fields must be an array")
	}
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, invalid("remove_fields entries must be field names")
		}
		id, e := validIdentifier(name, "field")
		if e != nil {
			return nil, e
		}
		if seen[id] {
			return nil, invalid("Duplicate field to remove: " + id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// mergeFields applies a table metadata PATCH to an existing field list. It is
// additive: fields named in the patch are merged onto the matching existing
// definition (or appended when new), and existing fields not named in the
// patch are preserved. Fields named in remove are dropped; removing a field
// still requires that it has no stored values, which updateTable enforces.
func mergeFields(existing []map[string]any, raw any, remove []string) ([]map[string]any, *apiError) {
	items := []any{}
	if raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return nil, invalid("fields must be an array")
		}
		items = list
	}
	existingByName := indexFields(existing)
	merged := make([]any, 0, len(existing)+len(items))
	nextOrder := 0
	for _, f := range existing {
		merged = append(merged, mapClone(f))
		if o, e := integerExact(f["order"]); e == nil && o >= nextOrder {
			nextOrder = o + 1
		}
	}
	supplied := map[string]bool{}
	for _, item := range items {
		source, ok := item.(map[string]any)
		if !ok {
			return nil, invalid("Each field must be an object")
		}
		name, e := validIdentifier(str(source, "name"), "field")
		if e != nil {
			return nil, e
		}
		if supplied[name] {
			return nil, invalid("Duplicate field: " + name)
		}
		supplied[name] = true
		replaced := false
		for i := range merged {
			current := merged[i].(map[string]any)
			if current["name"] != name {
				continue
			}
			updated := mapClone(current)
			for k, v := range source {
				updated[k] = v
			}
			updated["name"] = name
			merged[i] = updated
			replaced = true
			break
		}
		if !replaced {
			added := mapClone(source)
			added["name"] = name
			if value, ok := added["order"]; !ok || value == nil {
				added["order"] = nextOrder
				nextOrder++
			} else if o, e := integerExact(value); e == nil && o >= nextOrder {
				nextOrder = o + 1
			}
			merged = append(merged, added)
		}
	}
	for _, name := range remove {
		if supplied[name] {
			return nil, invalid("Field cannot be added and removed in the same request: " + name)
		}
		if existingByName[name] == nil {
			return nil, invalid("Unknown field to remove: " + name)
		}
	}
	if len(remove) > 0 {
		kept := merged[:0]
		for _, item := range merged {
			if !contains(remove, item.(map[string]any)["name"].(string)) {
				kept = append(kept, item)
			}
		}
		merged = kept
	}
	return normalizeFields(merged)
}

func saveFields(tx *sql.Tx, group, table string, fields []map[string]any) error {
	for i, f := range fields {
		b, e := json.Marshal(f)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`INSERT INTO fields_meta(group_name,table_name,name,position,definition) VALUES(?,?,?,?,?)`, group, table, f["name"], i, string(b)); e != nil {
			return e
		}
	}
	return nil
}

func (a *app) updateTable(group, table string, patch map[string]any) (map[string]any, *apiError) {
	current, e := a.table(group, table)
	if e != nil {
		return nil, e
	}
	for k := range patch {
		if !contains([]string{"label", "description", "fields", "remove_fields", "indexes"}, k) {
			return nil, invalid("Table property cannot be changed: " + k)
		}
	}
	for _, k := range []string{"label", "description"} {
		if v, ok := patch[k]; ok && v != nil {
			if _, ok := v.(string); !ok {
				return nil, invalid(k + " must be a string")
			}
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	defer tx.Rollback()
	effectiveFields, _ := current["fields"].([]map[string]any)
	var storedIndexes string
	if err = tx.QueryRow(`SELECT indexes FROM tables_meta WHERE group_name=? AND name=?`, group, table).Scan(&storedIndexes); err != nil {
		return nil, errAPI(err)
	}
	declarations, err := decodeIndexes(storedIndexes)
	if err != nil {
		return nil, errAPI(err)
	}
	indexesChanged := false
	indexesRaw, indexesPatched := patch["indexes"]
	if has(patch, "label") || has(patch, "description") {
		label := current["label"]
		desc := current["description"]
		if v, ok := patch["label"].(string); ok {
			label = v
		}
		if v, ok := patch["description"].(string); ok {
			desc = v
		}
		if _, err = tx.Exec(`UPDATE tables_meta SET label=?,description=? WHERE group_name=? AND name=?`, label, desc, group, table); err != nil {
			return nil, errAPI(err)
		}
	}
	removeNames, removeErr := normalizeRemoveFields(patch["remove_fields"])
	if removeErr != nil {
		return nil, removeErr
	}
	fieldsRaw, fieldsPatched := patch["fields"]
	if fieldsPatched || len(removeNames) > 0 {
		requested, ae := mergeFields(effectiveFields, fieldsRaw, removeNames)
		if ae != nil {
			return nil, ae
		}
		effectiveFields = requested
		for _, f := range requested {
			if f["type"] != "reference" {
				continue
			}
			if _, x := a.table(f["group"].(string), f["table"].(string)); x != nil {
				return nil, x
			}
			if value, ok := f["default"]; ok && value != nil {
				if x := a.validateReference(f, value); x != nil {
					return nil, x
				}
			}
		}
		old, e := a.fields(group, table)
		if e != nil {
			return nil, e
		}
		oldBy, newBy := indexFields(old), indexFields(requested)
		count, e := a.recordCount(group, table)
		if e != nil {
			return nil, e
		}
		if current["kind"] == "timeseries" {
			f := newBy[current["timestamp_field"].(string)]
			if f == nil || (f["type"] != "date" && f["type"] != "datetime") || f["required"] != true {
				return nil, conflict("The configured timestamp field must remain a required date/datetime field")
			}
		}
		principalTimestamps := 0
		for _, field := range requested {
			if field["role"] == "timestamp" {
				principalTimestamps++
				if (field["type"] != "date" && field["type"] != "datetime") || field["required"] != true {
					return nil, conflict("A timestamp role requires a required date or datetime field")
				}
				if current["kind"] == "timeseries" && field["name"] != current["timestamp_field"] {
					return nil, conflict("A timeseries timestamp role must match timestamp_field")
				}
			}
		}
		if principalTimestamps > 1 {
			return nil, invalid("A table may have only one principal timestamp field")
		}
		for name, of := range oldBy {
			nf := newBy[name]
			if nf == nil {
				var nonNull bool
				rows, x := tx.Query(`SELECT data FROM records WHERE group_name=? AND table_name=?`, group, table)
				if x != nil {
					return nil, errAPI(x)
				}
				for rows.Next() {
					var raw string
					if x = rows.Scan(&raw); x != nil {
						rows.Close()
						return nil, errAPI(x)
					}
					var data map[string]any
					_ = decodeJSON([]byte(raw), &data)
					if data[name] != nil {
						nonNull = true
						break
					}
				}
				rows.Close()
				if nonNull {
					return nil, conflict("Cannot delete field with stored values: " + name)
				}
			} else if of["type"] != nf["type"] && count > 0 {
				return nil, conflict("Cannot change field type while table contains records: " + name)
			}
		}
		for name, nf := range newBy {
			if oldBy[name] == nil && nf["required"] == true && !has(nf, "default") && count > 0 {
				return nil, conflict("A required field added to a non-empty table must have a default: " + name)
			}
		}
		rows, x := tx.Query(`SELECT id,data FROM records WHERE group_name=? AND table_name=?`, group, table)
		if x != nil {
			return nil, errAPI(x)
		}
		type update struct {
			id   string
			data string
		}
		updates := []update{}
		for rows.Next() {
			var id, raw string
			if x = rows.Scan(&id, &raw); x != nil {
				rows.Close()
				return nil, errAPI(x)
			}
			stored := map[string]any{}
			_ = decodeJSON([]byte(raw), &stored)
			updated := map[string]any{}
			for _, f := range requested {
				name := f["name"].(string)
				value, exists := stored[name]
				if !exists && oldBy[name] == nil && has(f, "default") {
					value = f["default"]
				}
				if value == nil && f["required"] == true {
					rows.Close()
					return nil, conflict("Existing records cannot satisfy required field: " + name)
				}
				if value != nil {
					if ae := validateConstraints(f, value); ae != nil {
						rows.Close()
						return nil, ae
					}
				}
				updated[name] = value
			}
			b, _ := json.Marshal(updated)
			updates = append(updates, update{id, string(b)})
		}
		rows.Close()
		for _, u := range updates {
			if _, x = tx.Exec(`UPDATE records SET data=? WHERE id=?`, u.data, u.id); x != nil {
				return nil, errAPI(x)
			}
		}
		if _, err = tx.Exec(`DELETE FROM fields_meta WHERE group_name=? AND table_name=?`, group, table); err != nil {
			return nil, errAPI(err)
		}
		if err = saveFields(tx, group, table, requested); err != nil {
			return nil, errAPI(err)
		}
		indexesChanged = true
	}
	if indexesPatched {
		declarations, e = normalizeIndexes(indexesRaw, effectiveFields)
		if e != nil {
			return nil, e
		}
		indexesChanged = true
	}
	if indexesChanged {
		declarations = validIndexesForFields(declarations, effectiveFields)
		if _, err = tx.Exec(`UPDATE tables_meta SET indexes=? WHERE group_name=? AND name=?`, mustEncodeIndexes(declarations), group, table); err != nil {
			return nil, errAPI(err)
		}
		timestampField := current["timestamp_field"]
		if current["kind"] != "timeseries" {
			timestampField = principalTimestamp(effectiveFields)
		}
		if name, ok := timestampField.(string); ok && name != "" {
			if _, err = tx.Exec(`UPDATE records SET timestamp_value=json_extract(data, ?) WHERE group_name=? AND table_name=?`, indexJSONPath(name), group, table); err != nil {
				return nil, errAPI(err)
			}
		} else if _, err = tx.Exec(`UPDATE records SET timestamp_value=NULL WHERE group_name=? AND table_name=?`, group, table); err != nil {
			return nil, errAPI(err)
		}
		if err = reconcileTableIndexes(tx, group, table, current["kind"].(string), timestampField, effectiveFields, declarations); err != nil {
			return nil, mapIndexError(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return a.table(group, table)
}

func (a *app) deleteTable(group, table string) *apiError {
	if _, e := a.table(group, table); e != nil {
		return e
	}
	count, e := a.recordCount(group, table)
	if e != nil {
		return e
	}
	if count > 0 {
		return conflict("Delete records before deleting this table")
	}
	rows, err := a.db.Query(`SELECT group_name,table_name,definition FROM fields_meta`)
	if err != nil {
		return errAPI(err)
	}
	defer rows.Close()
	for rows.Next() {
		var g, t, d string
		if err = rows.Scan(&g, &t, &d); err != nil {
			return errAPI(err)
		}
		f := map[string]any{}
		if err = decodeJSON([]byte(d), &f); err != nil {
			return errAPI(err)
		}
		if f["type"] == "reference" && f["group"] == group && f["table"] == table {
			return conflict("Table is referenced by another table")
		}
	}
	if err = rows.Err(); err != nil {
		return errAPI(err)
	}
	tx, err := a.db.Begin()
	if err != nil {
		return errAPI(err)
	}
	defer tx.Rollback()
	if err = reconcileTableIndexes(tx, group, table, "record", nil, nil, nil); err != nil {
		return errAPI(err)
	}
	if _, err = tx.Exec(`DELETE FROM tables_meta WHERE group_name=? AND name=?`, group, table); err != nil {
		return errAPI(err)
	}
	if err = tx.Commit(); err != nil {
		return errAPI(err)
	}
	return nil
}

func (a *app) group(name string) (map[string]any, *apiError) {
	var label, desc string
	var order int
	e := a.db.QueryRow(`SELECT label,description,sort_order FROM groups_meta WHERE name=?`, name).Scan(&label, &desc, &order)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, missing("Group")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	tables, ae := a.listTables(name)
	if ae != nil {
		return nil, ae
	}
	return map[string]any{"name": name, "label": label, "description": desc, "order": order, "url": a.baseURL + "/t/" + name, "api_url": a.baseURL + "/api/v1/groups/" + name, "tables": tables}, nil
}
func (a *app) listGroups() ([]map[string]any, *apiError) {
	rows, e := a.db.Query(`SELECT name FROM groups_meta ORDER BY sort_order,label,name`)
	if e != nil {
		return nil, errAPI(e)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			return nil, errAPI(e)
		}
		g, x := a.group(n)
		if x != nil {
			return nil, x
		}
		delete(g, "tables")
		out = append(out, g)
	}
	return out, nil
}
func (a *app) listTables(group string) ([]map[string]any, *apiError) {
	if e := a.requireGroup(group); e != nil {
		return nil, e
	}
	rows, e := a.db.Query(`SELECT name FROM tables_meta WHERE group_name=? ORDER BY label,name`, group)
	if e != nil {
		return nil, errAPI(e)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			return nil, errAPI(e)
		}
		t, x := a.table(group, n)
		if x != nil {
			return nil, x
		}
		out = append(out, t)
	}
	return out, nil
}
func (a *app) table(group, name string) (map[string]any, *apiError) {
	var label, desc, kind string
	var timestamp sql.NullString
	var rawIndexes string
	e := a.db.QueryRow(`SELECT label,description,kind,timestamp_field,indexes FROM tables_meta WHERE group_name=? AND name=?`, group, name).Scan(&label, &desc, &kind, &timestamp, &rawIndexes)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, missing("Table")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	fields, x := a.fields(group, name)
	if x != nil {
		return nil, x
	}
	var ts any
	if timestamp.Valid {
		ts = timestamp.String
	}
	declarations, decodeErr := decodeIndexes(rawIndexes)
	if decodeErr != nil {
		return nil, errAPI(decodeErr)
	}
	indexes := make([]map[string]any, 0, len(declarations))
	for _, declaration := range declarations {
		indexes = append(indexes, map[string]any{"fields": declaration.fields})
	}
	root := a.baseURL + "/t/" + group + "/" + name
	api := a.baseURL + "/api/v1/groups/" + group + "/tables/" + name
	return map[string]any{"group": group, "name": name, "label": label, "description": desc, "kind": kind, "timestamp_field": ts, "indexes": indexes, "fields": fields, "url": root, "api_url": api, "records_url": api + "/records", "record_url_template": root + "/{id}"}, nil
}
func (a *app) fields(group, table string) ([]map[string]any, *apiError) {
	rows, e := a.db.Query(`SELECT definition FROM fields_meta WHERE group_name=? AND table_name=? ORDER BY position`, group, table)
	if e != nil {
		return nil, errAPI(e)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, errAPI(e)
		}
		f := map[string]any{}
		if e = decodeJSON([]byte(raw), &f); e != nil {
			return nil, errAPI(e)
		}
		out = append(out, f)
	}
	return out, nil
}
func (a *app) requireGroup(group string) *apiError {
	var one int
	e := a.db.QueryRow(`SELECT 1 FROM groups_meta WHERE name=?`, group).Scan(&one)
	if errors.Is(e, sql.ErrNoRows) {
		return missing("Group")
	}
	if e != nil {
		return errAPI(e)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	b, e := json.Marshal(value)
	if e != nil {
		status = 500
		b = []byte(`{"error":"internal_error","message":"An internal error occurred"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
func writeHTML(w http.ResponseWriter, status int, value string) {
	writeText(w, status, value, "text/html; charset=utf-8")
}
func writeText(w http.ResponseWriter, status int, value, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, value)
}
func writeAPIError(w http.ResponseWriter, e *apiError) { writeJSON(w, e.Status, e) }
func writeErr(w http.ResponseWriter, e *apiError) {
	if e == nil {
		writeAPIError(w, invalid("Invalid request"))
	} else {
		writeAPIError(w, e)
	}
}
func errAPI(e error) *apiError {
	if e == nil {
		return nil
	}
	log.Printf("internal storage or filesystem error: %v", e)
	return &apiError{500, "internal_error", "An internal error occurred"}
}
func isConstraint(e error) bool {
	return e != nil && (strings.Contains(strings.ToLower(e.Error()), "constraint") || strings.Contains(strings.ToLower(e.Error()), "unique"))
}
func readJSON(r *http.Request, limit int64) (map[string]any, *apiError) {
	body, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if e != nil {
		return nil, invalid("Malformed JSON request")
	}
	if len(body) == 0 {
		return nil, invalid("Request body must contain JSON")
	}
	if int64(len(body)) > limit {
		return nil, &apiError{413, "body_too_large", "Request body is too large"}
	}
	m := map[string]any{}
	if e = decodeJSON(body, &m); e != nil {
		return nil, invalid("Malformed JSON request")
	}
	if m == nil {
		return nil, invalid("JSON body must be an object")
	}
	return m, nil
}
func decodeJSON(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := d.Decode(out); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("extra JSON data")
	}
	return nil
}

func str(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}
func validIdentifier(v, what string) (string, *apiError) {
	if v == "" {
		return "", invalid(what + " name is required")
	}
	v = strings.ToLower(v)
	if !identifierPattern.MatchString(v) || strings.Contains(v, ".") {
		return "", invalid("Invalid " + what + " identifier: " + v)
	}
	return v, nil
}
func splitPath(v string) []string {
	parts := strings.Split(v, "/")
	out := []string{}
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
func firstSegment(v string) string {
	s := splitPath(v)
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
func first(q url.Values, key string) string {
	v := q[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
func queryValues(r *http.Request) url.Values {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return url.Values{}
	}
	return q
}
func has(m map[string]any, key string) bool { _, ok := m[key]; return ok }
func contains(items []string, value string) bool {
	for _, v := range items {
		if v == value {
			return true
		}
	}
	return false
}
func fieldByName(fields []map[string]any, name string) map[string]any {
	for _, f := range fields {
		if f["name"] == name {
			return f
		}
	}
	return nil
}
func indexFields(fields []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, f := range fields {
		if n, ok := f["name"].(string); ok {
			out[n] = f
		}
	}
	return out
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func integerExact(v any) (int, error) {
	s, e := numericString(v)
	if e != nil {
		return 0, e
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok || !r.IsInt() {
		return 0, fmt.Errorf("not integer")
	}
	n := r.Num()
	if !n.IsInt64() {
		return 0, fmt.Errorf("overflow")
	}
	v64 := n.Int64()
	if int64(int(v64)) != v64 {
		return 0, fmt.Errorf("overflow")
	}
	return int(v64), nil
}
func numericString(v any) (string, error) {
	switch n := v.(type) {
	case json.Number:
		return n.String(), nil
	case int:
		return strconv.Itoa(n), nil
	case int64:
		return strconv.FormatInt(n, 10), nil
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	case string:
		return n, nil
	default:
		return "", fmt.Errorf("not numeric")
	}
}
func decimalString(v any) (string, error) {
	s, e := numericString(v)
	if e != nil {
		return "", e
	}
	if !decimalPattern.MatchString(s) {
		return "", fmt.Errorf("invalid decimal")
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return "", fmt.Errorf("invalid decimal")
	}
	return rationalDecimal(r)
}
func esc(v any) string {
	if v == nil {
		return ""
	}
	return html.EscapeString(fmt.Sprint(v))
}
func absContentURL(base, p string) string { return base + urlPath(p) }
func urlPath(p string) string             { return (&url.URL{Path: p}).EscapedPath() }
func mapClone(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var _ = os.ErrNotExist
var _ = path.Clean
var _ = filepath.Separator

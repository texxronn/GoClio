package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type schemaIndex struct {
	fields []string
	unique bool
}

func normalizeIndexes(raw any, fields []map[string]any) ([]schemaIndex, *apiError) {
	items, ok := raw.([]any)
	if !ok {
		return nil, invalid("indexes must be an array")
	}
	known := indexFields(fields)
	out := make([]schemaIndex, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		definition, ok := item.(map[string]any)
		if !ok {
			return nil, invalid("Each index must be an object")
		}
		values, ok := definition["fields"].([]any)
		if !ok || len(values) == 0 {
			return nil, invalid("Each index requires a non-empty fields array")
		}
		idx := schemaIndex{fields: make([]string, 0, len(values))}
		fieldSeen := map[string]bool{}
		for _, value := range values {
			name, ok := value.(string)
			if !ok || known[name] == nil {
				return nil, invalid("Index references an unknown field")
			}
			if fieldSeen[name] {
				return nil, invalid("An index cannot contain a field more than once")
			}
			fieldSeen[name] = true
			idx.fields = append(idx.fields, name)
		}
		key := strings.Join(idx.fields, ",")
		if seen[key] {
			return nil, invalid("Duplicate index declaration")
		}
		seen[key] = true
		out = append(out, idx)
	}
	return out, nil
}

func decodeIndexes(raw string) ([]schemaIndex, error) {
	var declarations [][]string
	if err := json.Unmarshal([]byte(raw), &declarations); err != nil {
		return nil, err
	}
	indexes := make([]schemaIndex, 0, len(declarations))
	for _, fields := range declarations {
		indexes = append(indexes, schemaIndex{fields: fields})
	}
	return indexes, nil
}

func encodeIndexes(indexes []schemaIndex) (string, error) {
	declarations := make([][]string, 0, len(indexes))
	for _, index := range indexes {
		declarations = append(declarations, index.fields)
	}
	encoded, err := json.Marshal(declarations)
	return string(encoded), err
}

func mustEncodeIndexes(indexes []schemaIndex) string {
	encoded, _ := encodeIndexes(indexes)
	return encoded
}

func validIndexesForFields(indexes []schemaIndex, fields []map[string]any) []schemaIndex {
	known := indexFields(fields)
	valid := make([]schemaIndex, 0, len(indexes))
	for _, index := range indexes {
		keep := len(index.fields) > 0
		for _, name := range index.fields {
			if known[name] == nil {
				keep = false
				break
			}
		}
		if keep {
			valid = append(valid, index)
		}
	}
	return valid
}

func principalTimestamp(fields []map[string]any) any {
	for _, field := range fields {
		if field["role"] == "timestamp" {
			return field["name"]
		}
	}
	return nil
}

func temporalField(meta map[string]any, fields []map[string]any) string {
	if field, ok := meta["timestamp_field"].(string); ok && field != "" {
		return field
	}
	if field, ok := principalTimestamp(fields).(string); ok {
		return field
	}
	return ""
}

func temporalFieldFromMap(meta map[string]any, fields map[string]map[string]any) string {
	if field, ok := meta["timestamp_field"].(string); ok && field != "" {
		return field
	}
	for name, field := range fields {
		if field["role"] == "timestamp" {
			return name
		}
	}
	return ""
}

func indexJSONPath(field string) string {
	key, _ := json.Marshal(field)
	return "$" + "." + string(key)
}

func indexJSONPathSQL(field string) string {
	return "'" + strings.ReplaceAll(indexJSONPath(field), "'", "''") + "'"
}

func managedIndexName(group, table string, index schemaIndex) string {
	key := group + "\x00" + table + "\x00" + strings.Join(index.fields, "\x00")
	if index.unique {
		key += "\x00unique"
	}
	// The scope marker changes the derived name so that the table-scoped index
	// definitions below replace any index created by an earlier Clio version.
	key += "\x00scoped-v2"
	sum := sha256.Sum256([]byte(key))
	return "clio_data_" + hex.EncodeToString(sum[:12])
}

// sqlLiteral quotes a string for direct inclusion in a CREATE INDEX statement.
// Managed group and table identifiers are already restricted, but escape
// defensively anyway.
func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// scopedFieldExpression wraps a field expression so that it only contributes a
// non-null value for rows of the declaring table. Managed indexes share the
// single records table; without this scoping a `unique` field in one table
// would enforce uniqueness across every other table's rows as well.
func scopedFieldExpression(group, table, field string, def map[string]any) string {
	expression, _ := sqlFieldExpression(field, def)
	return "CASE WHEN group_name=" + sqlLiteral(group) + " AND table_name=" + sqlLiteral(table) +
		" THEN " + expression + " ELSE NULL END"
}

func tableIndexes(fields []map[string]any, declarations []schemaIndex, kind string, timestampField any) []schemaIndex {
	indexes := []schemaIndex{}
	byFields := map[string]int{}
	add := func(index schemaIndex) {
		key := strings.Join(index.fields, ",")
		if existing, ok := byFields[key]; ok {
			if index.unique {
				indexes[existing].unique = true
			}
			return
		}
		byFields[key] = len(indexes)
		indexes = append(indexes, index)
	}
	for _, field := range fields {
		name := field["name"].(string)
		if field["unique"] == true {
			add(schemaIndex{fields: []string{name}, unique: true})
		}
		if field["type"] == "reference" {
			add(schemaIndex{fields: []string{name}})
		}
	}
	if kind == "timeseries" {
		if name, ok := timestampField.(string); ok && name != "" {
			// The timestamp_value column has a dedicated composite index.
			byFields[name] = -1
		}
	}
	for _, index := range declarations {
		key := strings.Join(index.fields, ",")
		if existing, ok := byFields[key]; ok {
			if existing >= 0 && index.unique {
				indexes[existing].unique = true
			}
			continue
		}
		add(index)
	}
	return indexes
}

func reconcileTableIndexes(tx *sql.Tx, group, table, kind string, timestampField any, fields []map[string]any, declarations []schemaIndex) error {
	indexes := tableIndexes(fields, declarations, kind, timestampField)
	wanted := make(map[string]schemaIndex, len(indexes))
	for _, index := range indexes {
		wanted[managedIndexName(group, table, index)] = index
	}
	rows, err := tx.Query(`SELECT name FROM managed_indexes WHERE group_name=? AND table_name=?`, group, table)
	if err != nil {
		return err
	}
	old := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		old = append(old, name)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range old {
		if _, keep := wanted[name]; !keep {
			if _, err = tx.Exec(`DROP INDEX IF EXISTS "` + name + `"`); err != nil {
				return err
			}
			if _, err = tx.Exec(`DELETE FROM managed_indexes WHERE name=?`, name); err != nil {
				return err
			}
		}
	}
	for name, index := range wanted {
		unique := ""
		if index.unique {
			unique = "UNIQUE "
		}
		expressions := []string{"group_name", "table_name"}
		definitions := indexFields(fields)
		for _, field := range index.fields {
			expressions = append(expressions, scopedFieldExpression(group, table, field, definitions[field]))
		}
		statement := fmt.Sprintf(`CREATE %sINDEX IF NOT EXISTS "%s" ON records(%s)`, unique, name, strings.Join(expressions, ","))
		if _, err = tx.Exec(statement); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT OR REPLACE INTO managed_indexes(name,group_name,table_name) VALUES(?,?,?)`, name, group, table); err != nil {
			return err
		}
	}
	return nil
}

func mapIndexError(err error) *apiError {
	if isConstraint(err) {
		return conflict("Index creation would violate uniqueness for existing records")
	}
	return errAPI(err)
}

// reconcileAllTableIndexes brings every managed index in line with its table's
// current metadata. It runs at database open so that index definitions changed
// by a newer Clio release (for example the table-scoped definitions added in
// v1.2) replace stale ones without requiring special operational steps.
func reconcileAllTableIndexes(db *sql.DB) error {
	rows, err := db.Query(`SELECT group_name,name,kind,timestamp_field,indexes FROM tables_meta`)
	if err != nil {
		return err
	}
	type tableMeta struct {
		group, name, kind string
		timestamp         sql.NullString
		indexes           string
	}
	tables := []tableMeta{}
	for rows.Next() {
		var t tableMeta
		if err = rows.Scan(&t.group, &t.name, &t.kind, &t.timestamp, &t.indexes); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, t)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(tables) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tables {
		fields, err := fieldsInTx(tx, t.group, t.name)
		if err != nil {
			return err
		}
		declarations, err := decodeIndexes(t.indexes)
		if err != nil {
			return err
		}
		var timestamp any
		if t.timestamp.Valid {
			timestamp = t.timestamp.String
		}
		if err = reconcileTableIndexes(tx, t.group, t.name, t.kind, timestamp, fields, declarations); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func fieldsInTx(tx *sql.Tx, group, table string) ([]map[string]any, error) {
	rows, err := tx.Query(`SELECT definition FROM fields_meta WHERE group_name=? AND table_name=? ORDER BY position`, group, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		field := map[string]any{}
		if err = decodeJSON([]byte(raw), &field); err != nil {
			return nil, err
		}
		out = append(out, field)
	}
	return out, rows.Err()
}

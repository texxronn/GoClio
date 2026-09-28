package main

import (
	"container/heap"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func (a *app) createRecord(group, table string, input map[string]any) (map[string]any, *apiError) {
	meta, e := a.table(group, table)
	if e != nil {
		return nil, e
	}
	defs, e := a.fields(group, table)
	if e != nil {
		return nil, e
	}
	values, ae := a.validateValues(input, defs, nil, false)
	if ae != nil {
		return nil, ae
	}
	id := newID()
	now := formatUTC(time.Now())
	var timestamp any
	if f, ok := meta["timestamp_field"].(string); ok {
		timestamp = values[f]
	}
	b, err := json.Marshal(values)
	if err != nil {
		return nil, errAPI(err)
	}
	_, err = a.db.Exec(`INSERT INTO records(id,group_name,table_name,created_at,updated_at,data,timestamp_value) VALUES(?,?,?,?,?,?,?)`, id, group, table, now, now, string(b), timestamp)
	if err != nil {
		if isConstraint(err) {
			return nil, conflict("Record violates a table constraint")
		}
		return nil, errAPI(err)
	}
	return a.getRecord(group, table, id)
}

func (a *app) patchRecord(group, table, id string, patch map[string]any) (map[string]any, *apiError) {
	meta, e := a.table(group, table)
	if e != nil {
		return nil, e
	}
	current, e := a.getRecord(group, table, id)
	if e != nil {
		return nil, e
	}
	defs, e := a.fields(group, table)
	if e != nil {
		return nil, e
	}
	existing := map[string]any{}
	for _, f := range defs {
		n := f["name"].(string)
		existing[n] = current[n]
	}
	values, ae := a.validateValues(patch, defs, existing, true)
	if ae != nil {
		return nil, ae
	}
	b, err := json.Marshal(values)
	if err != nil {
		return nil, errAPI(err)
	}
	now := formatUTC(time.Now())
	var timestamp any
	if f, ok := meta["timestamp_field"].(string); ok {
		timestamp = values[f]
	}
	_, err = a.db.Exec(`UPDATE records SET updated_at=?,data=?,timestamp_value=? WHERE id=? AND group_name=? AND table_name=?`, now, string(b), timestamp, id, group, table)
	if err != nil {
		return nil, errAPI(err)
	}
	return a.getRecord(group, table, id)
}

func (a *app) validateValues(input map[string]any, defs []map[string]any, existing map[string]any, patch bool) (map[string]any, *apiError) {
	for k := range input {
		if k == "id" || k == "created_at" || k == "updated_at" {
			return nil, invalid("System fields cannot be set: " + k)
		}
	}
	by := indexFields(defs)
	for k := range input {
		f := by[k]
		if f == nil {
			return nil, invalid("Unknown field: " + k)
		}
		if f["readonly"] == true {
			return nil, invalid("Field is read-only: " + k)
		}
	}
	out := map[string]any{}
	for _, f := range defs {
		name := f["name"].(string)
		value, provided := input[name]
		if patch && !provided {
			value = existing[name]
		} else if !provided {
			if d, ok := f["default"]; ok {
				value = d
			}
		}
		if value == nil && f["required"] == true {
			return nil, invalid(name + " is required and may not be null")
		}
		if value != nil {
			if f["type"] == "integer" {
				if _, ok := value.(json.Number); !ok {
					return nil, invalid("Invalid integer value for " + name + ": use a JSON integer")
				}
			}
			if f["type"] == "decimal" {
				if _, ok := value.(string); !ok {
					return nil, invalid("Invalid decimal value for " + name + ": use a JSON string")
				}
			}
			v, e := coerce(f, value)
			if e != nil {
				return nil, e
			}
			if e = validateConstraints(f, v); e != nil {
				return nil, e
			}
			if f["type"] == "reference" {
				if e = a.validateReference(f, v); e != nil {
					return nil, e
				}
			}
			value = v
		}
		out[name] = value
	}
	return out, nil
}

func (a *app) validateReference(f map[string]any, value any) *apiError {
	var one int
	e := a.db.QueryRow(`SELECT 1 FROM records WHERE id=? AND group_name=? AND table_name=?`, value, f["group"], f["table"]).Scan(&one)
	if errors.Is(e, sql.ErrNoRows) {
		return invalid("Referenced record does not exist for field " + fmt.Sprint(f["name"]))
	}
	if e != nil {
		return errAPI(e)
	}
	return nil
}

func (a *app) getRecord(group, table, id string) (map[string]any, *apiError) {
	if _, e := a.table(group, table); e != nil {
		return nil, e
	}
	var created, updated, raw string
	e := a.db.QueryRow(`SELECT created_at,updated_at,data FROM records WHERE group_name=? AND table_name=? AND id=?`, group, table, id).Scan(&created, &updated, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, missing("Record")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	out := map[string]any{}
	if e = decodeJSON([]byte(raw), &out); e != nil {
		return nil, errAPI(e)
	}
	out["id"] = id
	out["created_at"] = created
	out["updated_at"] = updated
	return out, nil
}

func (a *app) recordCount(group, table string) (int, *apiError) {
	var n int
	e := a.db.QueryRow(`SELECT count(*) FROM records WHERE group_name=? AND table_name=?`, group, table).Scan(&n)
	if e != nil {
		return 0, errAPI(e)
	}
	return n, nil
}

func (a *app) deleteRecord(group, table, id string) *apiError {
	if _, e := a.getRecord(group, table, id); e != nil {
		return e
	}
	rows, e := a.db.Query(`SELECT group_name,table_name,name,definition FROM fields_meta`)
	if e != nil {
		return errAPI(e)
	}
	type referenceField struct{ group, table, name string }
	references := []referenceField{}
	for rows.Next() {
		var sg, st, name, raw string
		if e = rows.Scan(&sg, &st, &name, &raw); e != nil {
			return errAPI(e)
		}
		f := map[string]any{}
		if e = decodeJSON([]byte(raw), &f); e != nil {
			rows.Close()
			return errAPI(e)
		}
		if f["group"] == group && f["table"] == table {
			if f["type"] == "reference" {
				references = append(references, referenceField{sg, st, name})
			}
		}
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return errAPI(e)
	}
	rows.Close()
	for _, ref := range references {
		matches, e := a.db.Query(`SELECT data FROM records WHERE group_name=? AND table_name=?`, ref.group, ref.table)
		if e != nil {
			return errAPI(e)
		}
		for matches.Next() {
			var raw string
			if e = matches.Scan(&raw); e != nil {
				matches.Close()
				return errAPI(e)
			}
			values := map[string]any{}
			if e = decodeJSON([]byte(raw), &values); e != nil {
				matches.Close()
				return errAPI(e)
			}
			if values[ref.name] == id {
				matches.Close()
				return conflict("Record is referenced by another record")
			}
		}
		if e = matches.Err(); e != nil {
			matches.Close()
			return errAPI(e)
		}
		matches.Close()
	}
	_, e = a.db.Exec(`DELETE FROM records WHERE group_name=? AND table_name=? AND id=?`, group, table, id)
	if e != nil {
		return errAPI(e)
	}
	return nil
}

func (a *app) queryRecords(group, table string, q url.Values) (map[string]any, *apiError) {
	meta, e := a.table(group, table)
	if e != nil {
		return nil, e
	}
	defs, e := a.fields(group, table)
	if e != nil {
		return nil, e
	}
	by := indexFields(defs)
	if ae := validateFilters(by, q); ae != nil {
		return nil, ae
	}
	timeseries := meta["kind"] == "timeseries"
	if (q.Has("from") || q.Has("to") || q.Has("bucket")) && !timeseries {
		return nil, invalid("from, to and bucket require a timeseries table")
	}
	var from, to *time.Time
	if q.Has("from") {
		t, x := parseRFC3339(first(q, "from"))
		if x != nil {
			return nil, x
		}
		from = &t
	}
	if q.Has("to") {
		t, x := parseRFC3339(first(q, "to"))
		if x != nil {
			return nil, x
		}
		to = &t
	}
	if from != nil && to != nil && to.Before(*from) {
		return nil, invalid("to must not be earlier than from")
	}
	if q.Has("distinct") {
		return a.distinctRecordValues(group, table, by, q, timeseries, meta, from, to)
	}
	if q.Has("bucket") {
		return a.bucketRecordAggregates(group, table, by, q, meta, from, to)
	}
	if q.Has("group_by") || q.Has("aggregate") {
		return a.groupRecordAggregates(group, table, by, q, timeseries, meta, from, to)
	}
	sortField, order := first(q, "sort"), strings.ToLower(first(q, "order"))
	if q.Has("sort") && sortField == "" {
		return nil, invalid("sort must name a field")
	}
	if q.Has("order") && sortField == "" {
		return nil, invalid("order requires sort")
	}
	if q.Has("order") && order != "asc" && order != "desc" {
		return nil, invalid("order must be asc or desc")
	}
	if sortField != "" && by[sortField] == nil {
		return nil, invalid("Unknown field: " + sortField)
	}
	if sortField != "" && order != "" && order != "asc" && order != "desc" {
		return nil, invalid("order must be asc or desc")
	}
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		return nil, ae
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		return nil, ae
	}
	return a.queryRecordPage(group, table, by, q, timeseries, meta, from, to, limit, offset, sortField, order)
}

type recordCandidate struct {
	row         map[string]any
	id          string
	createdAt   time.Time
	createdText string
	validTime   bool
	ordinal     int
	sortValue   any
	sortDef     map[string]any
	sortOrder   string
}

type recordCandidateHeap []recordCandidate

func (h recordCandidateHeap) Len() int           { return len(h) }
func (h recordCandidateHeap) Less(i, j int) bool { return candidateBetter(h[j], h[i]) }
func (h recordCandidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *recordCandidateHeap) Push(value any)    { *h = append(*h, value.(recordCandidate)) }
func (h *recordCandidateHeap) Pop() any {
	old := *h
	n := len(old)
	value := old[n-1]
	old[n-1] = recordCandidate{}
	*h = old[:n-1]
	return value
}

func candidateBetter(a, b recordCandidate) bool {
	if a.sortDef != nil {
		if a.sortValue == nil && b.sortValue != nil {
			return false
		}
		if b.sortValue == nil && a.sortValue != nil {
			return true
		}
		if a.sortValue != nil && b.sortValue != nil {
			c := compareTyped(a.sortValue, b.sortValue, a.sortDef)
			if c != 0 {
				if a.sortOrder == "desc" {
					return c > 0
				}
				return c < 0
			}
		}
		return a.ordinal < b.ordinal
	}
	if a.validTime && b.validTime {
		if a.createdAt.Equal(b.createdAt) {
			return a.ordinal < b.ordinal
		}
		return a.createdAt.After(b.createdAt)
	}
	if a.createdText == b.createdText {
		return a.ordinal < b.ordinal
	}
	return a.createdText > b.createdText
}

func (a *app) legacyQueryRecordPage(group, table string, by map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time, limit, offset int, sortField, order string) (map[string]any, *apiError) {
	maxCandidates := offset + limit
	if maxCandidates < offset {
		maxCandidates = int(^uint(0) >> 1)
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	defer tx.Rollback()
	filtering := false
	for key := range q {
		if strings.HasPrefix(key, "filter.") {
			filtering = true
			break
		}
	}
	needsFullDecode := filtering || from != nil || to != nil || sortField != ""
	query := `SELECT id,created_at,updated_at FROM records WHERE group_name=? AND table_name=?`
	if needsFullDecode {
		query = `SELECT id,created_at,updated_at,data FROM records WHERE group_name=? AND table_name=?`
	}
	rows, err := tx.Query(query, group, table)
	if err != nil {
		return nil, errAPI(err)
	}
	candidates := make(recordCandidateHeap, 0, min(maxCandidates, 1000))
	total, ordinal := 0, 0
	for rows.Next() {
		var id, created, updated, raw string
		if needsFullDecode {
			err = rows.Scan(&id, &created, &updated, &raw)
		} else {
			err = rows.Scan(&id, &created, &updated)
		}
		if err != nil {
			rows.Close()
			return nil, errAPI(err)
		}
		rowOrdinal := ordinal
		ordinal++
		createdTime, parseErr := time.Parse(time.RFC3339Nano, created)
		candidate := recordCandidate{id: id, createdAt: createdTime, createdText: created, validTime: parseErr == nil, ordinal: rowOrdinal, sortOrder: order}
		if sortField != "" {
			candidate.sortDef = by[sortField]
		}
		if !needsFullDecode {
			total++
			if len(candidates) == maxCandidates && !candidateBetter(candidate, candidates[0]) {
				continue
			}
		}
		if needsFullDecode {
			row := map[string]any{}
			if err = decodeJSON([]byte(raw), &row); err != nil {
				rows.Close()
				return nil, errAPI(err)
			}
			row["id"], row["created_at"], row["updated_at"] = id, created, updated
			candidate.sortValue = row[sortField]
			matchesFilters, ae := matches(row, by, q)
			if ae != nil {
				rows.Close()
				return nil, ae
			}
			if !matchesFilters {
				continue
			}
			if timeseries && (from != nil || to != nil) {
				timestamp := meta["timestamp_field"].(string)
				t, x := parseStoredTime(fmt.Sprint(row[timestamp]))
				if x != nil {
					rows.Close()
					return nil, x
				}
				if (from != nil && t.Before(*from)) || (to != nil && !t.Before(*to)) {
					continue
				}
			}
			total++
			candidate.row = row
		}
		if len(candidates) < maxCandidates {
			heap.Push(&candidates, candidate)
		} else if candidateBetter(candidate, candidates[0]) {
			heap.Pop(&candidates)
			heap.Push(&candidates, candidate)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, errAPI(err)
	}
	rows.Close()
	sort.Slice(candidates, func(i, j int) bool {
		return candidateBetter(candidates[i], candidates[j])
	})
	if offset >= len(candidates) {
		if err = tx.Commit(); err != nil {
			return nil, errAPI(err)
		}
		return map[string]any{"data": []map[string]any{}, "page": pageInfo(limit, offset, 0, total)}, nil
	}
	end := len(candidates)
	if len(candidates)-offset > limit {
		end = offset + limit
	}
	selected := candidates[offset:end]
	page := make([]map[string]any, len(selected))
	if needsFullDecode {
		for i := range page {
			page[i] = selected[i].row
		}
	} else {
		loaded, loadErr := loadQueryPage(tx, group, table, selected)
		if loadErr != nil {
			return nil, loadErr
		}
		page = loaded
	}
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return map[string]any{"data": page, "page": pageInfo(limit, offset, len(page), total)}, nil
}

func loadQueryPage(tx *sql.Tx, group, table string, candidates []recordCandidate) ([]map[string]any, *apiError) {
	loaded := make(map[string]map[string]any, len(candidates))
	const batchSize = 500
	for start := 0; start < len(candidates); start += batchSize {
		end := min(start+batchSize, len(candidates))
		batch := candidates[start:end]
		query := `SELECT id,created_at,updated_at,data FROM records WHERE group_name=? AND table_name=? AND id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",") + `)`
		args := make([]any, 0, len(batch)+2)
		args = append(args, group, table)
		for _, candidate := range batch {
			args = append(args, candidate.id)
		}
		rows, err := tx.Query(query, args...)
		if err != nil {
			return nil, errAPI(err)
		}
		for rows.Next() {
			var id, created, updated, raw string
			if err = rows.Scan(&id, &created, &updated, &raw); err != nil {
				rows.Close()
				return nil, errAPI(err)
			}
			row := map[string]any{}
			if err = decodeJSON([]byte(raw), &row); err != nil {
				rows.Close()
				return nil, errAPI(err)
			}
			row["id"], row["created_at"], row["updated_at"] = id, created, updated
			loaded[id] = row
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, errAPI(err)
		}
		rows.Close()
	}
	page := make([]map[string]any, len(candidates))
	for i, candidate := range candidates {
		row := loaded[candidate.id]
		if row == nil {
			return nil, errAPI(fmt.Errorf("query page record disappeared inside read transaction"))
		}
		page[i] = row
	}
	return page, nil
}

func (a *app) eachMatchingRecord(group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time, visit func(map[string]any) *apiError) *apiError {
	rows, err := a.db.Query(`SELECT id,created_at,updated_at,data FROM records WHERE group_name=? AND table_name=?`, group, table)
	if err != nil {
		return errAPI(err)
	}
	for rows.Next() {
		var id, created, updated, raw string
		if err = rows.Scan(&id, &created, &updated, &raw); err != nil {
			rows.Close()
			return errAPI(err)
		}
		row := map[string]any{}
		if err = decodeJSON([]byte(raw), &row); err != nil {
			rows.Close()
			return errAPI(err)
		}
		row["id"], row["created_at"], row["updated_at"] = id, created, updated
		matched, ae := matches(row, defs, q)
		if ae != nil {
			rows.Close()
			return ae
		}
		if !matched {
			continue
		}
		if timeseries && (from != nil || to != nil) {
			timestamp := meta["timestamp_field"].(string)
			t, x := parseStoredTime(fmt.Sprint(row[timestamp]))
			if x != nil {
				rows.Close()
				return x
			}
			if (from != nil && t.Before(*from)) || (to != nil && !t.Before(*to)) {
				continue
			}
		}
		if ae = visit(row); ae != nil {
			rows.Close()
			return ae
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return errAPI(err)
	}
	rows.Close()
	return nil
}

func (a *app) legacyDistinctRecordValues(group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time) (map[string]any, *apiError) {
	field := first(q, "distinct")
	if defs[field] == nil {
		return nil, invalid("Unknown field: " + field)
	}
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		return nil, ae
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		return nil, ae
	}
	values := []any{}
	seen := map[string]bool{}
	if ae = a.eachMatchingRecord(group, table, defs, q, timeseries, meta, from, to, func(row map[string]any) *apiError {
		value := row[field]
		key := distinctValueKey(value, defs[field])
		if !seen[key] {
			seen[key] = true
			values = append(values, value)
		}
		return nil
	}); ae != nil {
		return nil, ae
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i] == nil {
			return false
		}
		if values[j] == nil {
			return true
		}
		return compareTyped(values[i], values[j], defs[field]) < 0
	})
	pageValues := pageSlice(values, limit, offset)
	page := make([]any, len(pageValues))
	copy(page, pageValues)
	return map[string]any{"values": page, "page": pageInfo(limit, offset, len(page), len(values))}, nil
}

func distinctValueKey(value any, def map[string]any) string {
	if value == nil {
		return "<null>"
	}
	typ := fmt.Sprint(def["type"])
	if typ == "integer" || typ == "decimal" {
		if text, err := numericString(value); err == nil {
			if rational, ok := new(big.Rat).SetString(text); ok {
				return typ + ":" + rational.RatString()
			}
		}
	}
	if typ == "datetime" {
		if parsed, err := time.Parse(time.RFC3339Nano, fmt.Sprint(value)); err == nil {
			return typ + ":" + formatUTC(parsed)
		}
	}
	return fmt.Sprintf("%T:%v", value, value)
}

type aggregateAccumulator struct {
	count int
	sum   *big.Rat
	best  any
}

func (a *aggregateAccumulator) add(row map[string]any, operation aggregate, def map[string]any) {
	if operation.fn == "count" && operation.field == "" {
		a.count++
		return
	}
	value := row[operation.field]
	if value == nil {
		return
	}
	a.count++
	switch operation.fn {
	case "sum", "avg":
		if a.sum == nil {
			a.sum = new(big.Rat)
		}
		text, _ := numericString(value)
		part := new(big.Rat)
		part.SetString(text)
		a.sum.Add(a.sum, part)
	case "min":
		if a.best == nil || compareTyped(value, a.best, def) < 0 {
			a.best = value
		}
	case "max":
		if a.best == nil || compareTyped(value, a.best, def) > 0 {
			a.best = value
		}
	}
}

func (a aggregateAccumulator) result(operation aggregate, defs map[string]map[string]any) any {
	switch operation.fn {
	case "count":
		return a.count
	case "sum":
		sum := a.sum
		if sum == nil {
			sum = new(big.Rat)
		}
		if defs[operation.field]["type"] == "integer" {
			return json.Number(sum.Num().String())
		}
		return ratDecimal(sum)
	case "avg":
		if a.count == 0 {
			return nil
		}
		avg := new(big.Rat).Quo(a.sum, big.NewRat(int64(a.count), 1))
		return strings.TrimRight(strings.TrimRight(avg.FloatString(10), "0"), ".")
	case "min", "max":
		return a.best
	default:
		return nil
	}
}

type aggregateAccumulatorSet []aggregateAccumulator

func (a aggregateAccumulatorSet) add(row map[string]any, operations []aggregate, defs map[string]map[string]any) {
	for i, operation := range operations {
		a[i].add(row, operation, defs[operation.field])
	}
}

func (a aggregateAccumulatorSet) result(operations []aggregate, defs map[string]map[string]any) map[string]any {
	result := make(map[string]any, len(operations))
	for i, operation := range operations {
		result[operation.key] = a[i].result(operation, defs)
	}
	return result
}

type aggregateGroup struct {
	key   any
	state aggregateAccumulatorSet
}

func newAggregateState(operations []aggregate) aggregateAccumulatorSet {
	return make(aggregateAccumulatorSet, len(operations))
}

func (a *app) legacyGroupRecordAggregates(group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time) (map[string]any, *apiError) {
	groupField := first(q, "group_by")
	tsGroup := contains([]string{"year", "month", "day", "week"}, groupField) && timeseries
	if groupField != "" && defs[groupField] == nil && !tsGroup {
		return nil, invalid("Unknown field: " + groupField)
	}
	operations, ae := parseAggregates(first(q, "aggregate"), defs, groupField != "")
	if ae != nil {
		return nil, ae
	}
	if groupField == "" {
		state := newAggregateState(operations)
		if ae = a.eachMatchingRecord(group, table, defs, q, timeseries, meta, from, to, func(row map[string]any) *apiError {
			state.add(row, operations, defs)
			return nil
		}); ae != nil {
			return nil, ae
		}
		return map[string]any{"aggregate": state.result(operations, defs)}, nil
	}
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		return nil, ae
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		return nil, ae
	}
	groups := map[string]*aggregateGroup{}
	if ae = a.eachMatchingRecord(group, table, defs, q, timeseries, meta, from, to, func(row map[string]any) *apiError {
		key := row[groupField]
		if tsGroup {
			t, x := parseStoredTime(fmt.Sprint(row[meta["timestamp_field"].(string)]))
			if x != nil {
				return x
			}
			key = timeGroup(t, groupField)
		}
		encoded := fmt.Sprintf("%T:%v", key, key)
		current := groups[encoded]
		if current == nil {
			current = &aggregateGroup{key: key, state: newAggregateState(operations)}
			groups[encoded] = current
		}
		current.state.add(row, operations, defs)
		return nil
	}); ae != nil {
		return nil, ae
	}
	ordered := make([]*aggregateGroup, 0, len(groups))
	for _, current := range groups {
		ordered = append(ordered, current)
	}
	sort.Slice(ordered, func(i, j int) bool { return compareAny(ordered[i].key, ordered[j].key) < 0 })
	result := make([]map[string]any, len(ordered))
	for i, current := range ordered {
		row := map[string]any{groupField: current.key}
		for key, value := range current.state.result(operations, defs) {
			row[key] = value
		}
		result[i] = row
	}
	pageValues := pageSlice(result, limit, offset)
	page := make([]map[string]any, len(pageValues))
	copy(page, pageValues)
	return map[string]any{"groups": page, "page": pageInfo(limit, offset, len(page), len(result))}, nil
}

func (a *app) legacyBucketRecordAggregates(group, table string, defs map[string]map[string]any, q url.Values, meta map[string]any, from, to *time.Time) (map[string]any, *apiError) {
	unit := first(q, "bucket")
	if !contains([]string{"hour", "day", "week", "month"}, unit) {
		return nil, invalid("bucket must be hour, day, week or month")
	}
	operations, ae := parseAggregates(first(q, "aggregate"), defs, false)
	if ae != nil {
		return nil, ae
	}
	limit, ae := intParam(q, "limit", 1000, 1, 1000)
	if ae != nil {
		return nil, ae
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		return nil, ae
	}
	field := meta["timestamp_field"].(string)
	buckets := map[string]*aggregateAccumulatorSet{}
	if ae = a.eachMatchingRecord(group, table, defs, q, true, meta, from, to, func(row map[string]any) *apiError {
		t, x := parseStoredTime(fmt.Sprint(row[field]))
		if x != nil {
			return x
		}
		key := formatUTC(bucketStart(t, unit))
		state := buckets[key]
		if state == nil {
			newState := newAggregateState(operations)
			state = &newState
			buckets[key] = state
		}
		state.add(row, operations, defs)
		return nil
	}); ae != nil {
		return nil, ae
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]map[string]any, len(keys))
	for i, key := range keys {
		row := map[string]any{"bucket_start": key}
		for name, value := range buckets[key].result(operations, defs) {
			row[name] = value
		}
		result[i] = row
	}
	pageValues := pageSlice(result, limit, offset)
	page := make([]map[string]any, len(pageValues))
	copy(page, pageValues)
	return map[string]any{"buckets": page, "page": pageInfo(limit, offset, len(page), len(result))}, nil
}

func validateFilters(defs map[string]map[string]any, q url.Values) *apiError {
	for key, values := range q {
		if !strings.HasPrefix(key, "filter.") {
			continue
		}
		field, op, ae := parseFilterKey(key, defs)
		if ae != nil {
			return ae
		}
		f := defs[field]
		typ := fmt.Sprint(f["type"])
		if op == "isnull" {
			if len(values) == 0 || (strings.ToLower(values[len(values)-1]) != "true" && strings.ToLower(values[len(values)-1]) != "false") {
				return invalid("isnull must be true or false")
			}
			continue
		}
		if op == "contains" {
			if !contains([]string{"string", "text", "url"}, typ) {
				return invalid("contains requires a string field")
			}
			continue
		}
		if op == "in" {
			for _, v := range values {
				if _, e := coerce(f, v); e != nil {
					return e
				}
			}
			continue
		}
		if !contains([]string{"eq", "ne", "gt", "gte", "lt", "lte"}, op) {
			return invalid("Unsupported filter operator: " + op)
		}
		if contains([]string{"gt", "gte", "lt", "lte"}, op) && !contains([]string{"string", "text", "integer", "decimal", "date", "datetime", "enum", "url"}, typ) {
			return invalid(op + " requires an ordered field")
		}
		if len(values) == 0 {
			return invalid("Filter value is required")
		}
		if _, e := coerce(f, values[len(values)-1]); e != nil {
			return e
		}
	}
	return nil
}
func parseFilterKey(key string, defs map[string]map[string]any) (string, string, *apiError) {
	rest := strings.TrimPrefix(key, "filter.")
	for field := range defs {
		if rest == field {
			return field, "eq", nil
		}
		if strings.HasPrefix(rest, field+".") {
			return field, strings.TrimPrefix(rest, field+"."), nil
		}
	}
	return "", "", invalid("Unknown filter field: " + rest)
}
func matches(row map[string]any, defs map[string]map[string]any, q url.Values) (bool, *apiError) {
	for key, values := range q {
		if !strings.HasPrefix(key, "filter.") {
			continue
		}
		field, op, e := parseFilterKey(key, defs)
		if e != nil {
			return false, e
		}
		actual := row[field]
		if op == "isnull" {
			want := strings.EqualFold(values[len(values)-1], "true")
			if (actual == nil) != want {
				return false, nil
			}
			continue
		}
		if op == "in" {
			found := false
			for _, v := range values {
				x, ae := coerce(defs[field], v)
				if ae != nil {
					return false, ae
				}
				if actual != nil && equalValue(actual, x, defs[field]) {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
			continue
		}
		value := values[len(values)-1]
		if op == "contains" {
			if actual == nil || !strings.Contains(strings.ToLower(fmt.Sprint(actual)), strings.ToLower(value)) {
				return false, nil
			}
			continue
		}
		expected, ae := coerce(defs[field], value)
		if ae != nil {
			return false, ae
		}
		cmp := 0
		if actual == nil {
			cmp = -1
		} else {
			cmp = compareTyped(actual, expected, defs[field])
		}
		ok := false
		switch op {
		case "eq":
			ok = cmp == 0
		case "ne":
			ok = cmp != 0
		case "gt":
			ok = cmp > 0
		case "gte":
			ok = cmp >= 0
		case "lt":
			ok = cmp < 0
		case "lte":
			ok = cmp <= 0
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

type aggregate struct{ field, fn, key string }

func parseAggregates(spec string, defs map[string]map[string]any, defaultCount bool) ([]aggregate, *apiError) {
	if spec == "" {
		if defaultCount {
			return []aggregate{{fn: "count", key: "count"}}, nil
		}
		return nil, invalid("aggregate is required with group_by")
	}
	out := []aggregate{}
	for _, part := range strings.Split(spec, ",") {
		p := strings.Split(part, ":")
		if len(p) == 1 && p[0] == "count" {
			out = append(out, aggregate{fn: "count", key: "count"})
			continue
		}
		if len(p) != 2 {
			return nil, invalid("Invalid aggregate: " + part)
		}
		field, fn := p[0], p[1]
		f := defs[field]
		if f == nil {
			return nil, invalid("Unknown field: " + field)
		}
		typ := fmt.Sprint(f["type"])
		if !contains([]string{"sum", "avg", "min", "max", "count"}, fn) {
			return nil, invalid("Unsupported aggregate: " + fn)
		}
		if contains([]string{"sum", "avg"}, fn) && !contains([]string{"integer", "decimal"}, typ) {
			return nil, invalid(fn + " requires a numeric field")
		}
		if contains([]string{"min", "max"}, fn) && !contains([]string{"string", "text", "integer", "decimal", "date", "datetime", "enum", "url"}, typ) {
			return nil, invalid(fn + " requires an ordered field")
		}
		out = append(out, aggregate{field, fn, field + "_" + fn})
	}
	return out, nil
}

func timeGroup(t time.Time, part string) any {
	t = t.UTC()
	switch part {
	case "year":
		return t.Year()
	case "month":
		return t.Format("2006-01")
	case "day":
		return t.Format("2006-01-02")
	default:
		d := (int(t.Weekday()) + 6) % 7
		return t.AddDate(0, 0, -d).Format("2006-01-02")
	}
}
func bucketStart(t time.Time, unit string) time.Time {
	t = t.UTC()
	switch unit {
	case "hour":
		return t.Truncate(time.Hour)
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case "week":
		d := (int(t.Weekday()) + 6) % 7
		return time.Date(t.Year(), t.Month(), t.Day()-d, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
}
func parseRFC3339(s string) (time.Time, *apiError) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return time.Time{}, invalid("from and to must be RFC 3339 timestamps")
	}
	return t.UTC(), nil
}
func parseStoredTime(s string) (time.Time, *apiError) {
	if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
		return t.UTC(), nil
	}
	if d, e := time.Parse("2006-01-02", s); e == nil {
		return d.UTC(), nil
	}
	return time.Time{}, invalid("Time-series timestamps must be RFC 3339 timestamps")
}
func formatUTC(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func intParam(q url.Values, k string, def, min, max int) (int, *apiError) {
	s := first(q, k)
	if !q.Has(k) {
		return def, nil
	}
	n, e := strconv.Atoi(s)
	if e != nil || n < min || n > max {
		return 0, invalid(fmt.Sprintf("%s must be between %d and %d", k, min, max))
	}
	return n, nil
}
func pageSlice[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
func pageInfo(limit, offset, count, total int) map[string]any {
	return map[string]any{"limit": limit, "offset": offset, "count": count, "total": total}
}
func coerce(f map[string]any, value any) (any, *apiError) {
	typ := fmt.Sprint(f["type"])
	name := fmt.Sprint(f["name"])
	switch typ {
	case "string", "text", "url", "enum", "reference":
		s, ok := value.(string)
		if !ok {
			return nil, invalid("Invalid " + typ + " value for " + name)
		}
		if typ == "url" {
			u, e := url.ParseRequestURI(s)
			if e != nil || u.Scheme == "" {
				return nil, invalid(name + " must be an absolute URL")
			}
		}
		if typ == "enum" {
			values, _ := f["values"].([]any)
			found := false
			for _, v := range values {
				if v == s {
					found = true
				}
			}
			if !found {
				return nil, invalid("Invalid value for " + name)
			}
		}
		return s, nil
	case "integer":
		s, e := numericString(value)
		if e != nil {
			return nil, invalid("Invalid integer value for " + name)
		}
		r := new(big.Rat)
		if _, ok := r.SetString(s); !ok || !r.IsInt() {
			return nil, invalid("Invalid integer value for " + name)
		}
		return json.Number(r.Num().String()), nil
	case "decimal":
		s, ok := value.(string)
		if !ok {
			return nil, invalid("Invalid decimal value for " + name + ": use a JSON string")
		}
		canonical, e := decimalString(s)
		if e != nil {
			return nil, invalid("Invalid decimal value for " + name)
		}
		return canonical, nil
	case "boolean":
		if b, ok := value.(bool); ok {
			return b, nil
		}
		return nil, invalid("Invalid boolean value for " + name)
	case "date":
		s, ok := value.(string)
		if !ok {
			return nil, invalid("Invalid date value for " + name)
		}
		t, e := time.Parse("2006-01-02", s)
		if e != nil {
			return nil, invalid("Invalid date value for " + name)
		}
		return t.Format("2006-01-02"), nil
	case "datetime":
		s, ok := value.(string)
		if !ok {
			return nil, invalid("Invalid datetime value for " + name)
		}
		t, e := time.Parse(time.RFC3339Nano, s)
		if e != nil {
			return nil, invalid("Invalid datetime value for " + name)
		}
		return formatUTC(t), nil
	}
	return nil, invalid("Unsupported type: " + typ)
}

func validateConstraints(f map[string]any, value any) *apiError {
	text := fmt.Sprint(value)
	name := fmt.Sprint(f["name"])
	if v, ok := f["min_length"]; ok {
		n, _ := integerExact(v)
		if len([]rune(text)) < n {
			return invalid(name + " is shorter than min_length")
		}
	}
	if v, ok := f["max_length"]; ok {
		n, _ := integerExact(v)
		if len([]rune(text)) > n {
			return invalid(name + " is longer than max_length")
		}
	}
	if p, ok := f["pattern"].(string); ok {
		r, e := regexp.Compile(p)
		if e != nil || !r.MatchString(text) {
			return invalid(name + " does not match pattern")
		}
	}
	typ := fmt.Sprint(f["type"])
	for _, key := range []string{"min", "max"} {
		boundary, ok := f[key]
		if !ok || boundary == nil {
			continue
		}
		c, e := compareBoundary(value, boundary, typ)
		if e != nil {
			return invalid("Invalid min/max validation for " + name)
		}
		if key == "min" && c < 0 {
			return invalid(name + " is below min")
		}
		if key == "max" && c > 0 {
			return invalid(name + " exceeds max")
		}
	}
	return nil
}
func compareBoundary(value, boundary any, typ string) (int, error) {
	if contains([]string{"integer", "decimal"}, typ) {
		a, e := newRat(value)
		if e != nil {
			return 0, e
		}
		b, e := newRat(boundary)
		if e != nil {
			return 0, e
		}
		return a.Cmp(b), nil
	}
	if typ == "date" {
		a, e := time.Parse("2006-01-02", fmt.Sprint(value))
		if e != nil {
			return 0, e
		}
		b, e := time.Parse("2006-01-02", fmt.Sprint(boundary))
		if e != nil {
			return 0, e
		}
		if a.Before(b) {
			return -1, nil
		}
		if a.After(b) {
			return 1, nil
		}
		return 0, nil
	}
	if typ == "datetime" {
		a, e := time.Parse(time.RFC3339Nano, fmt.Sprint(value))
		if e != nil {
			return 0, e
		}
		b, e := time.Parse(time.RFC3339Nano, fmt.Sprint(boundary))
		if e != nil {
			return 0, e
		}
		if a.Before(b) {
			return -1, nil
		}
		if a.After(b) {
			return 1, nil
		}
		return 0, nil
	}
	return strings.Compare(fmt.Sprint(value), fmt.Sprint(boundary)), nil
}
func compareTyped(a, b any, f map[string]any) int {
	typ := fmt.Sprint(f["type"])
	if contains([]string{"integer", "decimal"}, typ) {
		x, e := newRat(a)
		if e == nil {
			y, e := newRat(b)
			if e == nil {
				return x.Cmp(y)
			}
		}
	}
	if typ == "datetime" {
		x, xe := time.Parse(time.RFC3339Nano, fmt.Sprint(a))
		y, ye := time.Parse(time.RFC3339Nano, fmt.Sprint(b))
		if xe == nil && ye == nil {
			if x.Before(y) {
				return -1
			}
			if x.After(y) {
				return 1
			}
			return 0
		}
	}
	return compareAny(a, b)
}
func compareAny(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	if x, e := newRat(a); e == nil {
		if y, e := newRat(b); e == nil {
			return x.Cmp(y)
		}
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}
func equalValue(a, b any, f map[string]any) bool { return compareTyped(a, b, f) == 0 }
func newRat(v any) (*big.Rat, error) {
	s, e := numericString(v)
	if e != nil {
		return nil, e
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, fmt.Errorf("invalid number")
	}
	return r, nil
}
func ratDecimal(r *big.Rat) string {
	s, _ := rationalDecimal(r)
	return s
}
func trimDecimal(s string) string { return strings.TrimRight(strings.TrimRight(s, "0"), ".") }

func rationalDecimal(r *big.Rat) (string, error) {
	den := new(big.Int).Set(r.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	countTwo, countFive := 0, 0
	for new(big.Int).Mod(den, two).Sign() == 0 {
		den.Div(den, two)
		countTwo++
	}
	for new(big.Int).Mod(den, five).Sign() == 0 {
		den.Div(den, five)
		countFive++
	}
	if den.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("non-terminating decimal")
	}
	scale := max(countTwo, countFive)
	if scale > 10000 {
		return "", fmt.Errorf("decimal scale too large")
	}
	num := new(big.Int).Set(r.Num())
	if countFive < scale {
		num.Mul(num, new(big.Int).Exp(five, big.NewInt(int64(scale-countFive)), nil))
	}
	if countTwo < scale {
		num.Mul(num, new(big.Int).Exp(two, big.NewInt(int64(scale-countTwo)), nil))
	}
	negative := num.Sign() < 0
	digits := new(big.Int).Abs(num).String()
	if scale > 0 {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		point := len(digits) - scale
		digits = digits[:point] + "." + digits[point:]
		digits = trimDecimal(digits)
	}
	if digits == "" {
		digits = "0"
	}
	if negative && digits != "0" {
		digits = "-" + digits
	}
	return digits, nil
}
func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

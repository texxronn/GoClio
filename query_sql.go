package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

const querySQLiteDriver = "clio-sqlite3"

func init() {
	sql.Register(querySQLiteDriver, &sqlite3.SQLiteDriver{ConnectHook: registerQuerySQLiteFunctions})
}

func registerQuerySQLiteFunctions(conn *sqlite3.SQLiteConn) error {
	if err := conn.RegisterCollation("CLIO_DECIMAL", compareDecimalText); err != nil {
		return err
	}
	if err := conn.RegisterCollation("CLIO_DATETIME", compareDateTimeText); err != nil {
		return err
	}
	if err := conn.RegisterFunc("clio_integer", integerJSONField, true); err != nil {
		return err
	}
	if err := conn.RegisterFunc("clio_contains", func(value any, needle string) bool {
		text, ok := sqliteText(value)
		return ok && strings.Contains(strings.ToLower(text), strings.ToLower(needle))
	}, true); err != nil {
		return err
	}
	if err := conn.RegisterAggregator("clio_sum", newSQLDecimalSum, true); err != nil {
		return err
	}
	return conn.RegisterAggregator("clio_avg", newSQLDecimalAverage, true)
}

func integerJSONField(data, field string) (any, error) {
	var values map[string]any
	if err := decodeJSON([]byte(data), &values); err != nil {
		return nil, err
	}
	value := values[field]
	if value == nil {
		return nil, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, fmt.Errorf("integer field %q is not a JSON number", field)
	}
	return number.String(), nil
}

func sqliteText(value any) (string, bool) {
	switch text := value.(type) {
	case string:
		return text, true
	case []byte:
		if text == nil {
			return "", false
		}
		return string(text), true
	default:
		return "", false
	}
}

func compareDecimalText(a, b string) int {
	aneg, bneg := strings.HasPrefix(a, "-"), strings.HasPrefix(b, "-")
	a, b = strings.TrimPrefix(a, "-"), strings.TrimPrefix(b, "-")
	ai, af, _ := strings.Cut(a, ".")
	bi, bf, _ := strings.Cut(b, ".")
	ai, bi = strings.TrimLeft(ai, "0"), strings.TrimLeft(bi, "0")
	if ai == "" {
		ai = "0"
	}
	if bi == "" {
		bi = "0"
	}
	cmp := 0
	switch {
	case len(ai) < len(bi):
		cmp = -1
	case len(ai) > len(bi):
		cmp = 1
	default:
		cmp = strings.Compare(ai, bi)
	}
	if cmp == 0 {
		for i := 0; i < max(len(af), len(bf)); i++ {
			ac, bc := byte('0'), byte('0')
			if i < len(af) {
				ac = af[i]
			}
			if i < len(bf) {
				bc = bf[i]
			}
			if ac < bc {
				cmp = -1
				break
			}
			if ac > bc {
				cmp = 1
				break
			}
		}
	}
	if aneg {
		cmp = -cmp
	}
	if aneg != bneg {
		if aneg {
			return -1
		}
		return 1
	}
	return cmp
}

func compareDateTimeText(a, b string) int {
	if len(a) == 20 && len(b) == 20 && a[19] == 'Z' && b[19] == 'Z' {
		return strings.Compare(a, b)
	}
	if len(a) < 19 || len(b) < 19 {
		return strings.Compare(a, b)
	}
	if cmp := strings.Compare(a[:19], b[:19]); cmp != 0 {
		return cmp
	}
	af, bf := timestampFraction(a), timestampFraction(b)
	for i := 0; i < max(len(af), len(bf)); i++ {
		ac, bc := byte('0'), byte('0')
		if i < len(af) {
			ac = af[i]
		}
		if i < len(bf) {
			bc = bf[i]
		}
		if ac < bc {
			return -1
		}
		if ac > bc {
			return 1
		}
	}
	return 0
}

func timestampFraction(value string) string {
	if len(value) <= 20 || value[19] != '.' {
		return ""
	}
	end := strings.IndexByte(value[20:], 'Z')
	if end < 0 {
		return ""
	}
	return value[20 : 20+end]
}

type sqlDecimalAccumulator struct {
	integer  int64
	rational *big.Rat
	count    int64
}

func (a *sqlDecimalAccumulator) add(text string) error {
	a.count++
	if value, err := strconv.ParseInt(text, 10, 64); err == nil && a.rational == nil {
		sum := a.integer + value
		if (value > 0 && sum < a.integer) || (value < 0 && sum > a.integer) {
			a.rational = new(big.Rat).SetInt64(a.integer)
			a.rational.Add(a.rational, new(big.Rat).SetInt64(value))
		} else {
			a.integer = sum
		}
		return nil
	}
	if a.rational == nil {
		a.rational = new(big.Rat).SetInt64(a.integer)
	}
	part, ok := new(big.Rat).SetString(text)
	if !ok {
		return fmt.Errorf("invalid numeric value in aggregate")
	}
	a.rational.Add(a.rational, part)
	return nil
}

func (a *sqlDecimalAccumulator) sumString() (string, error) {
	if a.rational == nil {
		return strconv.FormatInt(a.integer, 10), nil
	}
	return rationalDecimal(a.rational)
}

func (a *sqlDecimalAccumulator) averageString() (string, bool) {
	if a.count == 0 {
		return "", false
	}
	sum := a.rational
	if sum == nil {
		sum = new(big.Rat).SetInt64(a.integer)
	}
	average := new(big.Rat).Quo(sum, big.NewRat(a.count, 1))
	return trimDecimal(average.FloatString(10)), true
}

type sqlDecimalSum struct{ sqlDecimalAccumulator }

func newSQLDecimalSum() *sqlDecimalSum { return &sqlDecimalSum{} }

func (s *sqlDecimalSum) Step(value any) error {
	text, ok := sqliteText(value)
	if !ok {
		return nil
	}
	return s.add(text)
}

func (s *sqlDecimalSum) Done() (string, error) { return s.sumString() }

type sqlDecimalAverage struct{ sqlDecimalAccumulator }

func newSQLDecimalAverage() *sqlDecimalAverage { return &sqlDecimalAverage{} }

func (a *sqlDecimalAverage) Step(value any) error {
	text, ok := sqliteText(value)
	if !ok {
		return nil
	}
	return a.add(text)
}

func (a *sqlDecimalAverage) Done() (any, error) {
	if a.count == 0 {
		return nil, nil
	}
	average, _ := a.averageString()
	return average, nil
}

func sqlFieldExpression(field string, def map[string]any) (string, []any) {
	path := indexJSONPathSQL(field)
	if def["type"] == "integer" {
		return "CASE WHEN typeof(json_extract(data, " + path + "))='real' THEN clio_integer(data, '" + field + "') ELSE CAST(json_extract(data, " + path + ") AS TEXT) END COLLATE CLIO_DECIMAL", nil
	}
	expression := "json_extract(data, " + path + ")"
	switch def["type"] {
	case "decimal":
		expression += " COLLATE CLIO_DECIMAL"
	case "datetime":
		expression += " COLLATE CLIO_DATETIME"
	}
	return expression, nil
}

func sqlValue(value any, def map[string]any) any {
	if value == nil {
		return nil
	}
	switch def["type"] {
	case "integer":
		if text, ok := sqliteText(value); ok {
			return json.Number(text)
		}
	case "decimal", "string", "text", "url", "enum", "reference", "date", "datetime":
		if text, ok := sqliteText(value); ok {
			return text
		}
	case "boolean":
		if n, ok := value.(int64); ok {
			return n != 0
		}
	}
	return value
}

func filterSQLExpression(field string, def map[string]any) (string, []any) {
	return sqlFieldExpression(field, def)
}

func buildRecordWhere(project, group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time) (string, []any, *apiError) {
	conditions := []string{"project=?", "group_name=?", "table_name=?"}
	args := []any{project, group, table}
	if timeseries && (from != nil || to != nil) {
		if from != nil {
			conditions = append(conditions, "timestamp_value COLLATE CLIO_DATETIME >= ?")
			args = append(args, formatUTC(*from))
		}
		if to != nil {
			conditions = append(conditions, "timestamp_value COLLATE CLIO_DATETIME < ?")
			args = append(args, formatUTC(*to))
		}
	}
	keys := make([]string, 0)
	for key := range q {
		if strings.HasPrefix(key, "filter.") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		field, op, ae := parseFilterKey(key, defs)
		if ae != nil {
			return "", nil, ae
		}
		values := q[key]
		expression, expressionArgs := filterSQLExpression(field, defs[field])
		args = append(args, expressionArgs...)
		switch op {
		case "isnull":
			if strings.EqualFold(values[len(values)-1], "true") {
				conditions = append(conditions, expression+" IS NULL")
			} else {
				conditions = append(conditions, expression+" IS NOT NULL")
			}
		case "contains":
			conditions = append(conditions, "clio_contains("+expression+", ?)")
			args = append(args, values[len(values)-1])
		case "in":
			if len(values) == 0 {
				conditions = append(conditions, "0")
				continue
			}
			placeholders := make([]string, 0, len(values))
			for _, value := range values {
				coerced, _ := coerce(defs[field], value)
				placeholders = append(placeholders, "?")
				args = append(args, filterArgument(coerced))
			}
			conditions = append(conditions, expression+" IN ("+strings.Join(placeholders, ",")+")")
		default:
			coerced, _ := coerce(defs[field], values[len(values)-1])
			operator := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[op]
			condition := expression + " " + operator + " ?"
			if op == "ne" || op == "lt" || op == "lte" {
				condition = "(" + expression + " IS NULL OR " + condition + ")"
				args = append(args, expressionArgs...)
			}
			conditions = append(conditions, condition)
			args = append(args, filterArgument(coerced))
		}
	}
	return strings.Join(conditions, " AND "), args, nil
}

func filterArgument(value any) any {
	if number, ok := value.(json.Number); ok {
		return number.String()
	}
	return value
}

func (a *app) queryRecordPage(group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time, limit, offset int, sortField, order string, numeric bool) (map[string]any, *apiError) {
	where, args, ae := buildRecordWhere(a.project, group, table, defs, q, timeseries, meta, from, to)
	if ae != nil {
		return nil, ae
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRow("SELECT COUNT(*) FROM records WHERE "+where, args...).Scan(&total); err != nil {
		return nil, errAPI(err)
	}
	temporal := temporalFieldFromMap(meta, defs)
	ordering := "created_at COLLATE CLIO_DATETIME DESC, rowid ASC"
	if temporal != "" {
		ordering = "timestamp_value COLLATE CLIO_DATETIME DESC, id DESC"
	}
	pageArgs := append([]any(nil), args...)
	if sortField != "" {
		if order == "" {
			order = "asc"
		}
		if sortField == temporal {
			ordering = "timestamp_value COLLATE CLIO_DATETIME " + strings.ToUpper(order) + ", id " + strings.ToUpper(order)
		} else {
			expression, expressionArgs := sqlFieldExpression(sortField, defs[sortField])
			ordering = "(" + expression + " IS NULL) ASC, " + expression + " " + strings.ToUpper(order) + ", rowid ASC"
			pageArgs = append(pageArgs, expressionArgs...)
			pageArgs = append(pageArgs, expressionArgs...)
		}
	}
	pageArgs = append(pageArgs, limit, offset)
	rows, err := tx.Query("SELECT id,created_at,updated_at,data FROM records WHERE "+where+" ORDER BY "+ordering+" LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		return nil, errAPI(err)
	}
	page := make([]map[string]any, 0, limit)
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
		applyDecimalFormat(row, defs, numeric)
		page = append(page, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, errAPI(err)
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return map[string]any{"data": page, "page": pageInfo(limit, offset, len(page), total)}, nil
}

func (a *app) distinctRecordValues(group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time, numeric bool) (map[string]any, *apiError) {
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
	where, whereArgs, ae := buildRecordWhere(a.project, group, table, defs, q, timeseries, meta, from, to)
	if ae != nil {
		return nil, ae
	}
	expression, expressionArgs := sqlFieldExpression(field, defs[field])
	cte := "WITH filtered AS (SELECT data FROM records WHERE " + where + ") "
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	defer tx.Rollback()
	var total int
	countArgs := append(append([]any(nil), whereArgs...), expressionArgs...)
	countQuery := cte + "SELECT COUNT(*) FROM (SELECT DISTINCT " + expression + " AS value FROM filtered)"
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, errAPI(err)
	}
	query := cte + "SELECT DISTINCT " + expression + " AS value FROM filtered ORDER BY (value IS NULL), value LIMIT ? OFFSET ?"
	queryArgs := append(append([]any(nil), whereArgs...), expressionArgs...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := tx.Query(query, queryArgs...)
	if err != nil {
		return nil, errAPI(err)
	}
	defer rows.Close()
	values := make([]any, 0, limit)
	for rows.Next() {
		var value any
		if err = rows.Scan(&value); err != nil {
			return nil, errAPI(err)
		}
		item := sqlValue(value, defs[field])
		if numeric && defs[field]["type"] == "decimal" {
			if text, ok := item.(string); ok {
				item = json.Number(text)
			}
		}
		values = append(values, item)
	}
	if err = rows.Err(); err != nil {
		return nil, errAPI(err)
	}
	if err = rows.Close(); err != nil {
		return nil, errAPI(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return map[string]any{"values": values, "page": pageInfo(limit, offset, len(values), total)}, nil
}

func aggregateSQLExpression(operation aggregate, defs map[string]map[string]any) (string, []any) {
	if operation.fn == "count" && operation.field == "" {
		return "COUNT(*)", nil
	}
	expression, args := sqlFieldExpression(operation.field, defs[operation.field])
	switch operation.fn {
	case "count":
		return "COUNT(" + expression + ")", args
	case "sum":
		return "COALESCE(clio_sum(" + expression + "), '0')", args
	case "avg":
		return "clio_avg(" + expression + ")", args
	case "min", "max":
		return strings.ToUpper(operation.fn) + "(" + expression + ")", args
	default:
		return "NULL", nil
	}
}

func scanAggregateValue(value any, operation aggregate, defs map[string]map[string]any, numeric bool) any {
	if operation.fn == "count" {
		return value
	}
	if operation.fn == "sum" && defs[operation.field]["type"] == "integer" {
		if text, ok := sqliteText(value); ok {
			return json.Number(text)
		}
	}
	if (operation.fn == "min" || operation.fn == "max") && defs[operation.field]["type"] == "integer" {
		return sqlValue(value, defs[operation.field])
	}
	if text, ok := sqliteText(value); ok {
		if numeric && defs[operation.field]["type"] == "decimal" {
			return json.Number(text)
		}
		return text
	}
	return value
}

func (a *app) groupRecordAggregates(group, table string, defs map[string]map[string]any, q url.Values, timeseries bool, meta map[string]any, from, to *time.Time, numeric bool) (map[string]any, *apiError) {
	groupField := first(q, "group_by")
	tsGroup := contains([]string{"year", "month", "day", "week"}, groupField) && timeseries
	if groupField != "" && defs[groupField] == nil && !tsGroup {
		return nil, invalid("Unknown field: " + groupField)
	}
	operations, ae := parseAggregates(first(q, "aggregate"), defs, groupField != "")
	if ae != nil {
		return nil, ae
	}
	where, whereArgs, ae := buildRecordWhere(a.project, group, table, defs, q, timeseries, meta, from, to)
	if ae != nil {
		return nil, ae
	}
	selects := make([]string, len(operations))
	selectArgs := make([]any, 0)
	for i, operation := range operations {
		expression, args := aggregateSQLExpression(operation, defs)
		selects[i] = expression
		selectArgs = append(selectArgs, args...)
	}
	if groupField == "" {
		query := "WITH filtered AS (SELECT data FROM records WHERE " + where + ") SELECT " + strings.Join(selects, ",") + " FROM filtered"
		args := append(append([]any(nil), whereArgs...), selectArgs...)
		values := make([]any, len(operations))
		if err := a.db.QueryRow(query, args...).Scan(scanDestinations(values)...); err != nil {
			return nil, errAPI(err)
		}
		result := make(map[string]any, len(operations))
		for i, operation := range operations {
			result[operation.key] = scanAggregateValue(values[i], operation, defs, numeric)
		}
		return map[string]any{"aggregate": result}, nil
	}
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		return nil, ae
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		return nil, ae
	}
	groupExpression, groupArgs := sqlGroupExpression(groupField, tsGroup, defs)
	cte := "WITH filtered AS (SELECT data,timestamp_value FROM records WHERE " + where + ") "
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	defer tx.Rollback()
	countQuery := cte + "SELECT COUNT(*) FROM (SELECT " + groupExpression + " AS query_group FROM filtered GROUP BY query_group)"
	countArgs := append(append([]any(nil), whereArgs...), groupArgs...)
	var total int
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, errAPI(err)
	}
	pageGroups := "page_groups AS (SELECT " + groupExpression + " AS query_group FROM filtered GROUP BY query_group ORDER BY query_group LIMIT ? OFFSET ?) "
	selectSQL := []string{"page_groups.query_group"}
	selectSQL = append(selectSQL, selects...)
	query := cte + ", " + pageGroups + "SELECT " + strings.Join(selectSQL, ",") + " FROM page_groups JOIN filtered ON (" + groupExpression + ") IS page_groups.query_group GROUP BY page_groups.query_group ORDER BY page_groups.query_group"
	args := append(append([]any(nil), whereArgs...), groupArgs...)
	args = append(args, limit, offset)
	args = append(args, selectArgs...)
	args = append(args, groupArgs...)
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, errAPI(err)
	}
	defer rows.Close()
	groups := make([]map[string]any, 0, limit)
	for rows.Next() {
		values := make([]any, len(operations)+1)
		if err = rows.Scan(scanDestinations(values)...); err != nil {
			return nil, errAPI(err)
		}
		result := map[string]any{groupField: sqlGroupValue(values[0], groupField, tsGroup)}
		for i, operation := range operations {
			result[operation.key] = scanAggregateValue(values[i+1], operation, defs, numeric)
		}
		groups = append(groups, result)
	}
	if err = rows.Err(); err != nil {
		return nil, errAPI(err)
	}
	if err = rows.Close(); err != nil {
		return nil, errAPI(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return map[string]any{"groups": groups, "page": pageInfo(limit, offset, len(groups), total)}, nil
}

func scanDestinations(values []any) []any {
	destinations := make([]any, len(values))
	for i := range values {
		destinations[i] = &values[i]
	}
	return destinations
}

func sqlGroupExpression(field string, tsGroup bool, defs map[string]map[string]any) (string, []any) {
	if !tsGroup {
		return sqlFieldExpression(field, defs[field])
	}
	timestamp := "timestamp_value"
	switch field {
	case "year":
		return "CAST(strftime('%Y', " + timestamp + ") AS INTEGER)", nil
	case "month":
		return "strftime('%Y-%m', " + timestamp + ")", nil
	case "day":
		return "strftime('%Y-%m-%d', " + timestamp + ")", nil
	default:
		return "date(" + timestamp + ", '-' || ((CAST(strftime('%w', " + timestamp + ") AS INTEGER)+6)%7) || ' days')", nil
	}
}

func sqlGroupValue(value any, field string, tsGroup bool) any {
	if tsGroup && field == "year" {
		if n, ok := value.(int64); ok {
			return n
		}
	}
	return sqlValue(value, map[string]any{"type": "string"})
}

func (a *app) bucketRecordAggregates(group, table string, defs map[string]map[string]any, q url.Values, meta map[string]any, from, to *time.Time, numeric bool) (map[string]any, *apiError) {
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
	where, whereArgs, ae := buildRecordWhere(a.project, group, table, defs, q, true, meta, from, to)
	if ae != nil {
		return nil, ae
	}
	bucketExpr := sqlBucketExpression(unit)
	selects := make([]string, len(operations))
	selectArgs := make([]any, 0)
	for i, operation := range operations {
		expression, args := aggregateSQLExpression(operation, defs)
		selects[i] = expression
		selectArgs = append(selectArgs, args...)
	}
	cte := "WITH filtered AS (SELECT data,timestamp_value FROM records WHERE " + where + ") "
	tx, err := a.db.Begin()
	if err != nil {
		return nil, errAPI(err)
	}
	defer tx.Rollback()
	var total int
	countQuery := cte + "SELECT COUNT(*) FROM (SELECT " + bucketExpr + " AS query_bucket FROM filtered GROUP BY query_bucket)"
	if err := tx.QueryRow(countQuery, whereArgs...).Scan(&total); err != nil {
		return nil, errAPI(err)
	}
	pageBuckets := "page_buckets AS (SELECT " + bucketExpr + " AS query_bucket FROM filtered GROUP BY query_bucket ORDER BY query_bucket LIMIT ? OFFSET ?) "
	selected := []string{"page_buckets.query_bucket"}
	selected = append(selected, selects...)
	query := cte + ", " + pageBuckets + "SELECT " + strings.Join(selected, ",") + " FROM page_buckets JOIN filtered ON (" + bucketExpr + ") IS page_buckets.query_bucket GROUP BY page_buckets.query_bucket ORDER BY page_buckets.query_bucket"
	args := append(append([]any(nil), whereArgs...), limit, offset)
	args = append(args, selectArgs...)
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, errAPI(err)
	}
	defer rows.Close()
	buckets := make([]map[string]any, 0, limit)
	for rows.Next() {
		values := make([]any, len(operations)+1)
		if err = rows.Scan(scanDestinations(values)...); err != nil {
			return nil, errAPI(err)
		}
		result := map[string]any{"bucket_start": sqlValue(values[0], map[string]any{"type": "string"})}
		for i, operation := range operations {
			result[operation.key] = scanAggregateValue(values[i+1], operation, defs, numeric)
		}
		buckets = append(buckets, result)
	}
	if err = rows.Err(); err != nil {
		return nil, errAPI(err)
	}
	if err = rows.Close(); err != nil {
		return nil, errAPI(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, errAPI(err)
	}
	return map[string]any{"buckets": buckets, "page": pageInfo(limit, offset, len(buckets), total)}, nil
}

func sqlBucketExpression(unit string) string {
	timestamp := "timestamp_value"
	switch unit {
	case "hour":
		return "strftime('%Y-%m-%dT%H:00:00Z', " + timestamp + ")"
	case "day":
		return "strftime('%Y-%m-%dT00:00:00Z', " + timestamp + ")"
	case "week":
		return "strftime('%Y-%m-%dT00:00:00Z', date(" + timestamp + ", '-' || ((CAST(strftime('%w', " + timestamp + ") AS INTEGER)+6)%7) || ' days'))"
	default:
		return "strftime('%Y-%m-01T00:00:00Z', " + timestamp + ")"
	}
}

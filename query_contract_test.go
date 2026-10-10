package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func queryResult(t *testing.T, a *app, path string) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodGet, path, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	var res map[string]any
	testJSON(t, w, &res)
	return res
}

func queryRows(t *testing.T, a *app, path string) []any {
	t.Helper()
	return queryResult(t, a, path)["data"].([]any)
}

func queryTotal(t *testing.T, a *app, path string) float64 {
	t.Helper()
	return queryResult(t, a, path)["page"].(map[string]any)["total"].(float64)
}

func TestQueryDefaultsAndLimits(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "limits")
	createTestTable(t, a, "limits", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "tag", "type": "string"},
		map[string]any{"name": "n", "type": "integer"},
	}})
	createTestRecord(t, a, "limits", "items", map[string]any{"tag": "a", "n": 1})
	base := "/api/v1/default/data/groups/limits/tables/items/records"

	if got := queryResult(t, a, base)["page"].(map[string]any)["limit"]; got != float64(100) {
		t.Errorf("record default limit = %v, want 100", got)
	}
	if got := queryResult(t, a, base+"?distinct=tag")["page"].(map[string]any)["limit"]; got != float64(100) {
		t.Errorf("distinct default limit = %v, want 100", got)
	}
	if got := queryResult(t, a, base+"?group_by=tag")["page"].(map[string]any)["limit"]; got != float64(100) {
		t.Errorf("grouped default limit = %v, want 100", got)
	}
	if w := testRequest(t, a, http.MethodGet, base+"?limit=1000", nil, ""); w.Code != http.StatusOK {
		t.Errorf("limit=1000 status = %d, want 200", w.Code)
	}
	for _, query := range []string{"limit=1001", "limit=0", "limit=-1", "offset=-1"} {
		assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?"+query, nil, ""), http.StatusUnprocessableEntity)
	}
}

func TestBucketDefaultLimitAndAggregateRequirement(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "readings", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "type": "datetime", "required": true},
		map[string]any{"name": "temperature", "type": "decimal"},
	}})
	createTestRecord(t, a, "pool", "readings", map[string]any{"timestamp": "2026-09-27T12:00:00Z", "temperature": "20"})
	base := "/api/v1/default/data/groups/pool/tables/readings/records"

	if got := queryResult(t, a, base+"?bucket=day&aggregate=temperature:avg")["page"].(map[string]any)["limit"]; got != float64(1000) {
		t.Errorf("bucket default limit = %v, want 1000", got)
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?bucket=day", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?bucket=fortnight&aggregate=count", nil, ""), http.StatusUnprocessableEntity)
}

func TestDeterministicDefaultOrdering(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "ordered")
	createTestTable(t, a, "ordered", map[string]any{"name": "notes", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	base := "/api/v1/default/data/groups/ordered/tables/notes/records"
	first := createTestRecord(t, a, "ordered", "notes", map[string]any{"label": "first"})
	time.Sleep(2 * time.Millisecond)
	second := createTestRecord(t, a, "ordered", "notes", map[string]any{"label": "second"})
	time.Sleep(2 * time.Millisecond)
	third := createTestRecord(t, a, "ordered", "notes", map[string]any{"label": "third"})

	rows := queryRows(t, a, base)
	if len(rows) != 3 {
		t.Fatalf("default list returned %d rows, want 3", len(rows))
	}
	want := []string{third["id"].(string), second["id"].(string), first["id"].(string)}
	for i, row := range rows {
		if row.(map[string]any)["id"] != want[i] {
			t.Fatalf("default ordering = %#v, want newest first %v", rows, want)
		}
	}
}

func TestTemporalDefaultOrdering(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "sensors")
	createTestTable(t, a, "sensors", map[string]any{"name": "readings", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "type": "datetime", "required": true},
		map[string]any{"name": "value", "type": "decimal"},
	}})
	createTestRecord(t, a, "sensors", "readings", map[string]any{"timestamp": "2026-03-02T00:00:00Z", "value": "2"})
	createTestRecord(t, a, "sensors", "readings", map[string]any{"timestamp": "2026-03-03T00:00:00Z", "value": "3"})
	createTestRecord(t, a, "sensors", "readings", map[string]any{"timestamp": "2026-03-01T00:00:00Z", "value": "1"})

	rows := queryRows(t, a, "/api/v1/default/data/groups/sensors/tables/readings/records")
	if len(rows) != 3 {
		t.Fatalf("temporal list returned %d rows, want 3", len(rows))
	}
	for i, want := range []string{"2026-03-03T00:00:00Z", "2026-03-02T00:00:00Z", "2026-03-01T00:00:00Z"} {
		if got := rows[i].(map[string]any)["timestamp"]; got != want {
			t.Fatalf("temporal ordering row %d = %v, want %v", i, got, want)
		}
	}
}

func TestSortingRules(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "sorting")
	createTestTable(t, a, "sorting", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "tag", "type": "string"},
		map[string]any{"name": "n", "type": "integer"},
	}})
	createTestRecord(t, a, "sorting", "items", map[string]any{"tag": "b", "n": 2})
	createTestRecord(t, a, "sorting", "items", map[string]any{"tag": "a", "n": 1})
	base := "/api/v1/default/data/groups/sorting/tables/items/records"

	// order defaults to ascending when sort is supplied.
	rows := queryRows(t, a, base+"?sort=n")
	if rows[0].(map[string]any)["n"] != float64(1) {
		t.Fatalf("sort without order did not default to ascending: %#v", rows)
	}
	// desc works.
	rows = queryRows(t, a, base+"?sort=n&order=desc")
	if rows[0].(map[string]any)["n"] != float64(2) {
		t.Fatalf("order=desc did not reverse: %#v", rows)
	}
	// order without sort, empty sort, comma list and invalid order are rejected.
	for _, query := range []string{"order=desc", "sort=", "sort=tag,n", "sort=n&order=up"} {
		assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?"+query, nil, ""), http.StatusUnprocessableEntity)
	}
}

func TestFilterOperatorSemantics(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "filters")
	createTestTable(t, a, "filters", map[string]any{"name": "rows", "fields": []any{
		map[string]any{"name": "name", "type": "string"},
		map[string]any{"name": "amount", "type": "decimal"},
		map[string]any{"name": "enabled", "type": "boolean"},
		map[string]any{"name": "n", "type": "integer"},
	}})
	createTestRecord(t, a, "filters", "rows", map[string]any{"name": "100%", "amount": "5", "enabled": true, "n": 1})
	createTestRecord(t, a, "filters", "rows", map[string]any{"name": "100x", "amount": nil, "enabled": false, "n": 2})
	base := "/api/v1/default/data/groups/filters/tables/rows/records"

	// eq/gt/gte do not match null; ne/lt/lte match null.
	for query, want := range map[string]float64{
		"filter.amount.eq=5":  1,
		"filter.amount.gt=0":  1,
		"filter.amount.gte=5": 1,
		"filter.amount.ne=5":  1,
		"filter.amount.lt=4":  1,
		"filter.amount.lte=4": 1,
	} {
		if got := queryTotal(t, a, base+"?"+query); got != want {
			t.Errorf("%s total = %v, want %v", query, got, want)
		}
	}
	// contains is a case-insensitive literal substring, not a LIKE pattern.
	if got := queryTotal(t, a, base+"?filter.name.contains=%25"); got != 1 {
		t.Errorf("contains %% matched %v rows, want 1 (%% must be literal)", got)
	}
	if got := queryTotal(t, a, base+"?filter.name.contains=100"); got != 2 {
		t.Errorf("contains 100 matched %v rows, want 2", got)
	}
	// operator/type validation.
	for _, query := range []string{
		"filter.n.contains=1",
		"filter.enabled.gt=true",
		"filter.amount.isnull=maybe",
		"filter.amount.in=abc",
		"filter.amount.eq",
		"filter.missing=1",
	} {
		assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?"+query, nil, ""), http.StatusUnprocessableEntity)
	}
}

func TestBooleanFilterQueryValues(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "bools")
	createTestTable(t, a, "bools", map[string]any{"name": "flags", "fields": []any{
		map[string]any{"name": "flag", "type": "boolean"},
		map[string]any{"name": "label", "type": "string"},
	}})
	createTestRecord(t, a, "bools", "flags", map[string]any{"flag": true, "label": "t"})
	createTestRecord(t, a, "bools", "flags", map[string]any{"flag": false, "label": "f"})
	createTestRecord(t, a, "bools", "flags", map[string]any{"flag": nil, "label": "n"})
	base := "/api/v1/default/data/groups/bools/tables/flags/records"

	labelsFor := func(path string) map[string]bool {
		labels := map[string]bool{}
		for _, row := range queryRows(t, a, path) {
			labels[row.(map[string]any)["label"].(string)] = true
		}
		return labels
	}
	assertLabels := func(query string, want ...string) {
		t.Helper()
		labels := labelsFor(base + "?" + query)
		if len(labels) != len(want) {
			t.Fatalf("%s returned labels %v, want %v", query, labels, want)
		}
		for _, name := range want {
			if !labels[name] {
				t.Fatalf("%s returned labels %v, want %v", query, labels, want)
			}
		}
	}

	assertLabels("filter.flag=true", "t")
	assertLabels("filter.flag=True", "t")
	assertLabels("filter.flag=TRUE", "t")
	assertLabels("filter.flag=false", "f")
	assertLabels("filter.flag=False", "f")
	assertLabels("filter.flag=FALSE", "f")
	assertLabels("filter.flag.eq=true", "t")
	assertLabels("filter.flag.eq=false", "f")
	// ne matches null as well (SPEC section 25.2).
	assertLabels("filter.flag.ne=true", "f", "n")
	assertLabels("filter.flag.ne=false", "t", "n")
	// in matches any supplied value and never null.
	assertLabels("filter.flag.in=true", "t")
	assertLabels("filter.flag.in=false", "f")
	assertLabels("filter.flag.in=true&filter.flag.in=false", "t", "f")

	// Only true/false (case-insensitive) are valid boolean query values.
	for _, query := range []string{
		"filter.flag=yes",
		"filter.flag=1",
		"filter.flag=0",
		"filter.flag=maybe",
		"filter.flag.eq=yes",
		"filter.flag.ne=Y",
		"filter.flag.in=true&filter.flag.in=yes",
		"filter.flag.isnull=maybe",
	} {
		assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?"+query, nil, ""), http.StatusUnprocessableEntity)
	}

	// The grouped-aggregate and distinct query paths share buildRecordWhere,
	// so a boolean filter must work there too.
	groups := queryResult(t, a, base+"?filter.flag=true&group_by=flag&aggregate=count")["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["count"] != float64(1) {
		t.Fatalf("group_by with boolean filter = %#v", groups)
	}
	distinct := queryResult(t, a, base+"?filter.flag=false&distinct=label")["values"].([]any)
	if len(distinct) != 1 || distinct[0] != "f" {
		t.Fatalf("distinct with boolean filter = %#v", distinct)
	}
}

func TestMatchesBooleanQueryFilters(t *testing.T) {
	defs := map[string]map[string]any{"flag": {"type": "boolean", "name": "flag"}}
	cases := []struct {
		query string
		value any
		want  bool
	}{
		{"filter.flag=true", true, true},
		{"filter.flag=True", true, true},
		{"filter.flag=true", false, false},
		{"filter.flag=false", false, true},
		{"filter.flag=false", true, false},
		{"filter.flag.eq=false", false, true},
		{"filter.flag.ne=true", false, true},
		{"filter.flag.ne=true", nil, true},
		{"filter.flag.ne=true", true, false},
		{"filter.flag.ne=false", nil, true},
		{"filter.flag.in=true", true, true},
		{"filter.flag.in=false", true, false},
		{"filter.flag.in=true&filter.flag.in=false", false, true},
	}
	for _, tc := range cases {
		values, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatalf("parse query %q: %v", tc.query, err)
		}
		got, ae := matches(map[string]any{"flag": tc.value}, defs, values)
		if ae != nil {
			t.Fatalf("%s on %v: unexpected error %v", tc.query, tc.value, ae)
		}
		if got != tc.want {
			t.Errorf("%s on %v = %v, want %v", tc.query, tc.value, got, tc.want)
		}
	}
	for _, query := range []string{"filter.flag=yes", "filter.flag=1", "filter.flag.in=yes"} {
		values, _ := url.ParseQuery(query)
		if _, ae := matches(map[string]any{"flag": true}, defs, values); ae == nil {
			t.Errorf("%s: expected a validation error", query)
		}
	}
}

func TestGroupingContract(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "readings", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "type": "datetime", "required": true},
		map[string]any{"name": "temperature", "type": "decimal"},
	}})
	createTestRecord(t, a, "pool", "readings", map[string]any{"timestamp": "2026-01-05T12:00:00Z", "temperature": "20"})
	createTestRecord(t, a, "pool", "readings", map[string]any{"timestamp": "2026-01-06T12:00:00Z", "temperature": "22"})
	createTestRecord(t, a, "pool", "readings", map[string]any{"timestamp": "2026-02-01T00:30:00Z", "temperature": "26"})
	base := "/api/v1/default/data/groups/pool/tables/readings/records"

	// group_by without aggregate defaults to count.
	groups := queryResult(t, a, base+"?group_by=year")["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["year"] != float64(2026) || groups[0].(map[string]any)["count"] != float64(3) {
		t.Fatalf("group_by=year default count = %#v", groups)
	}
	// month/day/week keys and ordering.
	month := queryResult(t, a, base+"?group_by=month&aggregate=count")["groups"].([]any)
	if len(month) != 2 || month[0].(map[string]any)["month"] != "2026-01" || month[1].(map[string]any)["month"] != "2026-02" {
		t.Fatalf("group_by=month = %#v", month)
	}
	day := queryResult(t, a, base+"?group_by=day&aggregate=count")["groups"].([]any)
	if len(day) != 3 || day[0].(map[string]any)["day"] != "2026-01-05" {
		t.Fatalf("group_by=day = %#v", day)
	}
	// The January records share the week beginning Monday 2026-01-05; the
	// 2026-02-01 record (a Sunday) belongs to the week beginning 2026-01-26.
	week := queryResult(t, a, base+"?group_by=week&aggregate=count")["groups"].([]any)
	if len(week) != 2 || week[0].(map[string]any)["week"] != "2026-01-05" || week[1].(map[string]any)["week"] != "2026-01-26" {
		t.Fatalf("group_by=week = %#v", week)
	}
}

func TestVirtualGroupRejectedOnRecordTable(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "plain")
	createTestTable(t, a, "plain", map[string]any{"name": "records", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	base := "/api/v1/default/data/groups/plain/tables/records/records"
	assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?group_by=year&aggregate=count", nil, ""), http.StatusUnprocessableEntity)
}

func TestAggregationWithFilterAndTypes(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "costs")
	createTestTable(t, a, "costs", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "category", "type": "enum", "required": true, "values": []string{"service", "repair"}},
		map[string]any{"name": "amount", "type": "decimal"},
		map[string]any{"name": "count", "type": "integer"},
	}})
	createTestRecord(t, a, "costs", "items", map[string]any{"category": "service", "amount": "10", "count": 1})
	createTestRecord(t, a, "costs", "items", map[string]any{"category": "service", "amount": "15.5", "count": 2})
	createTestRecord(t, a, "costs", "items", map[string]any{"category": "repair", "amount": "5", "count": 3})
	base := "/api/v1/default/data/groups/costs/tables/items/records"

	subset := queryResult(t, a, base+"?aggregate=amount:sum,count:sum&filter.category=service")["aggregate"].(map[string]any)
	if subset["amount_sum"] != "25.5" {
		t.Errorf("filtered decimal sum = %v, want 25.5", subset["amount_sum"])
	}
	// Integer sum is a JSON number, not a string.
	if _, isString := subset["count_sum"].(string); isString {
		t.Errorf("integer sum returned a string: %#v", subset["count_sum"])
	}
	if subset["count_sum"] != float64(3) {
		t.Errorf("filtered integer sum = %v, want 3", subset["count_sum"])
	}
	// avg precision is at most ten decimal places.
	avg := queryResult(t, a, base+"?aggregate=amount:avg")["aggregate"].(map[string]any)["amount_avg"].(string)
	if strings.Contains(avg, ".") && len(strings.SplitN(avg, ".", 2)[1]) > 10 {
		t.Errorf("avg has more than ten decimal places: %q", avg)
	}
	// min/max over an ordered non-numeric field return canonical strings.
	extremes := queryResult(t, a, base+"?aggregate=category:min,category:max")["aggregate"].(map[string]any)
	if extremes["category_min"] != "repair" || extremes["category_max"] != "service" {
		t.Errorf("min/max over enum = %#v, want repair/service", extremes)
	}
}

func TestTimeRangePreconditions(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "readings", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "type": "datetime", "required": true},
		map[string]any{"name": "temperature", "type": "decimal"},
	}})
	createTestRecord(t, a, "pool", "readings", map[string]any{"timestamp": "2026-09-15T00:00:00Z", "temperature": "20"})
	base := "/api/v1/default/data/groups/pool/tables/readings/records"

	assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?from=2026-09-01", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?to=not-a-time", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?from=2026-09-30T00:00:00Z&to=2026-09-01T00:00:00Z", nil, ""), http.StatusUnprocessableEntity)
	// Equal bounds select nothing but are valid.
	if got := queryTotal(t, a, base+"?from=2026-09-15T00:00:00Z&to=2026-09-15T00:00:00Z"); got != 0 {
		t.Errorf("empty half-open range total = %v, want 0", got)
	}
	// Range parameters are rejected on a record table.
	createTestGroup(t, a, "plain")
	createTestTable(t, a, "plain", map[string]any{"name": "records", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/plain/tables/records/records?from=2026-09-01T00:00:00Z", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/plain/tables/records/records?bucket=day&aggregate=count", nil, ""), http.StatusUnprocessableEntity)
}

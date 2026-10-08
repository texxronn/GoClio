package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestDecimalFormatNumericOutput(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createRawTable(t, a, "billing", `{"name":"amounts","fields":[
		{"name":"label","type":"string"},
		{"name":"amount","type":"decimal"},
		{"name":"count","type":"integer"}
	]}`)
	postRecordRaw(t, a, "billing", "amounts", `{"label":"a","amount":"12.50","count":3}`)

	base := "/api/v1/default/data/groups/billing/tables/amounts/records"

	// Default behavior is unchanged: decimal values are JSON strings.
	def := testRequest(t, a, http.MethodGet, base, nil, "")
	if def.Code != http.StatusOK {
		t.Fatalf("default list status = %d: %s", def.Code, def.Body.String())
	}
	if !strings.Contains(def.Body.String(), `"amount":"12.5"`) {
		t.Fatalf("default response did not quote decimal: %s", def.Body.String())
	}

	// decimal_format=string is equivalent to the default.
	explicit := testRequest(t, a, http.MethodGet, base+"?decimal_format=string", nil, "")
	if !strings.Contains(explicit.Body.String(), `"amount":"12.5"`) {
		t.Fatalf("decimal_format=string quoted decimal missing: %s", explicit.Body.String())
	}

	// decimal_format=number emits a JSON number and leaves other fields intact.
	numeric := testRequest(t, a, http.MethodGet, base+"?decimal_format=number", nil, "")
	if numeric.Code != http.StatusOK {
		t.Fatalf("numeric list status = %d: %s", numeric.Code, numeric.Body.String())
	}
	numericBody := numeric.Body.String()
	if !strings.Contains(numericBody, `"amount":12.5`) {
		t.Fatalf("numeric response did not emit a JSON number: %s", numericBody)
	}
	if strings.Contains(numericBody, `"amount":"12.5"`) {
		t.Fatalf("numeric response still quoted the decimal: %s", numericBody)
	}
	if !strings.Contains(numericBody, `"label":"a"`) || !strings.Contains(numericBody, `"count":3`) {
		t.Fatalf("numeric response altered non-decimal fields: %s", numericBody)
	}

	// Single-record reads honor the parameter too.
	var listed map[string]any
	testJSON(t, def, &listed)
	rows, _ := listed["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one record, got %#v", listed["data"])
	}
	id := rows[0].(map[string]any)["id"].(string)
	item := testRequest(t, a, http.MethodGet, base+"/"+id+"?decimal_format=number", nil, "")
	if !strings.Contains(item.Body.String(), `"amount":12.5`) {
		t.Fatalf("numeric item response did not emit a JSON number: %s", item.Body.String())
	}

	// Distinct and aggregate decimal outputs honor the parameter.
	distinct := testRequest(t, a, http.MethodGet, base+"?distinct=amount&decimal_format=number", nil, "")
	if !strings.Contains(distinct.Body.String(), `"values":[12.5]`) {
		t.Fatalf("numeric distinct response = %s", distinct.Body.String())
	}
	aggregate := testRequest(t, a, http.MethodGet, base+"?aggregate=amount:sum&decimal_format=number", nil, "")
	if !strings.Contains(aggregate.Body.String(), `"amount_sum":12.5`) {
		t.Fatalf("numeric aggregate response = %s", aggregate.Body.String())
	}
	stringAggregate := testRequest(t, a, http.MethodGet, base+"?aggregate=amount:sum", nil, "")
	if !strings.Contains(stringAggregate.Body.String(), `"amount_sum":"12.5"`) {
		t.Fatalf("default aggregate response = %s", stringAggregate.Body.String())
	}

	// Any other value is rejected.
	bad := testRequest(t, a, http.MethodGet, base+"?decimal_format=float", nil, "")
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid decimal_format status = %d, want 422: %s", bad.Code, bad.Body.String())
	}
}

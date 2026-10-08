package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func (a *app) tableUI(w http.ResponseWriter, r *http.Request, s []string) {
	if r.Method != "GET" && r.Method != "POST" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	if len(s) == 0 {
		// /{project}/data is the client-side collection data browser (section
		// 35, re-scoped to the data partition by section 66.5). Per-table routes
		// below remain the server-rendered table UI and forms.
		a.dataBrowserUI(w, r)
		return
	}
	group, e := validIdentifier(s[0], "group")
	if e != nil {
		writeErr(w, e)
		return
	}
	if len(s) == 1 {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		g, e := a.group(group)
		if e != nil {
			writeErr(w, e)
			return
		}
		tables, e := a.listTables(group)
		if e != nil {
			writeErr(w, e)
			return
		}
		var b strings.Builder
		b.WriteString("<h1>" + esc(g["label"]) + " tables</h1><ul>")
		for _, t := range tables {
			b.WriteString("<li><a href=\"" + htmlAttr(a.dataURL(group, fmt.Sprint(t["name"]))) + "\">" + esc(t["label"]) + "</a></li>")
		}
		b.WriteString("</ul>")
		writeHTML(w, 200, a.pageShell(b.String(), "Tables · "+group))
		return
	}
	table, e := validIdentifier(s[1], "table")
	if e != nil {
		writeErr(w, e)
		return
	}
	base := a.dataURL(group, table)
	meta, e := a.table(group, table)
	if e != nil {
		writeErr(w, e)
		return
	}
	if len(s) == 2 {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		body, ae := a.tableHTML(group, table, meta, r.URL.Query())
		if ae != nil {
			writeErr(w, ae)
			return
		}
		writeHTML(w, 200, a.pageShell(body, base))
		return
	}
	if len(s) == 3 && s[2] == "new" {
		if r.Method == "GET" {
			writeHTML(w, 200, a.pageShell(a.formHTML(group, table, meta, nil), "New · "+table))
			return
		}
		defs, e := a.fields(group, table)
		if e != nil {
			writeErr(w, e)
			return
		}
		data, ae := formData(r, defs)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		record, ae := a.createRecord(group, table, data)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		http.Redirect(w, r, base+"/"+record["id"].(string), http.StatusSeeOther)
		return
	}
	if len(s) == 3 {
		if r.Method != "GET" {
			writeAPIError(w, methodNotAllowed())
			return
		}
		record, ae := a.getRecord(group, table, s[2])
		if ae != nil {
			writeErr(w, ae)
			return
		}
		writeHTML(w, 200, a.pageShell(a.recordHTML(group, table, meta, record), "Record · "+table))
		return
	}
	if len(s) == 4 && s[3] == "edit" {
		if r.Method == "GET" {
			record, ae := a.getRecord(group, table, s[2])
			if ae != nil {
				writeErr(w, ae)
				return
			}
			writeHTML(w, 200, a.pageShell(a.formHTML(group, table, meta, record), "Edit · "+table))
			return
		}
		defs, e := a.fields(group, table)
		if e != nil {
			writeErr(w, e)
			return
		}
		data, ae := formData(r, defs)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		if _, ae = a.patchRecord(group, table, s[2], data); ae != nil {
			writeErr(w, ae)
			return
		}
		http.Redirect(w, r, base+"/"+s[2], http.StatusSeeOther)
		return
	}
	if len(s) == 4 && s[3] == "delete" && r.Method == "POST" {
		if ae := a.deleteRecord(group, table, s[2]); ae != nil {
			writeErr(w, ae)
			return
		}
		http.Redirect(w, r, base, http.StatusSeeOther)
		return
	}
	writeAPIError(w, missing("Page"))
}

func (a *app) tableHTML(group, table string, meta map[string]any, q url.Values) (string, *apiError) {
	q = cleanTableQuery(q)
	if (q.Get("group_by") != "" || q.Get("bucket") != "") && q.Get("aggregate") == "" {
		q.Set("aggregate", "count")
	}
	result, e := a.queryRecords(group, table, q)
	if e != nil {
		return "", e
	}
	defs, _ := meta["fields"].([]map[string]any)
	base := a.dataURL(group, table)
	var b strings.Builder
	b.WriteString(`<style>
body{max-width:none;margin:0 auto;padding:0 1.4rem;background:#f4f7fb;color:#192a41}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:#fff;border-bottom:1px solid #e3e9f1;font-size:.9rem}body>nav a{color:#52647a}body>nav a:first-child{font-weight:750;color:#182f4b}main{max-width:none!important}.table-page{max-width:1180px;margin:1.2rem auto 4rem}.table-back{display:inline-block;margin:0 0 .75rem;color:#286a83;font-size:.84rem;font-weight:650;text-decoration:none}.table-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:1.2rem;margin-bottom:1rem;padding:1.3rem 1.4rem;border:1px solid #e3e9f1;border-radius:12px;background:#fff}.table-eyebrow{margin:0 0 .3rem;color:#8192a2;font-size:.68rem;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.table-heading h1{margin:0;color:#192a41;font-size:1.8rem;letter-spacing:-.04em}.table-heading p:last-child{margin:.4rem 0 0;color:#718394;font-size:.9rem}.table-actions{display:flex;align-items:center;gap:.55rem;flex:none}.table-actions a{padding:.58rem .78rem;border:1px solid #dce5eb;border-radius:7px;color:#315f75;font-size:.82rem;font-weight:650;text-decoration:none;white-space:nowrap}.table-actions a.button{border-color:#215f78;background:#215f78;color:#fff}.table-actions a:hover{filter:brightness(.97)}.table-query{margin:.75rem 0;padding:.85rem 1rem;border:1px solid #e3e9f1;border-radius:10px;background:#fff}.table-query summary{color:#315f75;font-size:.86rem;font-weight:650;cursor:pointer}.table-query fieldset{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,14rem),1fr));gap:.7rem;margin:1rem 0 0;padding:1rem 0 0;border:0;border-top:1px solid #edf0f4}.table-query legend{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0,0,0,0)}.table-query label{display:flex;flex-direction:column;gap:.35rem;margin:0;color:#52677b;font-size:.78rem;font-weight:600}.table-query input,.table-query select{box-sizing:border-box;width:100%;max-width:none;padding:.55rem .65rem;border:1px solid #d7e0e8;border-radius:6px;background:#fff;color:#192a41;font:inherit;font-weight:400}.table-query small{align-self:center;color:#718394;font-size:.78rem}.table-query button{padding:.55rem .8rem;border-radius:6px;background:#215f78;cursor:pointer}.table-query button+ a{align-self:center;color:#286a83;font-size:.82rem}.table-grid{overflow:auto;border:1px solid #e3e9f1;border-radius:10px;background:#fff}.table-grid table{min-width:100%;width:max-content;border-collapse:collapse}.table-grid th,.table-grid td{padding:.65rem .8rem;border:0;border-bottom:1px solid #e9eef2;text-align:left;font-size:.84rem}.table-grid th{background:#f7f9fb;color:#697d8e;font-size:.74rem;font-weight:650;white-space:nowrap}.table-grid th a{color:inherit;text-decoration:none}.table-grid tbody tr:hover{background:#f6fafb}.table-grid tbody tr:last-child td{border-bottom:0}.table-page>p{color:#718394;font-size:.83rem}.table-page>p a{margin-right:.5rem;color:#286a83}.table-page>p a.button{background:#215f78;color:#fff}.table-page>h1{margin:.6rem 0;font-size:1.4rem}.table-page>h1+p{max-width:60rem;color:#718394}.table-page>dl{padding:1rem;border:1px solid #e3e9f1;border-radius:9px;background:#fff}.table-page>dl dd{margin:.2rem 0 .8rem;color:#455b70}.table-page>form:not(.table-query form){display:inline}.table-page>form button{background:#a33c3c}.table-page>p:last-child{margin-top:.8rem}@media(max-width:760px){body{padding:0 .65rem}body>nav{margin:0 -.65rem .8rem;padding:.7rem .65rem;gap:.55rem;font-size:.82rem}.table-page{margin:.65rem auto 2rem}.table-heading{flex-direction:column;padding:1rem}.table-heading h1{font-size:1.5rem}.table-actions{flex-wrap:wrap}.table-query{padding:.75rem}.table-query fieldset{grid-template-columns:1fr}}
</style><section class="table-page"><a class="table-back" href="` + a.dataURL() + `">← Data browser</a><header class="table-heading"><div><p class="table-eyebrow">` + esc(group) + ` · TABLE</p><h1>` + esc(meta["label"]) + `</h1><p>` + esc(meta["description"]) + `</p></div><div class="table-actions"><a href="` + htmlAttr(a.dataBrowserURL(group, table)) + `">Data browser</a><a class="button" href="` + base + `/new">＋ New record</a></div></header><details class="table-query"><summary>Filter and analyze records</summary><form method="get"><fieldset><legend>Filters and query</legend>`)
	writeTableControls(&b, meta, defs, q)
	b.WriteString(`<button>Apply filters</button> <a href="` + base + `">Clear</a></fieldset></form></details><div class="table-grid"><table><thead><tr>`)
	records, isRecordList := result["data"].([]map[string]any)
	if isRecordList {
		sortBy, order := first(q, "sort"), first(q, "order")
		for _, f := range defs {
			if f["hidden"] == true {
				continue
			}
			name := f["name"].(string)
			next := "asc"
			if sortBy == name && strings.EqualFold(order, "asc") {
				next = "desc"
			}
			href := queryURL(base, q, map[string]string{"sort": name, "order": next, "offset": "0"})
			b.WriteString("<th><a href=\"" + esc(href) + "\">" + esc(f["label"]) + "</a></th>")
		}
		b.WriteString("<th></th></tr></thead><tbody>")
		for _, row := range records {
			b.WriteString("<tr>")
			linked := false
			for _, f := range defs {
				if f["hidden"] == true {
					continue
				}
				name := f["name"].(string)
				b.WriteString("<td>")
				if !linked {
					b.WriteString("<a href=\"" + base + "/" + esc(row["id"]) + "\">" + esc(row[name]) + "</a>")
					linked = true
				} else {
					b.WriteString(esc(row[name]))
				}
				b.WriteString("</td>")
			}
			b.WriteString("<td><a href=\"" + base + "/" + esc(row["id"]) + "/edit\">Edit</a></td></tr>")
		}
		b.WriteString("</tbody></table></div>")
	} else {
		rows, headers := aggregateTableRows(result, q)
		for _, header := range headers {
			b.WriteString("<th>" + esc(resultHeaderLabel(header, defs)) + "</th>")
		}
		b.WriteString("</tr></thead><tbody>")
		for _, row := range rows {
			b.WriteString("<tr>")
			for _, header := range headers {
				b.WriteString("<td>" + esc(row[header]) + "</td>")
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</tbody></table></div>")
	}
	page, hasPage := result["page"].(map[string]any)
	if hasPage {
		pageLimit := asInt(page["limit"])
		offset := asInt(page["offset"])
		total := asInt(page["total"])
		count := 0
		kind := "records"
		switch {
		case isRecordList:
			count = len(records)
		case result["groups"] != nil:
			count = len(result["groups"].([]map[string]any))
			kind = "groups"
		case result["buckets"] != nil:
			count = len(result["buckets"].([]map[string]any))
			kind = "buckets"
		}
		start := 0
		if total > 0 {
			start = offset + 1
		}
		b.WriteString(fmt.Sprintf("<p>%d–%d of %d %s</p>", start, offset+count, total, kind))
		if offset > 0 {
			b.WriteString("<a href=\"" + esc(queryURL(base, q, map[string]string{"offset": strconv.Itoa(max(0, offset-pageLimit))})) + "\">Previous</a> ")
		}
		if offset+count < total {
			b.WriteString("<a href=\"" + esc(queryURL(base, q, map[string]string{"offset": strconv.Itoa(offset + pageLimit)})) + "\">Next</a>")
		}
	}
	b.WriteString("</section>")
	return b.String(), nil
}

func cleanTableQuery(q url.Values) url.Values {
	clean := url.Values{}
	for key, values := range q {
		if strings.HasPrefix(key, "filter.") {
			allBlank := true
			for _, value := range values {
				if value != "" {
					allBlank = false
					break
				}
			}
			if allBlank {
				continue
			}
		} else if len(values) == 0 || values[0] == "" {
			continue
		}
		clean[key] = append([]string(nil), values...)
	}
	return clean
}

func writeAggregateOption(b *strings.Builder, value, label, selected string) {
	selectedAttr := ""
	if value == selected {
		selectedAttr = " selected"
	}
	b.WriteString("<option value=\"" + esc(value) + "\"" + selectedAttr + ">" + esc(label) + "</option>")
}

func writeTableControls(b *strings.Builder, meta map[string]any, defs []map[string]any, q url.Values) {
	for _, f := range defs {
		if f["hidden"] == true {
			continue
		}
		name := f["name"].(string)
		b.WriteString("<label>" + esc(f["label"]) + "<input name=\"filter." + name + "\" value=\"" + esc(first(q, "filter."+name)) + "\"></label>")
	}
	selectedGroup := first(q, "group_by")
	b.WriteString("<label>Group by<select name=group_by><option value=\"\">No grouping</option>")
	for _, f := range defs {
		if f["hidden"] == true {
			continue
		}
		name := f["name"].(string)
		selected := ""
		if selectedGroup == name {
			selected = " selected"
		}
		b.WriteString("<option value=\"" + esc(name) + "\"" + selected + ">" + esc(f["label"]) + "</option>")
	}
	if meta["kind"] == "timeseries" {
		for _, unit := range []string{"year", "month", "week", "day"} {
			selected := ""
			if selectedGroup == unit {
				selected = " selected"
			}
			b.WriteString("<option value=\"" + unit + "\"" + selected + ">" + strings.Title(unit) + "</option>")
		}
	}
	b.WriteString("</select></label><label>Aggregate<select name=aggregate><option value=\"\">Record list</option>")
	selectedAggregate := first(q, "aggregate")
	if strings.Contains(selectedAggregate, ",") {
		writeAggregateOption(b, selectedAggregate, "Custom: "+selectedAggregate, selectedAggregate)
	}
	writeAggregateOption(b, "count", "Count records", selectedAggregate)
	for _, f := range defs {
		if f["hidden"] == true {
			continue
		}
		name, label, typ := f["name"].(string), fmt.Sprint(f["label"]), fmt.Sprint(f["type"])
		functions := []string{"count"}
		if contains([]string{"integer", "decimal"}, typ) {
			functions = append([]string{"sum", "avg"}, functions...)
		}
		if contains([]string{"string", "text", "integer", "decimal", "date", "datetime", "enum", "url"}, typ) {
			functions = append(functions, "min", "max")
		}
		for _, function := range functions {
			writeAggregateOption(b, name+":"+function, label+" — "+strings.ToUpper(function), selectedAggregate)
		}
	}
	b.WriteString("</select></label>")
	if meta["kind"] == "timeseries" {
		b.WriteString("<label>Time bucket<select name=bucket><option value=\"\">No time buckets</option>")
		for _, unit := range []string{"hour", "day", "week", "month"} {
			selected := ""
			if first(q, "bucket") == unit {
				selected = " selected"
			}
			b.WriteString("<option value=\"" + unit + "\"" + selected + ">" + strings.Title(unit) + "</option>")
		}
		b.WriteString("</select></label><small>When a time bucket is selected, it takes precedence over Group by.</small>")
		b.WriteString("<label>From (RFC 3339, inclusive)<input name=from value=\"" + esc(first(q, "from")) + "\"></label><label>To (RFC 3339, exclusive)<input name=to value=\"" + esc(first(q, "to")) + "\"></label>")
	}
	if first(q, "sort") != "" {
		b.WriteString("<input type=hidden name=sort value=\"" + esc(first(q, "sort")) + "\"><input type=hidden name=order value=\"" + esc(first(q, "order")) + "\">")
	}
	limit := first(q, "limit")
	if limit == "" {
		limit = "100"
	}
	b.WriteString("<label>Rows per page<select name=limit>")
	limitOptions := []string{"25", "50", "100", "250", "1000"}
	if !contains(limitOptions, limit) {
		limitOptions = append([]string{limit}, limitOptions...)
	}
	for _, option := range limitOptions {
		selected := ""
		if limit == option {
			selected = " selected"
		}
		b.WriteString("<option value=\"" + option + "\"" + selected + ">" + option + "</option>")
	}
	b.WriteString("</select></label>")
}

func aggregateTableRows(result map[string]any, q url.Values) ([]map[string]any, []string) {
	rows := []map[string]any{}
	resultType := "aggregate"
	if grouped, ok := result["groups"].([]map[string]any); ok {
		rows, resultType = grouped, "groups"
	} else if buckets, ok := result["buckets"].([]map[string]any); ok {
		rows, resultType = buckets, "buckets"
	} else if aggregate, ok := result["aggregate"].(map[string]any); ok {
		rows = []map[string]any{aggregate}
	}
	headers := []string{}
	if resultType == "buckets" {
		headers = append(headers, "bucket_start")
	} else if group := first(q, "group_by"); group != "" {
		headers = append(headers, group)
	}
	for _, part := range strings.Split(first(q, "aggregate"), ",") {
		if part == "" {
			continue
		}
		if part == "count" {
			headers = append(headers, "count")
			continue
		}
		pieces := strings.SplitN(part, ":", 2)
		if len(pieces) == 2 {
			headers = append(headers, pieces[0]+"_"+pieces[1])
		}
	}
	if len(headers) == 0 && len(rows) > 0 {
		headers = sortedKeys(rows[0])
	}
	return rows, headers
}

func resultHeaderLabel(name string, defs []map[string]any) string {
	if name == "bucket_start" {
		return "Bucket start"
	}
	for _, f := range defs {
		field := f["name"].(string)
		if field == name {
			return fmt.Sprint(f["label"])
		}
		for _, fn := range []string{"count", "sum", "avg", "min", "max"} {
			if name == field+"_"+fn {
				return fmt.Sprint(f["label"]) + " " + strings.ToUpper(fn)
			}
		}
	}
	if name == "count" {
		return "Count"
	}
	return name
}

func (a *app) formHTML(group, table string, meta map[string]any, record map[string]any) string {
	defs, _ := meta["fields"].([]map[string]any)
	base := a.dataURL(group, table)
	edit := record != nil
	action := base + "/new"
	heading := "New "
	formMode := "New record"
	if edit {
		action = base + "/" + record["id"].(string) + "/edit"
		heading = "Edit "
		formMode = "Edit record"
	}
	var b strings.Builder
	b.WriteString(`<style>
body{max-width:none;margin:0 auto;padding:0 1.4rem;background:#f4f7fb;color:#192a41}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:#fff;border-bottom:1px solid #e3e9f1;font-size:.9rem}body>nav a{color:#52647a}body>nav a:first-child{font-weight:750;color:#182f4b}main{max-width:none!important}.record-form-page{max-width:850px;margin:1.2rem auto 4rem}.record-form-back{display:inline-block;margin:0 0 .8rem;color:#286a83;font-size:.84rem;font-weight:650;text-decoration:none}.record-form-back:hover{text-decoration:underline}.record-form-card{padding:clamp(1.2rem,4vw,2.2rem);border:1px solid #e3e9f1;border-radius:13px;background:#fff;box-shadow:0 5px 22px rgba(24,47,75,.04)}.record-form-eyebrow{margin:0 0 .35rem;color:#8192a2;font-size:.68rem;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.record-form-card h1{margin:0;color:#192a41;font-size:1.8rem;letter-spacing:-.04em}.record-form-intro{margin:.4rem 0 1.5rem;color:#718394;font-size:.9rem}.record-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1rem}.record-form label{display:flex;flex-direction:column;gap:.4rem;margin:0;color:#3e566c;font-size:.84rem;font-weight:650}.record-form label>span{color:#8494a2;font-size:.74rem;font-weight:500}.record-form input,.record-form textarea,.record-form select{box-sizing:border-box;width:100%;max-width:none;padding:.65rem .75rem;border:1px solid #d7e0e8;border-radius:7px;background:#fff;color:#192a41;font:inherit;font-weight:400;outline:none}.record-form input:focus,.record-form textarea:focus,.record-form select:focus{border-color:#4c9aa6;box-shadow:0 0 0 3px rgba(76,154,166,.14)}.record-form textarea{min-height:8rem;resize:vertical}.record-form input[type=checkbox]{width:1.05rem;height:1.05rem;accent-color:#24758a}.record-form-actions{grid-column:1/-1;display:flex;align-items:center;gap:.8rem;margin-top:.35rem;padding-top:1rem;border-top:1px solid #edf0f4}.record-form-actions button,.record-form-actions a{display:inline-flex;align-items:center;justify-content:center;min-height:2.45rem;padding:.55rem .9rem;border:0;border-radius:7px;font:inherit;font-size:.86rem;font-weight:650;text-decoration:none;cursor:pointer}.record-form-actions button{background:#215f78;color:#fff}.record-form-actions button:hover{background:#174d63}.record-form-actions a{color:#526b80}.record-form-actions a:hover{background:#f1f5f8}@media(max-width:700px){body{padding:0 .65rem}body>nav{margin:0 -.65rem .8rem;padding:.7rem .65rem;gap:.55rem;font-size:.82rem}.record-form-page{margin:.65rem auto 2rem}.record-form{grid-template-columns:1fr}.record-form-card{padding:1.1rem}}
</style><section class="record-form-page"><a class="record-form-back" href="` + base + `">← Back to ` + esc(meta["label"]) + `</a><div class="record-form-card"><p class="record-form-eyebrow">` + formMode + `</p><h1>` + heading + esc(meta["label"]) + `</h1><p class="record-form-intro">Enter the record details below. Fields marked required must be filled in.</p><form class="record-form" method="post" action="` + htmlAttr(action) + `">`)
	for _, f := range defs {
		if f["hidden"] == true || f["readonly"] == true {
			continue
		}
		name := f["name"].(string)
		typ := f["type"].(string)
		label := esc(f["label"])
		value := f["default"]
		if edit {
			value = record[name]
		}
		textValue := fmt.Sprint(value)
		if value == nil {
			textValue = ""
		}
		if typ == "datetime" && textValue != "" {
			textValue = strings.TrimSuffix(textValue, "Z")
		}
		b.WriteString("<label>" + label)
		if f["required"] == true {
			b.WriteString(" <span>(required)</span>")
		}
		required := ""
		if f["required"] == true {
			required = " required"
		}
		switch typ {
		case "text":
			b.WriteString("<textarea name=\"" + name + "\">" + esc(value) + "</textarea>")
		case "boolean":
			checked := ""
			if value == true {
				checked = " checked"
			}
			b.WriteString("<input type=checkbox name=\"" + name + "\" value=true" + checked + ">")
		case "enum":
			b.WriteString("<select name=\"" + name + "\"" + required + ">")
			if f["required"] != true {
				b.WriteString("<option value=\"\"></option>")
			}
			if values, ok := f["values"].([]any); ok {
				for _, v := range values {
					selected := ""
					if fmt.Sprint(v) == textValue {
						selected = " selected"
					}
					b.WriteString("<option value=\"" + esc(v) + "\"" + selected + ">" + esc(v) + "</option>")
				}
			}
			b.WriteString("</select>")
		case "reference":
			b.WriteString("<select name=\"" + name + "\"" + required + ">")
			if f["required"] != true {
				b.WriteString("<option value=\"\"></option>")
			}
			opts, _ := a.queryRecords(f["group"].(string), f["table"].(string), url.Values{"limit": []string{"1000"}})
			if records, ok := opts["data"].([]map[string]any); ok {
				for _, r := range records {
					id := fmt.Sprint(r["id"])
					sel := ""
					if id == textValue {
						sel = " selected"
					}
					b.WriteString("<option value=\"" + id + "\"" + sel + ">" + id + "</option>")
				}
			}
			b.WriteString("</select>")
		case "attachment":
			// A choose control listing the project's files; the stored value is
			// the content-entry ID (section 64.9).
			b.WriteString("<select name=\"" + name + "\"" + required + ">")
			if f["required"] != true {
				b.WriteString("<option value=\"\"></option>")
			}
			found := false
			for _, option := range a.contentEntryOptions() {
				sel := ""
				if option["id"] == textValue {
					sel = " selected"
					found = true
				}
				b.WriteString("<option value=\"" + option["id"] + "\"" + sel + ">" + esc(option["path"]) + "</option>")
			}
			if textValue != "" && !found {
				b.WriteString("<option value=\"" + esc(textValue) + "\" selected>" + esc(textValue) + "</option>")
			}
			b.WriteString("</select>")
		default:
			inputType := map[string]string{"integer": "number", "decimal": "number", "date": "date", "datetime": "datetime-local", "url": "url"}[typ]
			if inputType == "" {
				inputType = "text"
			}
			step := ""
			if typ == "integer" {
				step = " step=1"
			}
			if typ == "decimal" {
				step = " step=any"
			}
			b.WriteString("<input type=\"" + inputType + "\" name=\"" + name + "\" value=\"" + esc(textValue) + "\"" + required + step + ">")
		}
		if typ == "attachment" {
			if accept, ok := f["accept"].([]any); ok && len(accept) > 0 {
				labels := make([]string, len(accept))
				for i, v := range accept {
					labels[i] = fmt.Sprint(v)
				}
				b.WriteString("<span>Accepted: " + esc(strings.Join(labels, ", ")) + "</span>")
			}
		}
		if description := strings.TrimSpace(fmt.Sprint(f["description"])); description != "" && description != "<nil>" {
			b.WriteString("<span>" + esc(description) + "</span>")
		}
		b.WriteString("</label>")
	}
	b.WriteString(`<div class="record-form-actions"><button type="submit">Save record</button><a href="` + base + `">Cancel</a></div></form></div></section>`)
	return b.String()
}

func (a *app) recordHTML(group, table string, meta, record map[string]any) string {
	defs, _ := meta["fields"].([]map[string]any)
	base := a.dataURL(group, table)
	id := fmt.Sprint(record["id"])
	var b strings.Builder
	b.WriteString(`<style>
body{max-width:none;margin:0 auto;padding:0 1.4rem;background:#f4f7fb;color:#192a41}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:#fff;border-bottom:1px solid #e3e9f1;font-size:.9rem}body>nav a{color:#52647a}body>nav a:first-child{font-weight:750;color:#182f4b}main{max-width:none!important}.record-detail-page{max-width:920px;margin:1.2rem auto 4rem}.record-detail-back{display:inline-block;margin:0 0 .75rem;color:#286a83;font-size:.84rem;font-weight:650;text-decoration:none}.record-detail-card{overflow:hidden;border:1px solid #e3e9f1;border-radius:13px;background:#fff;box-shadow:0 5px 22px rgba(24,47,75,.04)}.record-detail-heading{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1.3rem 1.5rem;border-bottom:1px solid #e9eef2}.record-detail-eyebrow{margin:0 0 .3rem;color:#8192a2;font-size:.68rem;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.record-detail-heading h1{margin:0;color:#192a41;font-size:1.65rem;letter-spacing:-.04em}.record-detail-id{margin:.35rem 0 0;color:#8192a2;font: .78rem ui-monospace,SFMono-Regular,monospace;overflow-wrap:anywhere}.record-detail-actions{display:flex;align-items:center;gap:.5rem;flex:none}.record-detail-actions a,.record-detail-actions button{display:inline-flex;align-items:center;justify-content:center;min-height:2.4rem;padding:.5rem .8rem;border:1px solid #dce5eb;border-radius:7px;background:#fff;color:#315f75;font:inherit;font-size:.82rem;font-weight:650;text-decoration:none;cursor:pointer}.record-detail-actions a{border-color:#215f78;background:#215f78;color:#fff}.record-detail-actions a:hover{background:#174d63}.record-detail-actions button{color:#9a4541}.record-detail-actions button:hover{background:#fff5f4}.record-detail-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0;padding:.35rem 1.5rem}.record-detail-field{min-width:0;padding:.8rem .7rem;border-bottom:1px solid #edf0f4}.record-detail-field dt{margin:0 0 .35rem;color:#7c8d9c;font-size:.75rem;font-weight:600}.record-detail-field dd{margin:0;color:#253e55;font-size:.92rem;overflow-wrap:anywhere;white-space:pre-wrap}.record-detail-empty{padding:1.5rem;color:#718394;font-size:.88rem}@media(max-width:700px){body{padding:0 .65rem}body>nav{margin:0 -.65rem .8rem;padding:.7rem .65rem;gap:.55rem;font-size:.82rem}.record-detail-page{margin:.65rem auto 2rem}.record-detail-heading{align-items:flex-start;flex-direction:column;padding:1rem}.record-detail-heading h1{font-size:1.4rem}.record-detail-fields{grid-template-columns:1fr;padding:0 .75rem}.record-detail-actions{flex-wrap:wrap}}
</style><section class="record-detail-page"><a class="record-detail-back" href="` + base + `">← Back to table</a><div class="record-detail-card"><header class="record-detail-heading"><div><p class="record-detail-eyebrow">` + esc(group) + ` · RECORD</p><h1>` + esc(meta["label"]) + `</h1><p class="record-detail-id">` + esc(id) + `</p></div><div class="record-detail-actions"><a href="` + base + `/` + htmlAttr(id) + `/edit">Edit record</a><form method="post" action="` + base + `/` + htmlAttr(id) + `/delete" onsubmit="return confirm('Delete this record?')"><button type="submit">Delete</button></form></div></header><dl class="record-detail-fields">`)
	for _, f := range defs {
		if f["hidden"] == true {
			continue
		}
		name := f["name"].(string)
		b.WriteString(`<div class="record-detail-field"><dt>` + esc(f["label"]) + `</dt><dd>`)
		if f["type"] == "attachment" {
			if id, ok := record[name].(string); ok && id != "" {
				b.WriteString(`<a href="` + htmlAttr(a.projectPath("files", "id", id)) + `">` + esc(id) + `</a>`)
			} else {
				b.WriteString(esc(record[name]))
			}
		} else {
			b.WriteString(esc(record[name]))
		}
		b.WriteString(`</dd></div>`)
	}
	b.WriteString(`</dl></div></section>`)
	return b.String()
}

func (a *app) pageShell(body, title string) string {
	home := a.projectPath() + "/"
	dataPath := a.projectPath("data")
	filesPath := a.projectPath("files")
	searchPath := a.projectPath("search")
	helpPath := a.projectPath("help")
	healthPath := a.projectPath("health")
	return "<!doctype html><html lang=en><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1'><link rel=icon type='image/svg+xml' href='/favicon.svg'><title>" + esc(title) + " · Clio</title><style>body{font:16px system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem;color:#222}a{color:#165d9c}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:.5rem;text-align:left}label{display:block;margin:1rem 0}input,textarea,select{display:block;max-width:100%;width:28rem;padding:.5rem}textarea{height:8rem}button,.button{padding:.5rem .8rem;background:#165d9c;color:white;border:0;border-radius:3px}dt{font-weight:bold;margin-top:.7rem}pre{overflow:auto;background:#f5f5f5;padding:1rem}nav{border-bottom:1px solid #ddd;padding-bottom:1rem;margin-bottom:2rem}</style><nav><a href='" + home + "'>Clio</a> · <a href='" + dataPath + "'>Data browser</a> · <a href='" + filesPath + "'>Files</a> · <a href='" + searchPath + "'>Search</a> · <a href='" + helpPath + "'>Help</a> · <a href='" + healthPath + "'>Health</a></nav><main>" + body + "</main></html>"
}

// dataBrowserURL builds the client data-browser location for a group and table:
// /{project}/data?group=...&table=...
func (a *app) dataBrowserURL(group, table string) string {
	q := url.Values{}
	if group != "" {
		q.Set("group", group)
	}
	if table != "" {
		q.Set("table", table)
	}
	u := a.dataURL()
	if encoded := q.Encode(); encoded != "" {
		u += "?" + encoded
	}
	return u
}

// projectOverview renders /{project}/: the project home that links the data
// browser, the file explorer and search (sections 64.14 and 66.5). It replaces
// the former redirect to /{project}/data.
func (a *app) projectOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAPIError(w, methodNotAllowed())
		return
	}
	d, ae := a.directory("/")
	if ae != nil {
		writeErr(w, ae)
		return
	}
	children, _ := d["children"].([]map[string]any)
	writeHTML(w, http.StatusOK, a.homeHTML(children, a.projectsPanelHTML()))
}

// projectHref is the human URL of a project overview: /{name}/. It is absolute
// (not based on the current project) and keeps every generated URL
// project-scoped, so the projects UI never links to an instance route.
func projectHref(name string) string { return "/" + urlPath(name) + "/" }

// projectsPanelHTML renders the project manager mounted on the project overview
// /{project}/ (section 65.5 and section 66.5). The list is server-rendered
// inside a <noscript> fallback from listProjects so the overview stays useful
// without JavaScript, and Clio.Projects.mount drives create and delete through
// the existing public projects API. No human projects route is introduced
// (section 66.3): every link points at /{name}/.
func (a *app) projectsPanelHTML() string {
	projects, _ := a.listProjects()
	var b strings.Builder
	b.WriteString(`<style>.projects-panel{max-width:1120px;margin:2rem auto 4rem;padding:clamp(1.2rem,3vw,2rem);border:1px solid #e3e9f1;border-radius:14px;background:#fff;color:#192a41}.projects-panel h2{margin:0 0 .35rem;font-size:1.3rem;letter-spacing:-.03em}.projects-panel-intro{margin:0 0 1rem;color:#718394;font-size:.9rem}.projects-list{list-style:none;margin:0 0 1.25rem;padding:0;display:grid;gap:.4rem}.projects-item{display:flex;align-items:center;gap:.55rem;padding:.5rem .65rem;border:1px solid #edf0f4;border-radius:8px}.projects-item a{font-weight:650;color:#216b82;text-decoration:none}.projects-item a:hover{text-decoration:underline}.projects-slug{color:#8192a2;font: .78rem ui-monospace,SFMono-Regular,monospace}.projects-current{color:#26704d;font-size:.78rem;font-weight:650}.projects-danger{margin-left:auto;padding:.35rem .6rem;border:1px solid #f0caca;border-radius:6px;background:#fff;color:#9a3732;font:inherit;font-size:.8rem;cursor:pointer}.projects-danger:hover{background:#fff5f4}.projects-status{margin:.5rem 0;color:#718394;font-size:.85rem}.projects-status.projects-error{color:#9a4541;font-weight:600}.projects-create{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,13rem),1fr));gap:.6rem;align-items:end;padding-top:1rem;border-top:1px solid #edf0f4}.projects-create label{display:flex;flex-direction:column;gap:.3rem;margin:0;color:#52677b;font-size:.78rem;font-weight:600}.projects-create input{box-sizing:border-box;width:100%;padding:.5rem .6rem;border:1px solid #dce5eb;border-radius:6px}.projects-create button{padding:.55rem .8rem;border:0;border-radius:6px;background:#215f78;color:#fff;font:inherit;font-weight:650;cursor:pointer}.projects-create button:hover{background:#174d63}</style><section class="projects-panel"><h2>Projects</h2><p class="projects-panel-intro">Create, switch to, or delete a project. Deleting removes only an empty project.</p><div id="clio-projects" aria-live="polite"></div><noscript><ul class="projects-noscript">`)
	for _, project := range projects {
		name := fmt.Sprint(project["name"])
		label := fmt.Sprint(project["label"])
		if label == "" {
			label = name
		}
		b.WriteString(`<li><a href="` + htmlAttr(projectHref(name)) + `">` + esc(label) + `</a> <span class="projects-slug">` + esc(name) + `</span></li>`)
	}
	b.WriteString(`</ul></noscript></section><script src="/assets/clio.js"></script><script>Clio.Projects.mount(document.getElementById("clio-projects"));</script>`)
	return b.String()
}

// projectName is the active project for URL building. The zero value (an
// instance-scoped app such as health or help) addresses the default project.
func (a *app) projectName() string {
	if a.project == "" {
		return defaultProject
	}
	return a.project
}

// projectPath builds a relative human URL inside the current project:
// /{project}[/segments...].
func (a *app) projectPath(segments ...string) string {
	parts := []string{"/" + a.projectName()}
	for _, segment := range segments {
		if segment != "" {
			parts = append(parts, urlPath(segment))
		}
	}
	return strings.Join(parts, "/")
}

func formData(r *http.Request, defs []map[string]any) (map[string]any, *apiError) {
	if e := r.ParseForm(); e != nil {
		return nil, invalid("Malformed form data")
	}
	out := map[string]any{}
	for _, f := range defs {
		name := f["name"].(string)
		typ := f["type"].(string)
		if f["readonly"] == true || f["hidden"] == true {
			continue
		}
		values, ok := r.Form[name]
		if typ == "boolean" {
			out[name] = ok
			continue
		}
		if !ok {
			continue
		}
		value := values[len(values)-1]
		if value == "" && contains([]string{"integer", "decimal", "date", "datetime", "url", "reference", "attachment", "enum"}, typ) {
			out[name] = nil
		} else if typ == "integer" {
			out[name] = json.Number(value)
		} else if typ == "datetime" && !strings.HasSuffix(value, "Z") && !regexpOffset.MatchString(value) {
			out[name] = value + "Z"
		} else {
			out[name] = value
		}
	}
	return out, nil
}

var regexpOffset = mustCompile(`.*[+-][0-9]{2}:[0-9]{2}$`)

func mustCompile(v string) *regexp.Regexp { return regexp.MustCompile(v) }

// contentEntryOptions lists the project's content entries for an attachment
// form control, ordered by path (section 64.9).
func (a *app) contentEntryOptions() []map[string]string {
	rows, err := a.db.Query(`SELECT id,path FROM content_entries WHERE project=? ORDER BY path LIMIT 1000`, a.project)
	if err != nil {
		return nil
	}
	defer rows.Close()
	options := []map[string]string{}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return options
		}
		options = append(options, map[string]string{"id": id, "path": path})
	}
	return options
}

func queryURL(p string, q url.Values, overrides map[string]string) string {
	out := url.Values{}
	for k, values := range q {
		if _, ok := overrides[k]; !ok {
			for _, v := range values {
				out.Add(k, v)
			}
		}
	}
	for k, v := range overrides {
		out.Set(k, v)
	}
	encoded := out.Encode()
	if encoded == "" {
		return p
	}
	return p + "?" + encoded
}
func asInt(v any) int { n, _ := strconv.Atoi(fmt.Sprint(v)); return n }

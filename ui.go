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

func (a *app) tableUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	s := splitPath(strings.TrimPrefix(r.URL.Path, "/t/"))
	if len(s) == 0 {
		writeAPIError(w, missing("Table"))
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
			b.WriteString("<li><a href=\"/t/" + group + "/" + t["name"].(string) + "\">" + esc(t["label"]) + "</a></li>")
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
		writeHTML(w, 200, a.pageShell(body, "/t/"+group+"/"+table))
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
		http.Redirect(w, r, "/t/"+group+"/"+table+"/"+record["id"].(string), http.StatusSeeOther)
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
		http.Redirect(w, r, "/t/"+group+"/"+table+"/"+s[2], http.StatusSeeOther)
		return
	}
	if len(s) == 4 && s[3] == "delete" && r.Method == "POST" {
		if ae := a.deleteRecord(group, table, s[2]); ae != nil {
			writeErr(w, ae)
			return
		}
		http.Redirect(w, r, "/t/"+group+"/"+table, http.StatusSeeOther)
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
	var b strings.Builder
	b.WriteString("<h1>" + esc(meta["label"]) + "</h1><p>" + esc(meta["description"]) + "</p><p><a class=button href=\"/t/" + group + "/" + table + "/new\">New</a></p><form method=get><fieldset><legend>Filter and query</legend>")
	writeTableControls(&b, meta, defs, q)
	b.WriteString("<button>Apply filters</button> <a href=\"/t/" + group + "/" + table + "\">Clear</a></fieldset></form><table><thead><tr>")
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
			href := queryURL("/t/"+group+"/"+table, q, map[string]string{"sort": name, "order": next, "offset": "0"})
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
					b.WriteString("<a href=\"/t/" + group + "/" + table + "/" + esc(row["id"]) + "\">" + esc(row[name]) + "</a>")
					linked = true
				} else {
					b.WriteString(esc(row[name]))
				}
				b.WriteString("</td>")
			}
			b.WriteString("<td><a href=\"/t/" + group + "/" + table + "/" + esc(row["id"]) + "/edit\">Edit</a></td></tr>")
		}
		b.WriteString("</tbody></table>")
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
		b.WriteString("</tbody></table>")
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
			b.WriteString("<a href=\"" + esc(queryURL("/t/"+group+"/"+table, q, map[string]string{"offset": strconv.Itoa(max(0, offset-pageLimit))})) + "\">Previous</a> ")
		}
		if offset+count < total {
			b.WriteString("<a href=\"" + esc(queryURL("/t/"+group+"/"+table, q, map[string]string{"offset": strconv.Itoa(offset + pageLimit)})) + "\">Next</a>")
		}
	}
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
	edit := record != nil
	action := "/t/" + group + "/" + table + "/new"
	heading := "New "
	if edit {
		action = "/t/" + group + "/" + table + "/" + record["id"].(string) + "/edit"
		heading = "Edit "
	}
	var b strings.Builder
	b.WriteString("<h1>" + heading + esc(meta["label"]) + "</h1><form method=post action=\"" + action + "\">")
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
		b.WriteString("</label>")
	}
	b.WriteString("<button type=submit>Save</button> <a href=\"/t/" + group + "/" + table + "\">Cancel</a></form>")
	return b.String()
}

func (a *app) recordHTML(group, table string, meta, record map[string]any) string {
	defs, _ := meta["fields"].([]map[string]any)
	id := fmt.Sprint(record["id"])
	var b strings.Builder
	b.WriteString("<h1>" + esc(meta["label"]) + "</h1><p><a href=\"/t/" + group + "/" + table + "/" + id + "/edit\">Edit</a></p><form method=post action=\"/t/" + group + "/" + table + "/" + id + "/delete\" onsubmit=\"return confirm('Delete this record?')\"><button type=submit>Delete</button></form><dl>")
	for _, f := range defs {
		if f["hidden"] == true {
			continue
		}
		name := f["name"].(string)
		b.WriteString("<dt>" + esc(f["label"]) + "</dt><dd>" + esc(record[name]) + "</dd>")
	}
	b.WriteString("</dl>")
	return b.String()
}

func (a *app) pageShell(body, title string) string {
	return "<!doctype html><html lang=en><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1'><title>" + esc(title) + " · Clio</title><style>body{font:16px system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem;color:#222}a{color:#165d9c}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:.5rem;text-align:left}label{display:block;margin:1rem 0}input,textarea,select{display:block;max-width:100%;width:28rem;padding:.5rem}textarea{height:8rem}button,.button{padding:.5rem .8rem;background:#165d9c;color:white;border:0;border-radius:3px}dt{font-weight:bold;margin-top:.7rem}pre{overflow:auto;background:#f5f5f5;padding:1rem}nav{border-bottom:1px solid #ddd;padding-bottom:1rem;margin-bottom:2rem}</style><nav><a href='/'>Clio</a> · <a href='/collections'>Data Browser</a> · <a href='/help'>Help</a> · <a href='/health'>Health</a></nav><main>" + body + "</main></html>"
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
		if value == "" && contains([]string{"integer", "decimal", "date", "datetime", "url", "reference", "enum"}, typ) {
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

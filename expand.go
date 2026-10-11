package main

import (
	"net/url"
	"sort"
	"strings"
)

// Expansion limits. A reference may be expanded to a bounded depth and a single
// response may embed at most a bounded number of records.
const (
	maxExpandDepth   = 3
	maxExpandRecords = 1000
)

// expandNode is one resolved step of an expansion plan: a reference field and
// the table it targets, plus the target table's field definitions so that
// deeper paths and hidden-field filtering resolve without extra lookups.
type expandNode struct {
	field       string
	group       string
	table       string
	by          map[string]map[string]any
	children    []*expandNode
	childByName map[string]*expandNode
}

// expandSpec is the resolved form of the expand query parameter.
type expandSpec struct {
	roots []*expandNode
}

// resolveExpand parses and validates the expand query parameter against the
// fields of the table being queried. It returns nil when the parameter is
// absent. Paths are comma-separated and dotted: a bare field is depth 1 and a
// path may name at most maxExpandDepth reference fields.
func (a *app) resolveExpand(group, table string, defs []map[string]any, q url.Values) (*expandSpec, *apiError) {
	if !q.Has("expand") {
		return nil, nil
	}
	raw := strings.TrimSpace(first(q, "expand"))
	if raw == "" {
		return nil, invalid("expand must name one or more reference fields")
	}
	root := &expandNode{childByName: map[string]*expandNode{}}
	rootBy := indexFields(defs)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, invalid("expand contains an empty path")
		}
		segments := strings.Split(part, ".")
		if len(segments) > maxExpandDepth {
			return nil, invalid("expand paths may be at most 3 levels deep")
		}
		cur, curBy := root, rootBy
		for _, segment := range segments {
			if segment == "" {
				return nil, invalid("expand contains an empty path")
			}
			f := curBy[segment]
			if f == nil {
				return nil, invalid("Unknown field in expand: " + segment)
			}
			if f["type"] != "reference" {
				return nil, invalid("expand field is not a reference: " + segment)
			}
			child := cur.childByName[segment]
			if child == nil {
				tgroup, ttable := str(f, "group"), str(f, "table")
				tdefs, e := a.fields(tgroup, ttable)
				if e != nil {
					return nil, invalid("expand target table does not exist for field " + segment)
				}
				child = &expandNode{
					field:       segment,
					group:       tgroup,
					table:       ttable,
					by:          indexFields(tdefs),
					childByName: map[string]*expandNode{},
				}
				cur.childByName[segment] = child
				cur.children = append(cur.children, child)
			}
			cur, curBy = child, child.by
		}
	}
	if len(root.children) == 0 {
		return nil, invalid("expand must name one or more reference fields")
	}
	return &expandSpec{roots: root.children}, nil
}

// expandBudget counts the distinct records embedded across every path so a
// single response cannot grow without bound.
type expandBudget struct {
	seen map[string]bool
}

func (b *expandBudget) add(id string) *apiError {
	if b.seen[id] {
		return nil
	}
	b.seen[id] = true
	if len(b.seen) > maxExpandRecords {
		return invalid("expand exceeds the maximum of 1000 embedded records")
	}
	return nil
}

// applyExpand embeds referenced records into each record in place under an
// "expanded" object, with one entry per reference field resolved successfully.
func (a *app) applyExpand(spec *expandSpec, records []map[string]any, numeric bool) *apiError {
	if spec == nil || len(records) == 0 {
		return nil
	}
	return a.expandLevel(spec.roots, records, numeric, &expandBudget{seen: map[string]bool{}})
}

// expandLevel resolves a breadth-first frontier of expansion nodes. At each
// depth it loads the union of referenced IDs for every distinct target table in
// a single query, then clones targets into each parent so deeper paths attach to
// their own copy. Batching is per (depth, target table), never per row.
func (a *app) expandLevel(nodes []*expandNode, parents []map[string]any, numeric bool, budget *expandBudget) *apiError {
	if len(nodes) == 0 || len(parents) == 0 {
		return nil
	}
	type activeNode struct {
		node    *expandNode
		parents []map[string]any
	}
	frontier := make([]activeNode, 0, len(nodes))
	for _, node := range nodes {
		frontier = append(frontier, activeNode{node: node, parents: parents})
	}
	key := func(g, t string) string { return g + "\x00" + t }
	for len(frontier) > 0 {
		type tableLoad struct {
			node *expandNode
			ids  map[string]bool
		}
		loads := []*tableLoad{}
		loadByKey := map[string]*tableLoad{}
		for _, active := range frontier {
			k := key(active.node.group, active.node.table)
			load := loadByKey[k]
			if load == nil {
				load = &tableLoad{node: active.node, ids: map[string]bool{}}
				loadByKey[k] = load
				loads = append(loads, load)
			}
			for _, parent := range active.parents {
				if id, ok := parent[active.node.field].(string); ok && id != "" {
					load.ids[id] = true
				}
			}
		}
		loaded := map[string]map[string]map[string]any{}
		for _, load := range loads {
			targets, ae := a.loadExpandTargets(load.node, load.ids, numeric)
			if ae != nil {
				return ae
			}
			loaded[key(load.node.group, load.node.table)] = targets
		}
		next := []activeNode{}
		for _, active := range frontier {
			targets := loaded[key(active.node.group, active.node.table)]
			embedded := []map[string]any{}
			for _, parent := range active.parents {
				id, ok := parent[active.node.field].(string)
				if !ok || id == "" {
					continue
				}
				src := targets[id]
				if src == nil {
					continue
				}
				if ae := budget.add(id); ae != nil {
					return ae
				}
				child := mapClone(src)
				expanded, _ := parent["expanded"].(map[string]any)
				if expanded == nil {
					expanded = map[string]any{}
					parent["expanded"] = expanded
				}
				expanded[active.node.field] = child
				embedded = append(embedded, child)
			}
			if len(embedded) == 0 {
				continue
			}
			for _, nested := range active.node.children {
				next = append(next, activeNode{node: nested, parents: embedded})
			}
		}
		frontier = next
	}
	return nil
}

// loadExpandTargets fetches every requested ID of one target table in a single
// query and returns its record representations, with hidden fields removed.
func (a *app) loadExpandTargets(node *expandNode, ids map[string]bool, numeric bool) (map[string]map[string]any, *apiError) {
	out := map[string]map[string]any{}
	if len(ids) == 0 {
		return out, nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	sort.Strings(list)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(list)), ",")
	args := make([]any, 0, len(list)+3)
	args = append(args, a.project, node.group, node.table)
	for _, id := range list {
		args = append(args, id)
	}
	rows, err := a.db.Query(`SELECT id,created_at,updated_at,data FROM records WHERE project=? AND group_name=? AND table_name=? AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, errAPI(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, created, updated, raw string
		if err = rows.Scan(&id, &created, &updated, &raw); err != nil {
			return nil, errAPI(err)
		}
		record := map[string]any{}
		if err = decodeJSON([]byte(raw), &record); err != nil {
			return nil, errAPI(err)
		}
		record["id"], record["created_at"], record["updated_at"] = id, created, updated
		applyDecimalFormat(record, node.by, numeric)
		for name, def := range node.by {
			if def["hidden"] == true {
				delete(record, name)
			}
		}
		out[id] = record
	}
	if err = rows.Err(); err != nil {
		return nil, errAPI(err)
	}
	return out, nil
}

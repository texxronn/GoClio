package main

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// defaultProject is the implicit project that owns data created before
// namespaces and that unscoped routes address (section 65.1).
const defaultProject = "default"

// reservedProjects are the names that cannot be used for a project
// (sections 65.2 and 66.4). default is reserved and always exists.
var reservedProjects = map[string]bool{
	"api":         true,
	"health":      true,
	"help":        true,
	"projects":    true,
	"assets":      true,
	"favicon.svg": true,
	"default":     true,
}

func validProjectName(value string) (string, *apiError) {
	name, e := validIdentifier(value, "project")
	if e != nil {
		return "", e
	}
	if reservedProjects[name] {
		return "", invalid("Project name is reserved: " + name)
	}
	return name, nil
}

func (a *app) projectsAPI(w http.ResponseWriter, r *http.Request, s []string) {
	if len(s) == 1 {
		switch r.Method {
		case http.MethodGet:
			items, e := a.listProjects()
			if e != nil {
				writeErr(w, e)
				return
			}
			writeJSON(w, 200, items)
		case http.MethodPost:
			input, e := readJSON(r, bodyLimit)
			if e != nil {
				writeErr(w, e)
				return
			}
			item, e := a.createProject(input)
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
	if len(s) == 2 {
		name := s[1]
		switch r.Method {
		case http.MethodGet:
			item, e := a.projectInfo(name)
			if e != nil {
				writeErr(w, e)
				return
			}
			writeJSON(w, 200, item)
		case http.MethodDelete:
			if e := a.deleteProject(name); e != nil {
				writeErr(w, e)
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

func (a *app) projectInfo(name string) (map[string]any, *apiError) {
	var label, desc, created string
	var order int
	e := a.db.QueryRow(`SELECT label,description,sort_order,created_at FROM projects WHERE name=?`, name).Scan(&label, &desc, &order, &created)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, missing("Project")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	return map[string]any{
		"name":        name,
		"label":       label,
		"description": desc,
		"order":       order,
		"created_at":  created,
		"url":         a.baseURL + "/" + name + "/",
		"api_url":     a.baseURL + "/api/v1/projects/" + name,
	}, nil
}

func (a *app) listProjects() ([]map[string]any, *apiError) {
	rows, e := a.db.Query(`SELECT name FROM projects ORDER BY sort_order,label,name`)
	if e != nil {
		return nil, errAPI(e)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			return nil, errAPI(e)
		}
		item, x := a.projectInfo(name)
		if x != nil {
			return nil, x
		}
		out = append(out, item)
	}
	return out, nil
}

func (a *app) createProject(input map[string]any) (map[string]any, *apiError) {
	name, e := validProjectName(str(input, "name"))
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
	order := 0
	if v, ok := input["order"]; ok {
		n, x := integerExact(v)
		if x != nil || n < -2147483648 || n > 2147483647 {
			return nil, invalid("order must be a 32-bit integer")
		}
		order = n
	}
	_, err := a.db.Exec(`INSERT INTO projects(name,label,description,sort_order,created_at) VALUES(?,?,?,?,?)`, name, label, desc, order, formatUTC(time.Now()))
	if err != nil {
		if isConstraint(err) {
			return nil, conflict("Project already exists")
		}
		return nil, errAPI(err)
	}
	return a.projectInfo(name)
}

// deleteProject removes a project only when it is empty: it has no groups,
// tables or records (section 65.5). The default project cannot be deleted.
func (a *app) deleteProject(name string) *apiError {
	if name == defaultProject {
		return invalid("The default project cannot be deleted")
	}
	if _, e := a.projectInfo(name); e != nil {
		return e
	}
	var used int
	e := a.db.QueryRow(`SELECT (SELECT count(*) FROM groups_meta WHERE project=?) + (SELECT count(*) FROM tables_meta WHERE project=?) + (SELECT count(*) FROM content_entries WHERE project=?)`, name, name, name).Scan(&used)
	if e != nil {
		return errAPI(e)
	}
	if used > 0 {
		return conflict("Project is not empty")
	}
	root := filepath.Join(a.content, name)
	if entries, readErr := os.ReadDir(root); readErr == nil && len(entries) > 0 {
		return conflict("Project is not empty")
	} else if readErr != nil && !os.IsNotExist(readErr) {
		return errAPI(readErr)
	}
	if _, e = a.db.Exec(`DELETE FROM projects WHERE name=?`, name); e != nil {
		return errAPI(e)
	}
	if e = os.RemoveAll(root); e != nil {
		return errAPI(e)
	}
	return nil
}

// requireProject returns 404 when the project does not exist. It is used by
// project-scoped routing.
func (a *app) requireProject(name string) *apiError {
	var one int
	e := a.db.QueryRow(`SELECT 1 FROM projects WHERE name=?`, name).Scan(&one)
	if errors.Is(e, sql.ErrNoRows) {
		return missing("Project")
	}
	if e != nil {
		return errAPI(e)
	}
	return nil
}

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const bodyLimit int64 = 2 * 1024 * 1024
const zipLimit int64 = 32 * 1024 * 1024

func (a *app) createDirectory(raw string) (string, *apiError) {
	clean, ae := canonicalContentPath(raw)
	if ae != nil {
		return "", ae
	}
	if clean == "/" {
		return "", conflict("Root directory already exists")
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	target, ae := a.contentPath(clean)
	if ae != nil {
		return "", ae
	}
	if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
		return "", ae
	}
	_, statErr := os.Lstat(target)
	if statErr == nil {
		return "", conflict("A resource already exists at this path")
	}
	if !os.IsNotExist(statErr) {
		return "", errAPI(statErr)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		if os.IsExist(err) {
			return "", conflict("A resource already exists at this path")
		}
		return "", errAPI(err)
	}
	return clean, nil
}

func (a *app) directory(raw string) (map[string]any, *apiError) {
	canonical, ae := canonicalContentPath(raw)
	if ae != nil {
		return nil, ae
	}
	target, ae := a.contentPath(canonical)
	if ae != nil {
		return nil, ae
	}
	entries, e := os.ReadDir(target)
	if os.IsNotExist(e) && canonical == "/" {
		// Create the project's content subtree lazily so its root lists as empty.
		if mkErr := os.MkdirAll(target, 0755); mkErr != nil {
			return nil, errAPI(mkErr)
		}
		entries, e = os.ReadDir(target)
	}
	if os.IsNotExist(e) {
		return nil, missing("Directory")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	children := []map[string]any{}
	for _, entry := range entries {
		name := entry.Name()
		child := canonical
		if child == "/" {
			child = "/" + name
		} else {
			child += "/" + name
		}
		typ := "file"
		itemInfo, e := entry.Info()
		if e != nil {
			return nil, errAPI(e)
		}
		if itemInfo.IsDir() {
			typ = "directory"
		} else if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".html") {
			typ = "page"
		}
		item := map[string]any{"name": name, "type": typ, "url": a.contentURL(child)}
		if typ == "page" {
			if strings.HasSuffix(name, ".md") {
				item["content_type"] = "text/markdown"
			} else {
				item["content_type"] = "text/html"
			}
		}
		children = append(children, item)
	}
	sort.Slice(children, func(i, j int) bool { return children[i]["name"].(string) < children[j]["name"].(string) })
	u := a.baseURL
	if canonical != "/" {
		u = a.contentURL(canonical)
	}
	return map[string]any{"path": canonical, "url": u, "children": children}, nil
}

func (a *app) uploadZip(w http.ResponseWriter, r *http.Request, destination string) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	q := r.URL.Query()
	overwrite := false
	if q.Has("overwrite") {
		v := strings.ToLower(q.Get("overwrite"))
		if v != "true" && v != "false" {
			writeAPIError(w, invalid("overwrite must be true or false"))
			return
		}
		overwrite = v == "true"
	}
	dest, ae := canonicalContentPath(destination)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	destDir, ae := a.contentPath(dest)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if info, e := os.Stat(destDir); e == nil && !info.IsDir() {
		writeAPIError(w, conflict("Upload destination is not a directory"))
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, zipLimit+1))
	if e != nil {
		writeErr(w, errAPI(e))
		return
	}
	if int64(len(body)) > zipLimit {
		writeAPIError(w, invalid("Archive compressed size limit exceeded"))
		return
	}
	zr, e := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if e != nil {
		writeAPIError(w, invalid("Invalid ZIP archive"))
		return
	}
	if ae = a.checkNoFileParent(destDir); ae != nil {
		writeErr(w, ae)
		return
	}
	stage, e := os.MkdirTemp(filepath.Dir(a.content), ".clio-upload-")
	if e != nil {
		writeErr(w, errAPI(e))
		return
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload")
	backupDir := filepath.Join(stage, "backups")
	if e = os.Mkdir(payload, 0755); e != nil {
		writeErr(w, errAPI(e))
		return
	}
	files := []string{}
	dirs := map[string]bool{}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	prefixKinds := map[string]bool{}
	expanded := int64(0)
	entryCount := 0
	for _, f := range zr.File {
		entryCount++
		if entryCount > 10000 {
			writeAPIError(w, invalid("Archive has more than 10000 entries"))
			return
		}
		name := f.Name
		if !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || strings.Contains(name, "\\") || regexpDrive(name) {
			writeAPIError(w, invalid("Archive contains an absolute or invalid path"))
			return
		}
		clean := strings.TrimSuffix(name, "/")
		if clean == "" {
			continue
		}
		parts := strings.Split(clean, "/")
		isDir := f.FileInfo().IsDir()
		depth := len(parts)
		if !isDir {
			depth--
		}
		if depth > 32 {
			writeAPIError(w, invalid("Archive directory depth exceeds 32"))
			return
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || hasControl(part) {
				writeAPIError(w, invalid("Archive contains an invalid path"))
				return
			}
		}
		if seen[clean] {
			writeAPIError(w, invalid("Archive contains a duplicate path: "+clean))
			return
		}
		seen[clean] = true
		for parent := path.Dir(clean); parent != "."; parent = path.Dir(parent) {
			if parentIsDir, ok := kinds[parent]; ok && !parentIsDir {
				writeAPIError(w, invalid("Archive file blocks another entry: "+parent))
				return
			}
		}
		if !isDir && prefixKinds[clean] {
			writeAPIError(w, invalid("Archive file blocks another entry: "+clean))
			return
		}
		kinds[clean] = isDir
		for parent := path.Dir(clean); parent != "."; parent = path.Dir(parent) {
			prefixKinds[parent] = true
		}
		relative := filepath.FromSlash(clean)
		full := filepath.Join(destDir, relative)
		if !within(a.contentRoot(), full) {
			writeAPIError(w, invalid("Archive path escapes content directory"))
			return
		}
		joined := dest
		if joined == "/" {
			joined = ""
		}
		joined += "/" + filepath.ToSlash(relative)
		if _, e := canonicalContentPath(joined); e != nil {
			writeErr(w, e)
			return
		}
		stagePath := filepath.Join(payload, relative)
		if isDir {
			if e = os.MkdirAll(stagePath, 0755); e != nil {
				writeErr(w, errAPI(e))
				return
			}
			dirs[relative] = true
			continue
		}
		if f.UncompressedSize64 > 16*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		if f.UncompressedSize64 > 256*1024*1024 || expanded+int64(f.UncompressedSize64) > 256*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		if e = os.MkdirAll(filepath.Dir(stagePath), 0755); e != nil {
			writeErr(w, errAPI(e))
			return
		}
		src, e := f.Open()
		if e != nil {
			writeAPIError(w, invalid("Invalid ZIP archive"))
			return
		}
		dst, e := os.OpenFile(stagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e != nil {
			src.Close()
			writeErr(w, errAPI(e))
			return
		}
		n, copyErr := io.Copy(dst, io.LimitReader(src, 16*1024*1024+1))
		srcErr := src.Close()
		dstErr := dst.Close()
		if copyErr != nil || n > 16*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		if srcErr != nil || dstErr != nil {
			writeErr(w, errAPI(firstError(srcErr, dstErr)))
			return
		}
		expanded += n
		if expanded > 256*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		files = append(files, relative)
	}
	for rel := range dirs {
		target := filepath.Join(destDir, rel)
		info, e := os.Lstat(target)
		if e == nil && !info.IsDir() {
			writeAPIError(w, conflict("Archive directory conflicts with an existing file: "+rel))
			return
		}
		if e != nil && !os.IsNotExist(e) {
			writeErr(w, errAPI(e))
			return
		}
		if ae := a.checkNoFileParent(target); ae != nil {
			writeErr(w, ae)
			return
		}
	}
	uploadTime := formatUTC(time.Now())
	createdTimes := map[string]string{}
	for _, rel := range files {
		target := filepath.Join(destDir, rel)
		if ae := a.checkNoFileParent(filepath.Dir(target)); ae != nil {
			writeErr(w, ae)
			return
		}
		info, e := os.Lstat(target)
		if e == nil && (!overwrite || info.IsDir()) {
			writeAPIError(w, conflict("Archive would replace an existing resource: "+rel))
			return
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			writeAPIError(w, conflict("Archive would replace an incompatible resource: "+rel))
			return
		}
		if e != nil && !os.IsNotExist(e) {
			writeErr(w, errAPI(e))
			return
		}
		entry, found, lookupErr := a.contentEntryByPath(uploadContentPath(dest, rel))
		if lookupErr != nil {
			writeErr(w, errAPI(lookupErr))
			return
		}
		created := uploadTime
		if found {
			created = entry.CreatedAt
		} else if e == nil {
			created = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
		createdTimes[rel] = created
	}
	createdDirs := []string{}
	removeCreatedDirs := func() {
		for i := len(createdDirs) - 1; i >= 0; i-- {
			_ = os.Remove(createdDirs[i])
		}
	}
	for _, dir := range append([]string{destDir}, directoryTargets(destDir, dirs, files)...) {
		if e = ensureContentDirectory(a.contentRoot(), dir, &createdDirs); e != nil {
			removeCreatedDirs()
			writeErr(w, errAPI(e))
			return
		}
	}
	if e = os.Mkdir(backupDir, 0755); e != nil {
		removeCreatedDirs()
		writeErr(w, errAPI(e))
		return
	}
	type installedFile struct {
		target, backup string
		installed      bool
	}
	installed := []installedFile{}
	rollback := func() {
		for i := len(installed) - 1; i >= 0; i-- {
			item := installed[i]
			if item.installed {
				_ = os.Remove(item.target)
			}
			if item.backup != "" {
				_ = os.Rename(item.backup, item.target)
			}
		}
		removeCreatedDirs()
	}
	urls := []string{}
	for i, rel := range files {
		target := filepath.Join(destDir, rel)
		item := installedFile{target: target}
		if info, statErr := os.Lstat(target); statErr == nil {
			if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				rollback()
				writeAPIError(w, conflict("Archive would replace an incompatible resource: "+rel))
				return
			}
			if !overwrite {
				rollback()
				writeAPIError(w, conflict("Archive would replace an existing resource: "+rel))
				return
			}
			item.backup = filepath.Join(backupDir, fmt.Sprintf("%d", i))
			if e = os.Rename(target, item.backup); e != nil {
				rollback()
				writeErr(w, errAPI(e))
				return
			}
		} else if !os.IsNotExist(statErr) {
			rollback()
			writeErr(w, errAPI(statErr))
			return
		}
		installed = append(installed, item)
		if e = os.Rename(filepath.Join(payload, rel), target); e != nil {
			rollback()
			writeErr(w, errAPI(e))
			return
		}
		installed[len(installed)-1].installed = true
		child := dest
		if child == "/" {
			child = ""
		}
		child += "/" + filepath.ToSlash(rel)
		urls = append(urls, a.contentURL(child))
	}
	if len(createdTimes) > 0 {
		tx, txErr := a.db.Begin()
		if txErr != nil {
			rollback()
			writeErr(w, errAPI(txErr))
			return
		}
		for rel, created := range createdTimes {
			data, readErr := os.ReadFile(filepath.Join(destDir, rel))
			if readErr != nil {
				tx.Rollback()
				rollback()
				writeErr(w, errAPI(readErr))
				return
			}
			if txErr = saveContentEntryExec(tx, a.project, uploadContentPath(dest, rel), data, created, ""); txErr != nil {
				tx.Rollback()
				rollback()
				writeErr(w, errAPI(txErr))
				return
			}
		}
		if txErr = tx.Commit(); txErr != nil {
			rollback()
			writeErr(w, errAPI(txErr))
			return
		}
	}
	writeJSON(w, 201, map[string]any{"path": dest, "urls": urls})
}

func directoryTargets(dest string, dirs map[string]bool, files []string) []string {
	all := []string{}
	for rel := range dirs {
		all = append(all, filepath.Join(dest, rel))
	}
	for _, rel := range files {
		all = append(all, filepath.Dir(filepath.Join(dest, rel)))
	}
	return all
}

func uploadContentPath(dest, rel string) string {
	if dest == "/" {
		return "/" + filepath.ToSlash(rel)
	}
	return dest + "/" + filepath.ToSlash(rel)
}

func ensureContentDirectory(root, target string, created *[]string) error {
	if !within(root, target) {
		return fmt.Errorf("directory escapes content root")
	}
	if filepath.Clean(target) == filepath.Clean(root) {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			return os.MkdirAll(target, 0755)
		}
		return nil
	}
	info, err := os.Lstat(target)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component is not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(target)
	if err = ensureContentDirectory(root, parent, created); err != nil {
		return err
	}
	if err = os.Mkdir(target, 0755); err != nil {
		return err
	}
	*created = append(*created, target)
	return nil
}

func firstError(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func (a *app) checkNoFileParent(target string) *apiError {
	for p := target; within(a.contentRoot(), p); p = filepath.Dir(p) {
		info, e := os.Stat(p)
		if e == nil && !info.IsDir() {
			return conflict("A file blocks content path: " + p)
		}
		if errors.Is(e, syscall.ENOTDIR) {
			return conflict("A file blocks content path: " + p)
		}
		if e != nil && !os.IsNotExist(e) {
			return errAPI(e)
		}
		if filepath.Clean(p) == filepath.Clean(a.contentRoot()) {
			break
		}
	}
	return nil
}
func (a *app) canonicalContentPath(raw string) (string, *apiError) { return canonicalContentPath(raw) }
func canonicalContentPath(raw string) (string, *apiError) {
	if !utf8.ValidString(raw) {
		return "", invalid("Content paths must be valid UTF-8")
	}
	if raw == "" {
		return "/", nil
	}
	if !strings.HasPrefix(raw, "/") {
		return "", invalid("Content paths must start with /")
	}
	if raw == "/" {
		return raw, nil
	}
	if strings.HasSuffix(raw, "/") {
		return "", invalid("Trailing slash is not canonical")
	}
	parts := strings.Split(strings.TrimPrefix(raw, "/"), "/")
	for i, p := range parts {
		if p == "" || p == "." || p == ".." || strings.Contains(p, "\\") || hasControl(p) {
			return "", invalid("Invalid content path")
		}
		if i == 0 && (reservedRoot[p] || p == reservedFilesSegment) {
			return "", invalid("Reserved root path")
		}
	}
	return raw, nil
}
func (a *app) contentPath(raw string) (string, *apiError) {
	clean, e := canonicalContentPath(raw)
	if e != nil {
		return "", e
	}
	rel := strings.TrimPrefix(clean, "/")
	target := filepath.Join(a.contentRoot(), filepath.FromSlash(rel))
	if !within(a.contentRoot(), target) {
		return "", invalid("Path escapes content directory")
	}
	for p := target; p != a.contentRoot(); p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", invalid("Symbolic links are not permitted in content paths")
		}
		if errors.Is(e, syscall.ENOTDIR) {
			return "", conflict("A file blocks content path: " + p)
		}
		if e != nil && !os.IsNotExist(e) {
			return "", errAPI(e)
		}
	}
	return target, nil
}
func within(root, p string) bool {
	r, e := filepath.Rel(root, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}
func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
func regexpDrive(s string) bool {
	return len(s) >= 2 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && s[1] == ':'
}

func (a *app) contentUI(w http.ResponseWriter, r *http.Request, s []string) {
	// The reserved `id` segment is the stable URL /{project}/files/id/{id}
	// (section 66.5). It always downloads the raw stored bytes, including for a
	// page, and must be handled before content-path canonicalisation rejects the
	// reserved first segment.
	if len(s) > 0 && s[0] == reservedFilesSegment {
		if len(s) != 2 {
			writeAPIError(w, missing("Content"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeAPIError(w, methodNotAllowed())
			return
		}
		a.serveFileContent(w, r, s[1])
		return
	}
	if r.Method != "GET" && r.Method != "POST" && r.Method != "HEAD" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	raw := "/"
	if len(s) > 0 {
		raw = "/" + strings.Join(s, "/")
	}
	clean, e := canonicalContentPath(raw)
	if e != nil {
		writeErr(w, e)
		return
	}
	target, e := a.contentPath(clean)
	if e != nil {
		writeErr(w, e)
		return
	}
	info, err := os.Stat(target)
	if os.IsNotExist(err) && clean == "/" {
		// The project's content subtree is created lazily; an absent root is an
		// empty directory.
		info, err = nil, nil
	}
	if os.IsNotExist(err) {
		writeAPIError(w, missing("Content"))
		return
	}
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if info == nil || info.IsDir() {
		if r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
			if err := r.ParseForm(); err != nil {
				writeHTML(w, http.StatusBadRequest, a.pageShell("<h1>Invalid form</h1><p>Could not read the directory name.</p>", "Invalid form"))
				return
			}
			name := r.Form.Get("name")
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
				writeHTML(w, http.StatusUnprocessableEntity, a.pageShell("<h1>Invalid directory name</h1><p>Enter a single directory name.</p>", "Invalid directory name"))
				return
			}
			child := name
			if clean != "/" {
				child = clean + "/" + name
			} else {
				child = "/" + name
			}
			created, ae := a.createDirectory(child)
			if ae != nil {
				status := ae.Status
				message := esc(ae.Message)
				writeHTML(w, status, a.pageShell("<h1>Could not create directory</h1><p>"+message+"</p>", "Directory error"))
				return
			}
			http.Redirect(w, r, a.filesPath(created), http.StatusSeeOther)
			return
		}
		d, ae := a.directory(clean)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		writeHTML(w, 200, a.directoryHTML(d))
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	if !info.Mode().IsRegular() {
		writeAPIError(w, missing("Content"))
		return
	}
	if strings.HasSuffix(target, ".md") {
		data, err := os.ReadFile(target)
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		parent := path.Dir(clean)
		if parent == "." {
			parent = "/"
		}
		body := `<style>
body{max-width:none;margin:0 auto;padding:0 1.4rem;background:#f4f7fb;color:#192a41}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:#fff;border-bottom:1px solid #e3e9f1;font-size:.9rem}body>nav a{color:#52647a}body>nav a:first-child{font-weight:750;color:#182f4b}main{max-width:none!important}.published-page{max-width:850px;margin:1.2rem auto 4rem;padding:clamp(1.25rem,4vw,2.6rem);border:1px solid #e3e9f1;border-radius:14px;background:#fff;box-shadow:0 5px 22px rgba(24,47,75,.045)}.published-back{display:inline-block;margin-bottom:1.5rem;color:#286a83;font-size:.85rem;font-weight:650;text-decoration:none}.published-back:hover{text-decoration:underline}.published-markdown{color:#344b62;line-height:1.75}.published-markdown h1,.published-markdown h2,.published-markdown h3{color:#192a41;line-height:1.25;letter-spacing:-.03em}.published-markdown h1{font-size:clamp(1.8rem,4vw,2.4rem);margin:.15rem 0 1.2rem}.published-markdown h2{font-size:1.45rem;margin:2rem 0 .7rem}.published-markdown p{margin:.75rem 0}.published-markdown a{color:#216b82}.published-markdown li{margin:.3rem 0}.published-markdown pre{padding:1rem;border:1px solid #e4eaf0;border-radius:8px;background:#f5f8fa}.published-markdown code{padding:.12em .3em;border-radius:4px;background:#f0f4f7;color:#244c65}.published-markdown pre code{padding:0;background:transparent}.published-markdown blockquote{margin:1rem 0;padding:.15rem 1rem;border-left:3px solid #72b7b5;color:#60778a;background:#f6fafb}@media(max-width:700px){body{padding:0 .65rem}body>nav{margin:0 -.65rem .8rem;padding:.7rem .65rem;gap:.55rem;font-size:.82rem}.published-page{margin:.65rem auto 2rem;padding:1.1rem;border-radius:10px}}
</style><article class="published-page"><a class="published-back" href="` + htmlAttr(a.filesPath(parent)) + `">← Back to folder</a><div class="published-markdown">` + markdownHTML(string(data)) + `</div></article>`
		writeHTML(w, 200, a.pageShell(body, clean))
		return
	}
	if strings.HasSuffix(target, ".html") {
		data, err := os.ReadFile(target)
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		writeHTML(w, 200, string(data))
		return
	}
	// Any other content is served as a download so it cannot execute in-origin
	// (section 64.5). http.ServeContent supplies byte ranges and HEAD.
	if ae := serveDownload(w, r, target, contentMediaType(clean)); ae != nil {
		writeErr(w, ae)
	}
}

func (a *app) directoryHTML(d map[string]any) string {
	pathName := d["path"].(string)
	children, _ := d["children"].([]map[string]any)
	if pathName == "/" {
		return a.homeHTML(children)
	}
	return a.contentDirectoryHTML(pathName, children)
}

func (a *app) contentDirectoryHTML(pathName string, children []map[string]any) string {
	var b strings.Builder
	b.WriteString(`<style>
body{max-width:none;margin:0 auto;padding:0 1.4rem;background:#f4f7fb;color:#192a41}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:#fff;border-bottom:1px solid #e3e9f1;font-size:.9rem}body>nav a{color:#52647a}body>nav a:first-child{font-weight:750;color:#182f4b}main{max-width:none!important}.content-directory{max-width:1120px;margin:1.3rem auto 4rem}.content-breadcrumbs{display:flex;align-items:center;gap:.5rem;margin:.2rem 0 1rem;color:#91a0ae;font-size:.82rem}.content-breadcrumbs a{color:#60778a;text-decoration:none}.content-breadcrumbs a:hover{color:#216b82}.content-heading{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:1rem;padding:1.3rem 1.4rem;border:1px solid #e3e9f1;border-radius:12px;background:#fff}.content-heading-main{display:flex;align-items:center;gap:.9rem;min-width:0}.content-folder-mark{display:grid;place-items:center;flex:none;width:3rem;height:3rem;border-radius:10px;background:#e9f4f6;color:#24758a;font-size:1.35rem}.content-eyebrow{margin:0 0 .2rem;color:#8192a2;font-size:.68rem;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.content-heading h1{margin:0;color:#192a41;font-size:clamp(1.3rem,3vw,1.8rem);letter-spacing:-.035em;overflow-wrap:anywhere}.content-count{margin:.3rem 0 0;color:#7a8b9a;font-size:.8rem}.content-create summary{padding:.58rem .78rem;border:1px solid #dce5eb;border-radius:7px;color:#315f75;font-size:.82rem;font-weight:650;cursor:pointer;white-space:nowrap}.content-create form{display:flex;gap:.45rem;margin-top:.55rem}.content-create label{margin:0}.content-create input{box-sizing:border-box;width:10rem;padding:.52rem .6rem;border:1px solid #dce5eb;border-radius:6px}.content-create button{padding:.54rem .7rem;border-radius:6px;background:#215f78;font-size:.8rem;cursor:pointer}.content-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,16rem),1fr));gap:.7rem}.content-entry{display:flex;align-items:center;gap:.75rem;min-width:0;padding:.85rem;border:1px solid #e3e9f1;border-radius:9px;background:#fff;color:#344b62;text-decoration:none;box-shadow:0 2px 8px rgba(24,47,75,.025);transition:background-color .16s ease,border-color .16s ease,transform .16s ease}.content-entry:hover{transform:translateY(-1px);border-color:#bdd8df;background:#fbfefe}.content-entry-mark{display:grid;place-items:center;flex:none;width:2.25rem;height:2.25rem;border-radius:7px;background:#eff3f7;color:#52728b;font-size:1.05rem}.content-entry-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.9rem;font-weight:650}.content-entry-type{color:#8493a1;font-size:.72rem}.content-entry-arrow{color:#3b8291}.content-empty{padding:1.4rem;border:1px dashed #d5e0e8;border-radius:9px;background:rgba(255,255,255,.55);color:#718394;font-size:.88rem}@media(max-width:700px){body{padding:0 .65rem}body>nav{margin:0 -.65rem .8rem;padding:.7rem .65rem;gap:.55rem;font-size:.82rem}.content-directory{margin:.65rem auto 2rem}.content-heading{align-items:flex-start;padding:1rem}.content-create summary{font-size:0}.content-create summary:after{content:"＋ Folder";font-size:.82rem}.content-create[open] summary{font-size:.82rem}.content-create[open] summary:after{content:""}.content-create input{width:8rem}}
</style><div class="content-directory"><nav class="content-breadcrumbs" aria-label="Breadcrumb"><a href="` + htmlAttr(a.filesPath("/")) + `">Clio Home</a>`)
	current := ""
	for _, part := range splitPath(strings.TrimPrefix(pathName, "/")) {
		current += "/" + part
		b.WriteString(`<span aria-hidden="true">/</span><a href="` + htmlAttr(a.filesPath(current)) + `">` + esc(part) + `</a>`)
	}
	name := path.Base(pathName)
	b.WriteString(`</nav><header class="content-heading"><div class="content-heading-main"><span class="content-folder-mark" aria-hidden="true">▰</span><div><p class="content-eyebrow">Published content</p><h1>` + esc(name) + `</h1><p class="content-count">` + fmt.Sprint(len(children)) + ` items</p></div></div><details class="content-create"><summary>＋ New folder</summary><form method="post" action="` + htmlAttr(a.filesPath(pathName)) + `"><label>Folder name<input name="name" placeholder="Folder name" required></label><button type="submit">Create</button></form></details></header><section class="content-list" aria-label="Folder contents">`)
	if len(children) == 0 {
		b.WriteString(`<p class="content-empty">This folder is empty. Add published pages or create a subfolder to get started.</p>`)
	}
	for _, child := range children {
		u := strings.TrimPrefix(fmt.Sprint(child["url"]), a.baseURL)
		childName := fmt.Sprint(child["name"])
		typeName := fmt.Sprint(child["type"])
		icon := "▤"
		if typeName == "directory" {
			icon = "▰"
		}
		b.WriteString(`<a class="content-entry" href="` + htmlAttr(u) + `"><span class="content-entry-mark" aria-hidden="true">` + icon + `</span><span class="content-entry-name">` + esc(childName) + `</span><span class="content-entry-type">` + esc(typeName) + `</span><span class="content-entry-arrow" aria-hidden="true">→</span></a>`)
	}
	b.WriteString(`</section></div>`)
	return a.pageShell(b.String(), name)
}

func (a *app) homeHTML(children []map[string]any) string {
	groups, _ := a.listGroups()
	tableCount := 0
	var collections strings.Builder
	for _, group := range groups {
		groupName := fmt.Sprint(group["name"])
		tables, err := a.listTables(groupName)
		if err != nil {
			continue
		}
		tableCount += len(tables)
		label := group["label"]
		if label == nil || fmt.Sprint(label) == "" {
			label = groupName
		}
		collections.WriteString(`<section class="home-collection"><div class="home-collection-heading"><span class="home-collection-mark" aria-hidden="true">▦</span><div><h3>` + esc(label) + `</h3><span>` + fmt.Sprint(len(tables)) + ` tables</span></div></div>`)
		for _, table := range tables {
			tableName := fmt.Sprint(table["name"])
			tableLabel := table["label"]
			if tableLabel == nil || fmt.Sprint(tableLabel) == "" {
				tableLabel = tableName
			}
			collections.WriteString(`<a class="home-table-link" href="` + htmlAttr(a.projectPath("collections", groupName, tableName)) + `"><span>` + esc(tableLabel) + `</span><span aria-hidden="true">→</span></a>`)
		}
		collections.WriteString(`</section>`)
	}
	if collections.Len() == 0 {
		collections.WriteString(`<p class="home-empty">No collections yet. Create one through the API, then browse it here.</p>`)
	}

	var content strings.Builder
	for _, child := range children {
		name := fmt.Sprint(child["name"])
		typeName := fmt.Sprint(child["type"])
		icon := "▤"
		if typeName == "directory" {
			icon = "▰"
		}
		href := strings.TrimPrefix(fmt.Sprint(child["url"]), a.baseURL)
		content.WriteString(`<a class="home-content-link" href="` + htmlAttr(href) + `"><span class="home-file-mark" aria-hidden="true">` + icon + `</span><span class="home-file-name">` + esc(name) + `</span><span class="home-file-type">` + esc(typeName) + `</span><span aria-hidden="true">→</span></a>`)
	}
	if content.Len() == 0 {
		content.WriteString(`<p class="home-empty">Published pages and folders will appear here.</p>`)
	}

	body := `<style>
.home-page{--home-ink:#192a41;--home-muted:#718096;--home-line:#e3e9f1;max-width:1120px;margin:1.5rem auto 4rem;color:var(--home-ink)}body{max-width:none;background:#f4f7fb;color:var(--home-ink);margin:0 auto;padding:0 1.4rem}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:#fff;border-bottom:1px solid var(--home-line);font-size:.9rem}body>nav a:first-child{font-weight:750;letter-spacing:-.02em;color:#182f4b}body>nav a{color:#52647a}main{max-width:none!important}.home-hero{position:relative;display:grid;grid-template-columns:minmax(0,1.35fr) minmax(15rem,.65fr);gap:2rem;align-items:center;overflow:hidden;padding:clamp(1.5rem,4vw,3.25rem);border-radius:18px;background:radial-gradient(ellipse at 85% 0%,rgba(88,190,198,.3),transparent 35%),linear-gradient(125deg,#163959,#155a70 72%,#187181);color:#fff;box-shadow:0 16px 40px rgba(22,57,89,.14)}.home-hero:after{position:absolute;content:"";width:22rem;height:22rem;border:1px solid rgba(255,255,255,.11);border-radius:50%;right:-8rem;bottom:-18rem;pointer-events:none}.home-eyebrow{margin:0 0 .9rem;color:#a9e1e1;font-size:.72rem;font-weight:700;letter-spacing:.14em;text-transform:uppercase}.home-hero h1{max-width:12ch;margin:0;font-size:clamp(2rem,4vw,3.25rem);line-height:1.04;letter-spacing:-.055em}.home-hero-copy{max-width:34rem;margin:.9rem 0 1.35rem;color:#d2e3ed;line-height:1.6}.home-actions{display:flex;flex-wrap:wrap;align-items:center;gap:.85rem}.home-primary{display:inline-flex;align-items:center;gap:.85rem;padding:.7rem 1rem;border-radius:7px;background:#fff;color:#153b59;text-decoration:none;font-weight:700}.home-primary:hover{background:#eaf8f8}.home-secondary{color:#e4f1f4;text-decoration:none;font-weight:600}.home-secondary:hover{text-decoration:underline}.home-hero-aside{position:relative;z-index:1;padding:1.15rem;border:1px solid rgba(255,255,255,.2);border-radius:12px;background:rgba(7,34,55,.2);backdrop-filter:blur(8px)}.home-ready{display:flex;align-items:center;gap:.65rem;font-weight:700}.home-ready-dot{width:.55rem;height:.55rem;border-radius:50%;background:#74e0b5;box-shadow:0 0 0 4px rgba(116,224,181,.13)}.home-hero-aside p{margin:.65rem 0 0;color:#c9dce6;font-size:.88rem;line-height:1.55}.home-stats{display:flex;gap:1.5rem;margin-top:1.1rem;padding-top:.9rem;border-top:1px solid rgba(255,255,255,.18)}.home-stat strong,.home-stat span{display:block}.home-stat strong{font-size:1.4rem;letter-spacing:-.03em}.home-stat span{color:#c9dce6;font-size:.75rem}.home-grid{display:grid;grid-template-columns:minmax(0,1.3fr) minmax(17rem,.7fr);gap:1rem;margin-top:1rem}.home-card{min-width:0;padding:1.2rem;border:1px solid var(--home-line);border-radius:12px;background:#fff;box-shadow:0 3px 14px rgba(24,47,75,.035)}.home-card-heading{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:.9rem}.home-card-heading h2{margin:0;font-size:1.05rem;letter-spacing:-.025em}.home-card-heading p{margin:.25rem 0 0;color:var(--home-muted);font-size:.83rem}.home-card-heading>a{font-size:.82rem;font-weight:650;text-decoration:none}.home-collection{padding:.9rem 0;border-top:1px solid #edf0f4}.home-collection:first-child{padding-top:0;border-top:0}.home-collection-heading{display:flex;align-items:center;gap:.65rem;margin-bottom:.4rem}.home-collection-mark{display:grid;place-items:center;width:2rem;height:2rem;border-radius:7px;background:#eaf4f8;color:#236b81;font-size:1.1rem}.home-collection-heading h3{margin:0;font-size:.9rem}.home-collection-heading div>span{color:var(--home-muted);font-size:.73rem}.home-table-link{display:flex;justify-content:space-between;gap:1rem;padding:.48rem .55rem;border-radius:6px;color:#3d566e;text-decoration:none;font-size:.85rem}.home-table-link:hover,.home-content-link:hover{background:#f3f7fa;color:#1b6f83}.home-content-link{display:flex;align-items:center;gap:.6rem;padding:.66rem .35rem;border-top:1px solid #edf0f4;color:#344b62;text-decoration:none;font-size:.85rem}.home-content-link:first-child{border-top:0}.home-file-mark{display:grid;place-items:center;width:1.8rem;height:1.8rem;border-radius:6px;background:#eff3f8;color:#55728b}.home-file-name{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.home-file-type{color:var(--home-muted);font-size:.72rem}.home-empty{margin:.2rem 0;color:var(--home-muted);font-size:.85rem;line-height:1.5}.home-create{margin-top:1rem;padding-top:.85rem;border-top:1px solid #edf0f4}.home-create summary{color:#526b80;font-size:.82rem;font-weight:600;cursor:pointer}.home-create form{display:flex;align-items:center;gap:.5rem;margin-top:.6rem}.home-create label{margin:0;flex:1}.home-create input{width:100%;box-sizing:border-box;padding:.5rem .6rem;border:1px solid #dce4ec;border-radius:6px}.home-create button{padding:.53rem .7rem;border-radius:6px;background:#215f78;font-size:.8rem;cursor:pointer}.visually-hidden{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}@media(max-width:760px){body{padding:0 .7rem}body>nav{margin:0 -.7rem .8rem;padding:.7rem;gap:.55rem;font-size:.82rem}.home-page{margin:.7rem auto 2rem}.home-hero{grid-template-columns:1fr;gap:1.2rem;padding:1.4rem;border-radius:13px}.home-hero-aside{max-width:none}.home-grid{grid-template-columns:1fr}.home-card{padding:1rem}}@media(prefers-reduced-motion:no-preference){.home-primary,.home-table-link,.home-content-link{transition:background-color .16s ease,color .16s ease}}
</style><div class="home-page"><section class="home-hero"><div><p class="home-eyebrow">CLIO · CONTENT WORKSPACE</p><h1>Everything you need, in one place.</h1><p class="home-hero-copy">Browse structured collections and explore your published content from one simple workspace.</p><div class="home-actions"><a class="home-primary" href="` + htmlAttr(a.projectPath("collections")) + `">Open data browser <span aria-hidden="true">→</span></a><a class="home-secondary" href="` + htmlAttr(a.projectPath("help")) + `">Read the guide</a></div></div><aside class="home-hero-aside"><div class="home-ready"><span class="home-ready-dot" aria-hidden="true"></span>Your workspace is ready</div><p>Collections, tables, and published pages are all close at hand.</p><div class="home-stats"><div class="home-stat"><strong>` + fmt.Sprint(len(groups)) + `</strong><span>collections</span></div><div class="home-stat"><strong>` + fmt.Sprint(tableCount) + `</strong><span>tables</span></div><div class="home-stat"><strong>` + fmt.Sprint(len(children)) + `</strong><span>content items</span></div></div></aside></section><div class="home-grid"><section class="home-card"><div class="home-card-heading"><div><h2>Collections</h2><p>Your structured data, organized by group.</p></div><a href="` + htmlAttr(a.projectPath("collections")) + `">Browse all →</a></div>` + collections.String() + `</section><section class="home-card"><div class="home-card-heading"><div><h2>Published content</h2><p>Pages and folders in your content library.</p></div></div>` + content.String() + `<details class="home-create"><summary>＋ Create a folder</summary><form method="post" action="` + htmlAttr(a.filesPath("/")) + `"><label><span class="visually-hidden">Folder name</span><input name="name" placeholder="Folder name" required></label><button type="submit">Create</button></form></details></section></div></div>`
	return a.pageShell(body, "Home")
}
func htmlAttr(s string) string { return strings.ReplaceAll(esc(s), "&#39;", "&#39;") }

func markdownHTML(source string) string {
	var out strings.Builder
	inList, inCode := false, false
	var code strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "```") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			if inCode {
				out.WriteString("<pre><code>" + esc(code.String()) + "</code></pre>")
				code.Reset()
				inCode = false
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			code.WriteString(line + "\n")
			continue
		}
		if strings.HasPrefix(line, "#") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			n := 0
			for n < len(line) && line[n] == '#' {
				n++
			}
			if n <= 6 && len(line) > n && line[n] == ' ' {
				out.WriteString(fmt.Sprintf("<h%d>%s</h%d>", n, inlineMarkdown(line[n+1:]), n))
			} else {
				out.WriteString("<p>" + inlineMarkdown(line) + "</p>")
			}
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>" + inlineMarkdown(line[2:]) + "</li>")
		} else {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			if strings.TrimSpace(line) != "" {
				out.WriteString("<p>" + inlineMarkdown(line) + "</p>")
			}
		}
	}
	if inList {
		out.WriteString("</ul>")
	}
	if inCode {
		out.WriteString("<pre><code>" + esc(code.String()) + "</code></pre>")
	}
	return out.String()
}
func inlineMarkdown(s string) string {
	safe := esc(s)
	safe = regexpReplace(safe, "`([^`]+)`", "<code>$1</code>")
	safe = regexpReplace(safe, `\*\*([^*]+)\*\*`, `<strong>$1</strong>`)
	safe = regexpReplace(safe, `\*([^*]+)\*`, `<em>$1</em>`)
	re := regexp.MustCompile(`\[([^]]+)\]\((https?://[^ )]+)\)`)
	return re.ReplaceAllString(safe, `<a href="$2">$1</a>`)
}
func regexpReplace(s, pattern, replacement string) string {
	return regexp.MustCompile(pattern).ReplaceAllString(s, replacement)
}

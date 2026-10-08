package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// version is the product version reported by /api/v1/health and the startup
// log. It defaults to the product version documented in SPEC.md and is
// overridden at build time with -ldflags "-X main.version=...", normally from
// the git tag (see the Makefile and Dockerfile).
var version = "1.0.0"

type app struct {
	db        *sql.DB
	content   string
	baseURL   string
	auth      authConfig
	started   time.Time
	contentMu *sync.Mutex
	// project is the project scope of a request. The zero value means the
	// default project. ServeHTTP replaces the handler with a shallow copy that
	// carries the resolved project (see withProject); the shared fields are
	// read-only for the life of a request.
	project string
}

// withProject returns a shallow copy of the app scoped to a project. Fields are
// shared, including the content mutex pointer, so copies are safe to use
// concurrently. The zero-value project means the default project.
func (a *app) withProject(project string) *app {
	scoped := *a
	scoped.project = project
	if scoped.contentMu == nil {
		scoped.contentMu = &sync.Mutex{}
	}
	return &scoped
}

func (a *app) contentLock() *sync.Mutex {
	if a.contentMu == nil {
		a.contentMu = &sync.Mutex{}
	}
	return a.contentMu
}

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) == 2 && os.Args[1] == "hash-password" {
			if err := generatePasswordHash(os.Stdin, os.Stdout, os.Stderr); err != nil {
				log.Fatalf("hash-password: %v", err)
			}
			return
		}
		log.Fatalf("usage: %s [hash-password]", filepath.Base(os.Args[0]))
	}
	auth, err := loadAuthConfig(os.LookupEnv)
	if err != nil {
		log.Fatalf("invalid authentication or TLS configuration: %v", err)
	}
	addr := env("CLIO_ADDR", "0.0.0.0:8080")
	addr, err = bindAddress(addr)
	if err != nil {
		log.Fatalf("invalid CLIO_ADDR: %v", err)
	}
	dataDir := env("CLIO_DATA_DIR", "./data")
	dbPath := env("CLIO_DB", filepath.Join(dataDir, "clio.db"))
	baseURL := strings.TrimRight(env("CLIO_BASE_URL", "http://localhost:8080"), "/")
	publicURL, err := url.Parse(baseURL)
	if err != nil || (publicURL.Scheme != "http" && publicURL.Scheme != "https") || publicURL.Host == "" || (publicURL.Path != "" && publicURL.Path != "/") || publicURL.RawQuery != "" || publicURL.Fragment != "" || publicURL.User != nil {
		log.Fatalf("CLIO_BASE_URL must be an absolute HTTP(S) URL without query or fragment")
	}
	content := filepath.Join(dataDir, "content")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Fatalf("create database directory: %v", err)
	}
	if err := os.MkdirAll(content, 0755); err != nil {
		log.Fatalf("create content directory: %v", err)
	}
	db, err := openDatabase(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	a := &app{db: db, content: content, baseURL: baseURL, auth: auth, started: time.Now(), contentMu: &sync.Mutex{}}
	if err = a.migrateContentLayout(); err != nil {
		log.Fatalf("migrate content layout: %v", err)
	}
	if err = a.reconcileAllContent(); err != nil {
		log.Fatalf("reconcile content entries: %v", err)
	}
	srv := &http.Server{Addr: addr, Handler: a, ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("HTTP server failed to listen: %v", err)
	}
	transport := "HTTP"
	if auth.tlsCertificateFile != "" {
		certificate, loadErr := tls.LoadX509KeyPair(auth.tlsCertificateFile, auth.tlsPrivateKeyFile)
		if loadErr != nil {
			_ = listener.Close()
			log.Fatalf("load TLS certificate and key: %v", loadErr)
		}
		listener = tls.NewListener(listener, &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		})
		transport = "HTTPS"
	}
	log.Printf("Clio %s listening with %s on %s (public URL %s)", version, transport, addr, baseURL)
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)
	if err := serveUntilSignal(srv, listener, shutdownSignals); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func serveUntilSignal(srv *http.Server, listener net.Listener, shutdownSignals <-chan os.Signal) error {
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- srv.Serve(listener) }()
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-shutdownSignals:
		log.Printf("HTTP shutdown requested: signal=%s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		shutdownErr := srv.Shutdown(ctx)
		cancel()
		if shutdownErr != nil {
			log.Printf("HTTP graceful shutdown failed; forcing close")
			if closeErr := srv.Close(); closeErr != nil {
				log.Printf("HTTP forced close failed")
			}
		}
		serveErr := <-serveErrors
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		log.Printf("Clio shutdown complete")
		return nil
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func openDatabase(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + url.PathEscape(abs) + "?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL&_synchronous=NORMAL"
	db, err := sql.Open(querySQLiteDriver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	var mode string
	if err = db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		db.Close()
		return nil, err
	}
	if !strings.EqualFold(mode, "wal") {
		db.Close()
		return nil, fmt.Errorf("SQLite WAL mode unavailable (journal_mode=%s)", mode)
	}
	if err = reconcileAllTableIndexes(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("reconcile indexes: %w", err)
	}
	return db, nil
}

// migrateSchema creates the SQLite schema and upgrades databases created by
// earlier Clio versions. Project scoping (section 65.7) makes the primary and
// foreign keys of the data tables project-scoped, so a legacy database needs
// those tables rebuilt rather than a plain ALTER TABLE. The rebuild runs on one
// connection with foreign keys disabled so that dropping a parent table cannot
// cascade into its children.
func migrateSchema(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if _, e := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); e != nil {
			log.Printf("restore foreign key enforcement: %v", e)
		}
		_ = conn.Close()
	}()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS projects (name TEXT PRIMARY KEY, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS groups_meta (project TEXT NOT NULL DEFAULT 'default', name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(project,name), FOREIGN KEY(project) REFERENCES projects(name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS tables_meta (project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, timestamp_field TEXT, indexes TEXT NOT NULL DEFAULT '[]', PRIMARY KEY(project,group_name,name), FOREIGN KEY(project,group_name) REFERENCES groups_meta(project,name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS fields_meta (project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, table_name TEXT NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL, definition TEXT NOT NULL, PRIMARY KEY(project,group_name,table_name,name), FOREIGN KEY(project,group_name,table_name) REFERENCES tables_meta(project,group_name,name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS records (id TEXT PRIMARY KEY, project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, table_name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, data TEXT NOT NULL, timestamp_value TEXT, FOREIGN KEY(project,group_name,table_name) REFERENCES tables_meta(project,group_name,name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS content_entries (id TEXT PRIMARY KEY, project TEXT NOT NULL DEFAULT 'default', path TEXT NOT NULL, kind TEXT NOT NULL, content_type TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0, sha256 TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(project,path), FOREIGN KEY(project) REFERENCES projects(name) ON DELETE CASCADE)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS content_search USING fts5(id UNINDEXED, project UNINDEXED, path UNINDEXED, kind UNINDEXED, source UNINDEXED, title, body, tokenize='unicode61')`,
		`CREATE TABLE IF NOT EXISTS managed_indexes (name TEXT PRIMARY KEY, project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, table_name TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err = conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	// The implicit default project always exists (section 65.1).
	if _, err = conn.ExecContext(ctx, `INSERT OR IGNORE INTO projects(name,label,description,sort_order,created_at) VALUES('default','Default','',0,?)`, formatUTC(time.Now())); err != nil {
		return err
	}
	// Add the schema declaration column to databases created by earlier Clio
	// versions before tables_meta is rebuilt.
	if err = ensureColumn(ctx, conn, "tables_meta", "indexes", `TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return err
	}
	if err = ensureColumn(ctx, conn, "managed_indexes", "project", `TEXT NOT NULL DEFAULT 'default'`); err != nil {
		return err
	}
	if err = migrateContentEntries(ctx, conn); err != nil {
		return err
	}
	rebuilds := []struct{ table, create, copy string }{
		{
			"groups_meta",
			`CREATE TABLE groups_meta_new (project TEXT NOT NULL DEFAULT 'default', name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(project,name), FOREIGN KEY(project) REFERENCES projects(name) ON DELETE CASCADE)`,
			`INSERT INTO groups_meta_new(project,name,label,description,sort_order) SELECT 'default',name,label,description,sort_order FROM groups_meta`,
		},
		{
			"tables_meta",
			`CREATE TABLE tables_meta_new (project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, timestamp_field TEXT, indexes TEXT NOT NULL DEFAULT '[]', PRIMARY KEY(project,group_name,name), FOREIGN KEY(project,group_name) REFERENCES groups_meta(project,name) ON DELETE CASCADE)`,
			`INSERT INTO tables_meta_new(project,group_name,name,label,description,kind,timestamp_field,indexes) SELECT 'default',group_name,name,label,description,kind,timestamp_field,indexes FROM tables_meta`,
		},
		{
			"fields_meta",
			`CREATE TABLE fields_meta_new (project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, table_name TEXT NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL, definition TEXT NOT NULL, PRIMARY KEY(project,group_name,table_name,name), FOREIGN KEY(project,group_name,table_name) REFERENCES tables_meta(project,group_name,name) ON DELETE CASCADE)`,
			`INSERT INTO fields_meta_new(project,group_name,table_name,name,position,definition) SELECT 'default',group_name,table_name,name,position,definition FROM fields_meta`,
		},
		{
			"records",
			`CREATE TABLE records_new (id TEXT PRIMARY KEY, project TEXT NOT NULL DEFAULT 'default', group_name TEXT NOT NULL, table_name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, data TEXT NOT NULL, timestamp_value TEXT, FOREIGN KEY(project,group_name,table_name) REFERENCES tables_meta(project,group_name,name) ON DELETE CASCADE)`,
			`INSERT INTO records_new(id,project,group_name,table_name,created_at,updated_at,data,timestamp_value) SELECT id,'default',group_name,table_name,created_at,updated_at,data,timestamp_value FROM records`,
		},
	}
	for _, rebuild := range rebuilds {
		if err = rebuildProjectTable(ctx, conn, rebuild.table, rebuild.create, rebuild.copy); err != nil {
			return err
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS records_table_idx ON records(group_name,table_name)`,
		`CREATE INDEX IF NOT EXISTS records_created_idx ON records(group_name,table_name,created_at)`,
		`CREATE INDEX IF NOT EXISTS records_timeseries_idx ON records(group_name,table_name,timestamp_value)`,
		`CREATE INDEX IF NOT EXISTS records_created_query_idx ON records(group_name,table_name,created_at COLLATE CLIO_DATETIME DESC)`,
		`CREATE INDEX IF NOT EXISTS records_timeseries_query_idx ON records(group_name,table_name,timestamp_value COLLATE CLIO_DATETIME)`,
		`CREATE INDEX IF NOT EXISTS records_temporal_page_idx ON records(group_name,table_name,timestamp_value COLLATE CLIO_DATETIME DESC,id DESC)`,
	}
	for _, statement := range indexes {
		if _, err = conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// rebuildProjectTable gives a legacy table project-scoped primary and foreign
// keys. It is a no-op once the table already has a project column.
func rebuildProjectTable(ctx context.Context, conn *sql.Conn, table, create, copy string) error {
	has, err := tableHasColumn(ctx, conn, table, "project")
	if err != nil || has {
		return err
	}
	statements := []string{`DROP TABLE IF EXISTS ` + table + `_new`, create, copy, `DROP TABLE ` + table, `ALTER TABLE ` + table + `_new RENAME TO ` + table}
	for _, statement := range statements {
		if _, err = conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate %s to project scope: %w", table, err)
		}
	}
	return nil
}

// migrateContentEntries converts the legacy content_page_times table into
// project-scoped content entries, assigning every row to the default project
// with a new ID (section 64.2, section 65.7). The legacy table is dropped.
func migrateContentEntries(ctx context.Context, conn *sql.Conn) error {
	exists, err := tableExists(ctx, conn, "content_page_times")
	if err != nil || !exists {
		return err
	}
	rows, err := conn.QueryContext(ctx, `SELECT path,created_at,updated_at FROM content_page_times`)
	if err != nil {
		return err
	}
	type legacy struct{ path, created, updated string }
	entries := []legacy{}
	for rows.Next() {
		var item legacy
		if err = rows.Scan(&item.path, &item.created, &item.updated); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range entries {
		if _, err = conn.ExecContext(ctx, `INSERT INTO content_entries(id,project,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			newID(), defaultProject, item.path, contentKind(item.path), contentMediaType(item.path), 0, "", item.created, item.updated); err != nil {
			return err
		}
	}
	if _, err = conn.ExecContext(ctx, `DROP TABLE content_page_times`); err != nil {
		return err
	}
	return nil
}

func tableExists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var one int
	err := conn.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func tableHasColumn(ctx context.Context, conn *sql.Conn, table, column string) (bool, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ensureColumn(ctx context.Context, conn *sql.Conn, table, column, definition string) error {
	has, err := tableHasColumn(ctx, conn, table, column)
	if err != nil || has {
		return err
	}
	_, err = conn.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition)
	return err
}

func bindAddress(value string) (string, error) {
	if strings.Contains(value, "://") {
		return "", fmt.Errorf("CLIO_ADDR must be host:port")
	}
	if strings.HasPrefix(value, ":") {
		value = net.JoinHostPort("", strings.TrimPrefix(value, ":"))
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("CLIO_ADDR must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 0 || p > 65535 {
		return "", fmt.Errorf("CLIO_ADDR port must be between 0 and 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(p)), nil
}

func fatalf(format string, args ...any) { log.Printf(format, args...) }

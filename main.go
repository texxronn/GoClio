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

const version = "1.0.0"

type app struct {
	db        *sql.DB
	content   string
	baseURL   string
	auth      authConfig
	started   time.Time
	contentMu sync.Mutex
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
	a := &app{db: db, content: content, baseURL: baseURL, auth: auth, started: time.Now()}
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
	statements := []string{
		`CREATE TABLE IF NOT EXISTS groups_meta (name TEXT PRIMARY KEY, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS tables_meta (group_name TEXT NOT NULL, name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, timestamp_field TEXT, PRIMARY KEY(group_name,name), FOREIGN KEY(group_name) REFERENCES groups_meta(name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS fields_meta (group_name TEXT NOT NULL, table_name TEXT NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL, definition TEXT NOT NULL, PRIMARY KEY(group_name,table_name,name), FOREIGN KEY(group_name,table_name) REFERENCES tables_meta(group_name,name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS records (id TEXT PRIMARY KEY, group_name TEXT NOT NULL, table_name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, data TEXT NOT NULL, timestamp_value TEXT, FOREIGN KEY(group_name,table_name) REFERENCES tables_meta(group_name,name) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS content_page_times (path TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS records_table_idx ON records(group_name,table_name)`,
		`CREATE INDEX IF NOT EXISTS records_created_idx ON records(group_name,table_name,created_at)`,
		`CREATE INDEX IF NOT EXISTS records_timeseries_idx ON records(group_name,table_name,timestamp_value)`,
		`CREATE INDEX IF NOT EXISTS records_created_query_idx ON records(group_name,table_name,created_at COLLATE CLIO_DATETIME DESC)`,
		`CREATE INDEX IF NOT EXISTS records_timeseries_query_idx ON records(group_name,table_name,timestamp_value COLLATE CLIO_DATETIME)`,
	}
	for _, statement := range statements {
		if _, err = db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
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
	return db, nil
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

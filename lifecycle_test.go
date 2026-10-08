package main

import (
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func captureLogOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	previousWriter, previousFlags, previousPrefix := log.Writer(), log.Flags(), log.Prefix()
	buffer := &bytes.Buffer{}
	log.SetOutput(buffer)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
	return buffer
}

func TestHTTPFailureLoggingOmitsRequestDetails(t *testing.T) {
	a := newTestApp(t)
	logs := captureLogOutput(t)
	response := testRequest(t, a, http.MethodGet, "/private-records/secret?token=never-log-this", nil, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing content status = %d, want 404", response.Code)
	}
	if got := logs.String(); !strings.Contains(got, "HTTP failure: method=GET status=404") {
		t.Fatalf("HTTP failure was not logged: %q", got)
	}
	if strings.Contains(logs.String(), "private-records") || strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "never-log-this") {
		t.Fatalf("failure log contains request URL details: %q", logs.String())
	}
	beforeHealth := logs.String()
	if health := testRequest(t, a, http.MethodGet, "/health", nil, ""); health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}
	if logs.String() != beforeHealth {
		t.Fatalf("successful response generated a failure log: before=%q after=%q", beforeHealth, logs.String())
	}

	a.db = nil
	panicResponse := testRequest(t, a, http.MethodGet, "/api/v1/health?token=also-never-log-this", nil, "")
	if panicResponse.Code != http.StatusInternalServerError {
		t.Fatalf("panic response status = %d, want 500", panicResponse.Code)
	}
	if !strings.Contains(logs.String(), "request panic: method=GET") || strings.Contains(logs.String(), "also-never-log-this") {
		t.Fatalf("panic log is missing or includes request details: %q", logs.String())
	}
}

// panicAfterCommitWriter commits a status then panics on its first body write,
// so a second Write (the recovery branch appending a 500 JSON error to an
// already-committed response) is recorded rather than panicking again.
type panicAfterCommitWriter struct {
	header   http.Header
	status   int
	writes   [][]byte
	panicked bool
}

func (w *panicAfterCommitWriter) Header() http.Header { return w.header }

func (w *panicAfterCommitWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *panicAfterCommitWriter) Write(p []byte) (int, error) {
	if !w.panicked {
		w.panicked = true
		panic("handler panicked after committing the response")
	}
	w.writes = append(w.writes, append([]byte(nil), p...))
	return len(p), nil
}

func TestPanicAfterResponseCommitDoesNotAppendError(t *testing.T) {
	a := newTestApp(t)
	logs := captureLogOutput(t)

	// A handler that panics after committing a status (the body write panics
	// here) must not have a JSON error appended to the response.
	committed := &panicAfterCommitWriter{header: http.Header{}}
	a.ServeHTTP(committed, httptest.NewRequest(http.MethodGet, "/help", nil))
	if committed.status != http.StatusOK {
		t.Fatalf("committed status = %d, want 200", committed.status)
	}
	if len(committed.writes) != 0 {
		t.Fatalf("recovery appended a body after commit: %q", committed.writes)
	}
	if !strings.Contains(logs.String(), "request panic: method=GET") {
		t.Fatalf("panic was not logged: %q", logs.String())
	}

	// A panic before anything is written still yields the generic 500 JSON.
	a.db = nil
	early := testRequest(t, a, http.MethodGet, "/api/v1/health", nil, "")
	if early.Code != http.StatusInternalServerError {
		t.Fatalf("panic before write status = %d, want 500", early.Code)
	}
	var body map[string]any
	testJSON(t, early, &body)
	if body["error"] != "internal_error" || body["message"] != "An internal error occurred" {
		t.Fatalf("panic before write body = %#v", body)
	}
}

func TestServeUntilSignalGracefullyDrainsActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for test server: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "request completed")
	})}
	signals := make(chan os.Signal, 1)
	logs := captureLogOutput(t)
	serveDone := make(chan error, 1)
	go func() { serveDone <- serveUntilSignal(server, listener, signals) }()

	type clientResult struct {
		status int
		body   string
		err    error
	}
	responseDone := make(chan clientResult, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/active")
		if requestErr != nil {
			responseDone <- clientResult{err: requestErr}
			return
		}
		defer response.Body.Close()
		body, readErr := io.ReadAll(response.Body)
		responseDone <- clientResult{status: response.StatusCode, body: string(body), err: readErr}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request handler did not start")
	}
	signals <- syscall.SIGTERM
	select {
	case err := <-serveDone:
		t.Fatalf("server stopped before active request drained: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case response := <-responseDone:
		if response.err != nil || response.status != http.StatusOK || response.body != "request completed" {
			t.Fatalf("active request did not complete during shutdown: %#v", response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active request did not return")
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("graceful server shutdown returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not finish shutting down")
	}
	if !strings.Contains(logs.String(), "HTTP shutdown requested: signal=terminated") || !strings.Contains(logs.String(), "Clio shutdown complete") {
		t.Fatalf("shutdown lifecycle was not logged: %q", logs.String())
	}
}

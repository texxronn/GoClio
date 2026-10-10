package main

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second || srv.IdleTimeout != 120*time.Second || srv.MaxHeaderBytes != 64<<10 {
		t.Fatalf("server = header %v idle %v maxHeader %d", srv.ReadHeaderTimeout, srv.IdleTimeout, srv.MaxHeaderBytes)
	}
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatal("whole-request timeouts would cut off large uploads and downloads")
	}
}

func TestSlowSmallBodyIsCutOff(t *testing.T) {
	a := newTestApp(t)
	a.smallBodyTimeout = 200 * time.Millisecond
	server := httptest.NewServer(a)
	t.Cleanup(server.Close)
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Declare 20 bytes, send 5, then stall.
	_, _ = conn.Write([]byte("POST /api/v1/default/data/groups HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 20\r\n\r\n{\"nam"))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("no response before 3s: %v", err)
	}
	if !strings.Contains(status, "422") {
		t.Fatalf("status line = %q, want 422", status)
	}
}

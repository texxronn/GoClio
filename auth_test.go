package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func authLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func testPasswordHash(t *testing.T) []byte {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestLoadAuthConfigDefaultsAndValidation(t *testing.T) {
	cfg, err := loadAuthConfig(authLookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.enabled || !cfg.requireHTTPS || cfg.username != "admin" || len(cfg.trustedHTTPNetworks) != 2 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := loadAuthConfig(authLookup(map[string]string{"CLIO_AUTH_PASSWORD_HASH": "ignored-when-auth-is-disabled"})); err != nil {
		t.Fatalf("disabled auth should not require a valid password hash: %v", err)
	}

	hash := string(testPasswordHash(t))
	for _, test := range []struct {
		name   string
		values map[string]string
	}{
		{name: "invalid HTTP CIDR", values: map[string]string{"CLIO_TRUSTED_HTTP_NETWORKS": "10.0.0.1"}},
		{name: "proxy without trusted networks", values: map[string]string{"CLIO_TRUST_PROXY": "true"}},
		{name: "invalid proxy CIDR", values: map[string]string{"CLIO_TRUST_PROXY": "true", "CLIO_TRUSTED_PROXY_NETWORKS": "bad-cidr"}},
		{name: "missing password hash", values: map[string]string{"CLIO_AUTH_ENABLED": "true"}},
		{name: "invalid password hash", values: map[string]string{"CLIO_AUTH_ENABLED": "true", "CLIO_AUTH_PASSWORD_HASH": "not-a-bcrypt-hash"}},
		{name: "username contains Basic Auth delimiter", values: map[string]string{"CLIO_AUTH_ENABLED": "true", "CLIO_AUTH_PASSWORD_HASH": hash, "CLIO_AUTH_USER": "admin:other"}},
		{name: "incomplete TLS pair", values: map[string]string{"CLIO_TLS_CERT": "cert.pem"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loadAuthConfig(authLookup(test.values)); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}

	cfg, err = loadAuthConfig(authLookup(map[string]string{"CLIO_AUTH_ENABLED": "true", "CLIO_AUTH_PASSWORD_HASH": hash}))
	if err != nil || !cfg.enabled {
		t.Fatalf("valid auth config: cfg=%+v err=%v", cfg, err)
	}
	cfg, err = loadAuthConfig(authLookup(map[string]string{"CLIO_AUTH_ENABLED": "true", "CLIO_AUTH_PASSWORD_HASH": hash, "CLIO_REQUIRE_HTTPS": "false"}))
	if err != nil || cfg.requireHTTPS {
		t.Fatalf("explicit HTTP allowance: cfg=%+v err=%v", cfg, err)
	}
}

func authRequest(t *testing.T, a *app, path, remote string, credentials bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = remote
	if credentials {
		r.SetBasicAuth("admin", "secret")
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func TestAuthenticationProtectsAllApplicationRoutes(t *testing.T) {
	a := newTestApp(t)
	a.auth = authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t), requireHTTPS: true}

	var missingCredentialsBody string
	for _, route := range []string{"/api/v1/default/data/metadata", "/help", "/health", "/assets/clio-markdown.js", "/assets/clio.js", "/assets/clio/v1/clio.js", "/published.md"} {
		response := authRequest(t, a, route, "127.0.0.1:1234", false)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without credentials status=%d, want 401", route, response.Code)
		}
		if got := response.Header().Get("WWW-Authenticate"); got != `Basic realm="Clio"` {
			t.Errorf("GET %s challenge=%q", route, got)
		}
		if route == "/help" {
			missingCredentialsBody = response.Body.String()
		}
	}
	malformedQuery := httptest.NewRequest(http.MethodGet, "/help?invalid=%zz", nil)
	malformedQuery.RemoteAddr = "127.0.0.1:1234"
	malformedResponse := httptest.NewRecorder()
	a.ServeHTTP(malformedResponse, malformedQuery)
	if malformedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("malformed query bypassed auth middleware: status=%d", malformedResponse.Code)
	}

	bad := httptest.NewRequest(http.MethodGet, "/help", nil)
	bad.RemoteAddr = "127.0.0.1:1234"
	bad.SetBasicAuth("admin", "wrong")
	badResponse := httptest.NewRecorder()
	a.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusUnauthorized || badResponse.Header().Get("WWW-Authenticate") != `Basic realm="Clio"` {
		t.Fatalf("invalid credentials response: status=%d headers=%v", badResponse.Code, badResponse.Header())
	}
	if !strings.Contains(missingCredentialsBody, `"message":"Authentication required"`) || badResponse.Body.String() != missingCredentialsBody {
		t.Fatalf("authentication response revealed credential state: missing=%q invalid=%q", missingCredentialsBody, badResponse.Body.String())
	}

	for _, route := range []string{"/api/v1/default/data/metadata", "/help", "/health", "/assets/clio-markdown.js", "/assets/clio.js", "/assets/clio/v1/clio.js"} {
		response := authRequest(t, a, route, "127.0.0.1:1234", true)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s with valid credentials status=%d: %s", route, response.Code, response.Body.String())
		}
	}

	if response := authRequest(t, newTestApp(t), "/help", "203.0.113.5:1234", false); response.Code != http.StatusOK {
		t.Fatalf("auth-disabled route status=%d, want 200", response.Code)
	}
}

func TestHTTPSRequirementAndTrustedNetworks(t *testing.T) {
	cfg := authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t), requireHTTPS: true}
	trustedHTTP, err := parseNetworks("10.20.0.0/16", "trusted")
	if err != nil {
		t.Fatal(err)
	}
	cfg.trustedHTTPNetworks = trustedHTTP

	for _, test := range []struct {
		name   string
		remote string
		tls    bool
		want   bool
	}{
		{name: "direct TLS", remote: "203.0.113.5:1234", tls: true, want: true},
		{name: "localhost", remote: "127.0.0.1:1234", want: true},
		{name: "configured trusted HTTP network", remote: "10.20.4.5:1234", want: true},
		{name: "untrusted HTTP network", remote: "192.0.2.5:1234", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://clio.test/help", nil)
			r.RemoteAddr = test.remote
			if test.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := cfg.requestIsHTTPS(r); got != test.want {
				t.Fatalf("requestIsHTTPS=%t, want %t", got, test.want)
			}
		})
	}

	a := newTestApp(t)
	a.auth = cfg
	if response := authRequest(t, a, "/help", "192.0.2.5:1234", true); response.Code != http.StatusForbidden {
		t.Fatalf("untrusted HTTP status=%d, want 403", response.Code)
	}
	cfg.requireHTTPS = false
	a.auth = cfg
	if response := authRequest(t, a, "/help", "192.0.2.5:1234", true); response.Code != http.StatusOK {
		t.Fatalf("HTTP allowed when HTTPS is disabled: status=%d", response.Code)
	}
}

func TestForwardedHTTPSOnlyTrustedFromProxy(t *testing.T) {
	proxyNetworks, err := parseNetworks("10.0.0.0/8", "trusted proxy")
	if err != nil {
		t.Fatal(err)
	}
	cfg := authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t), requireHTTPS: true, trustProxy: true, trustedProxyNetworks: proxyNetworks}
	for _, test := range []struct {
		name     string
		remote   string
		protocol []string
		want     bool
	}{
		{name: "trusted proxy forwarded HTTPS", remote: "10.1.2.3:8080", protocol: []string{"https"}, want: true},
		{name: "untrusted client spoofing HTTPS", remote: "203.0.113.8:8080", protocol: []string{"https"}, want: false},
		{name: "trusted proxy forwarded HTTP", remote: "10.1.2.3:8080", protocol: []string{"http"}, want: false},
		{name: "duplicate forwarded values", remote: "10.1.2.3:8080", protocol: []string{"https", "https"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://clio.test/help", nil)
			r.RemoteAddr = test.remote
			for _, protocol := range test.protocol {
				r.Header.Add("X-Forwarded-Proto", protocol)
			}
			if got := cfg.requestIsHTTPS(r); got != test.want {
				t.Fatalf("requestIsHTTPS=%t, want %t", got, test.want)
			}
		})
	}

	a := newTestApp(t)
	a.auth = cfg
	r := httptest.NewRequest(http.MethodGet, "http://clio.test/help", nil)
	r.RemoteAddr = "10.1.2.3:8080"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("trusted proxy HTTPS status=%d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestVerifiedCredentialSkipsRepeatBcrypt(t *testing.T) {
	a := newTestApp(t)
	a.auth = authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t), requireHTTPS: false, memo: &credentialMemo{}}
	calls := 0
	original := compareCredential
	compareCredential = func(hash, password []byte) error {
		calls++
		return original(hash, password)
	}
	t.Cleanup(func() { compareCredential = original })

	for i := 0; i < 3; i++ {
		if w := authRequest(t, a, "/api/v1/health", "127.0.0.1:1", true); w.Code != http.StatusOK {
			t.Fatalf("request %d status = %d", i, w.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("bcrypt calls for 3 identical requests = %d, want 1", calls)
	}

	wrong := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	wrong.RemoteAddr = "127.0.0.1:1"
	wrong.SetBasicAuth("admin", "not-the-password")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, wrong)
	if w.Code != http.StatusUnauthorized || calls != 2 {
		t.Fatalf("wrong password: status = %d, bcrypt calls = %d; want 401 and 2", w.Code, calls)
	}
	// A failed attempt must not evict or replace the verified credential.
	if w := authRequest(t, a, "/api/v1/health", "127.0.0.1:1", true); w.Code != http.StatusOK || calls != 2 {
		t.Fatalf("after failure: status = %d, bcrypt calls = %d; want 200 and 2", w.Code, calls)
	}
}

func TestAuthWithoutMemoStillVerifies(t *testing.T) {
	a := newTestApp(t)
	a.auth = authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t)}
	if w := authRequest(t, a, "/api/v1/health", "127.0.0.1:1", true); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

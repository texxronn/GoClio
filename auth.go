package main

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

type authConfig struct {
	enabled              bool
	username             string
	passwordHash         []byte
	requireHTTPS         bool
	trustedHTTPNetworks  []*net.IPNet
	trustProxy           bool
	trustedProxyNetworks []*net.IPNet
	tlsCertificateFile   string
	tlsPrivateKeyFile    string
}

func loadAuthConfig(lookup func(string) (string, bool)) (authConfig, error) {
	value := func(key, fallback string) string {
		if configured, ok := lookup(key); ok && configured != "" {
			return configured
		}
		return fallback
	}
	parseBool := func(key string, fallback bool) (bool, error) {
		defaultValue := strconv.FormatBool(fallback)
		parsed, err := strconv.ParseBool(value(key, defaultValue))
		if err != nil {
			return false, fmt.Errorf("%s must be a boolean", key)
		}
		return parsed, nil
	}

	enabled, err := parseBool("CLIO_AUTH_ENABLED", false)
	if err != nil {
		return authConfig{}, err
	}
	requireHTTPS, err := parseBool("CLIO_REQUIRE_HTTPS", true)
	if err != nil {
		return authConfig{}, err
	}
	trustProxy, err := parseBool("CLIO_TRUST_PROXY", false)
	if err != nil {
		return authConfig{}, err
	}
	trustedHTTP, err := parseNetworks(value("CLIO_TRUSTED_HTTP_NETWORKS", "127.0.0.0/8,::1/128"), "CLIO_TRUSTED_HTTP_NETWORKS")
	if err != nil {
		return authConfig{}, err
	}
	trustedProxy, err := parseNetworks(value("CLIO_TRUSTED_PROXY_NETWORKS", ""), "CLIO_TRUSTED_PROXY_NETWORKS")
	if err != nil {
		return authConfig{}, err
	}
	if trustProxy && len(trustedProxy) == 0 {
		return authConfig{}, fmt.Errorf("CLIO_TRUST_PROXY requires CLIO_TRUSTED_PROXY_NETWORKS")
	}

	username := value("CLIO_AUTH_USER", "admin")
	passwordHash := value("CLIO_AUTH_PASSWORD_HASH", "")
	if enabled {
		if passwordHash == "" {
			return authConfig{}, fmt.Errorf("CLIO_AUTH_PASSWORD_HASH is required when CLIO_AUTH_ENABLED=true")
		}
		if username == "" || strings.Contains(username, ":") {
			return authConfig{}, fmt.Errorf("CLIO_AUTH_USER must be non-empty and must not contain a colon")
		}
		if _, err := bcrypt.Cost([]byte(passwordHash)); err != nil {
			return authConfig{}, fmt.Errorf("CLIO_AUTH_PASSWORD_HASH must be a valid bcrypt hash")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), nil); err != nil && err != bcrypt.ErrMismatchedHashAndPassword {
			return authConfig{}, fmt.Errorf("CLIO_AUTH_PASSWORD_HASH must be a valid bcrypt hash")
		}
	}

	certFile := value("CLIO_TLS_CERT", "")
	keyFile := value("CLIO_TLS_KEY", "")
	if (certFile == "") != (keyFile == "") {
		return authConfig{}, fmt.Errorf("CLIO_TLS_CERT and CLIO_TLS_KEY must be configured together")
	}
	return authConfig{
		enabled:              enabled,
		username:             username,
		passwordHash:         []byte(passwordHash),
		requireHTTPS:         requireHTTPS,
		trustedHTTPNetworks:  trustedHTTP,
		trustProxy:           trustProxy,
		trustedProxyNetworks: trustedProxy,
		tlsCertificateFile:   certFile,
		tlsPrivateKeyFile:    keyFile,
	}, nil
}

func parseNetworks(value, name string) ([]*net.IPNet, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	networks := []*net.IPNet{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("%s contains an empty network", name)
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("%s contains an invalid CIDR", name)
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func (c authConfig) requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := remoteIP(r.RemoteAddr)
	if peer == nil {
		return false
	}
	if peer.IsLoopback() || ipInNetworks(peer, c.trustedHTTPNetworks) {
		return true
	}
	if !c.trustProxy || !ipInNetworks(peer, c.trustedProxyNetworks) {
		return false
	}
	protocols := r.Header.Values("X-Forwarded-Proto")
	return len(protocols) == 1 && strings.EqualFold(strings.TrimSpace(protocols[0]), "https")
}

func remoteIP(remoteAddress string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = strings.Trim(remoteAddress, "[]")
	}
	return net.ParseIP(host)
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (a *app) authorizeRequest(w http.ResponseWriter, r *http.Request) bool {
	cfg := a.auth
	if !cfg.enabled {
		return true
	}
	if cfg.requireHTTPS && !cfg.requestIsHTTPS(r) {
		writeAPIError(w, &apiError{http.StatusForbidden, "https_required", "HTTPS is required"})
		return false
	}

	username, password, validBasic := r.BasicAuth()
	if !validBasic {
		password = ""
	}
	validPassword := bcrypt.CompareHashAndPassword(cfg.passwordHash, []byte(password)) == nil
	validUsername := subtle.ConstantTimeCompare([]byte(username), []byte(cfg.username)) == 1
	if !validBasic || !validUsername || !validPassword {
		log.Printf("authentication failure: remote=%q path=%q reason=invalid_credentials", r.RemoteAddr, r.URL.Path)
		w.Header().Set("WWW-Authenticate", `Basic realm="Clio"`)
		writeAPIError(w, &apiError{http.StatusUnauthorized, "unauthorized", "Authentication required"})
		return false
	}
	return true
}

func generatePasswordHash(stdin *os.File, stdout, stderr io.Writer) error {
	if _, err := fmt.Fprint(stderr, "Password: "); err != nil {
		return err
	}
	password, err := term.ReadPassword(int(stdin.Fd()))
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return fmt.Errorf("read password from terminal")
	}
	defer clear(password)
	if len(password) == 0 {
		return fmt.Errorf("password must not be empty")
	}
	if _, err = fmt.Fprint(stderr, "Confirm password: "); err != nil {
		return err
	}
	confirmation, err := term.ReadPassword(int(stdin.Fd()))
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return fmt.Errorf("read password confirmation from terminal")
	}
	defer clear(confirmation)
	if subtle.ConstantTimeCompare(password, confirmation) != 1 {
		return fmt.Errorf("passwords do not match")
	}
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("generate bcrypt hash")
	}
	if _, err = fmt.Fprintln(stdout, string(hash)); err != nil {
		return err
	}
	return nil
}

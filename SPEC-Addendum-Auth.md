# Clio

## SPEC-Addendum-Auth

**Status: Implementation Addendum**

This document adds optional authentication and transport-security behaviour to Clio v1.

It does not change the core data model, API model, query model, content model or deployment architecture defined in the frozen Clio specification.

Authentication remains intentionally minimal.

The goal is:

> **Provide a simple access gate without turning Clio into an identity-management system.**

---

# 1. Scope

This addendum adds:

* optional single-user authentication
* HTTP Basic Authentication
* password-hash configuration
* configurable HTTPS enforcement
* configurable trusted networks where HTTP may be permitted
* support for TLS termination by a trusted reverse proxy
* appropriate authentication responses

This addendum does **not** add:

* multiple users
* roles
* permissions
* sessions
* OAuth
* OIDC
* SSO
* JWT
* registration
* password reset
* MFA
* user administration UI
* authorization rules

---

# 2. Authentication model

Clio supports exactly one authentication model:

**HTTP Basic Authentication**

When enabled, requests must provide valid credentials.

Example:

```text
Authorization: Basic <credentials>
```

The browser and normal HTTP clients such as `curl` must be able to use Clio without special client software.

Example:

```bash
curl -u admin:password \
  https://clio.example.com/api/v1/metadata
```

---

# 3. Single-user model

Clio has exactly one configured authentication identity.

The identity consists of:

```text
username
password hash
```

There is no users table.

There are no per-user permissions.

There is no concept of an administrator versus ordinary user.

If authentication is enabled, the configured user has access to the entire Clio instance.

---

# 4. Configuration

The following configuration options are added.

```text
CLIO_AUTH_ENABLED=false

CLIO_AUTH_USER=admin

CLIO_AUTH_PASSWORD_HASH=<password hash>

CLIO_REQUIRE_HTTPS=true

CLIO_TRUSTED_HTTP_NETWORKS=127.0.0.0/8,::1/128
```

All values should be configurable through the normal Clio configuration mechanism.

Sensible defaults must exist.

---

# 5. Authentication defaults

Authentication is **disabled by default**.

When:

```text
CLIO_AUTH_ENABLED=false
```

Clio behaves exactly as specified by the core specification.

When:

```text
CLIO_AUTH_ENABLED=true
```

all HTTP endpoints require authentication except explicitly documented health endpoints if the implementation chooses to expose a minimal unauthenticated health check.

For v1, the preferred behaviour is:

> **All endpoints require authentication when authentication is enabled.**

This keeps security semantics simple.

---

# 6. Password handling

Clio must never store or require a plaintext password in persistent configuration.

`CLIO_AUTH_PASSWORD_HASH` contains a password hash produced using a modern password-hashing algorithm/library.

The implementation must use an established password-hashing mechanism.

The implementation must not invent its own password hashing algorithm.

Suitable modern password hashing mechanisms include:

* Argon2id
* bcrypt
* another established adaptive password-hashing mechanism appropriate for the Java implementation

The chosen mechanism must be documented.

Clio compares the supplied password against the stored hash.

---

# 7. Authentication failure

If authentication is enabled and credentials are missing or invalid, Clio returns:

```text
401 Unauthorized
```

and:

```text
WWW-Authenticate: Basic realm="Clio"
```

The response body should contain a simple error representation appropriate to the endpoint.

The API must not reveal whether:

* the username exists
* the password was incorrect
* the credentials were absent

---

# 8. HTTPS requirement

When authentication is enabled, Clio should require HTTPS by default.

```text
CLIO_REQUIRE_HTTPS=true
```

means:

> A request carrying authentication credentials must only be accepted over an HTTPS connection unless the request originates from a configured trusted HTTP network.

This prevents credentials from being sent over an unencrypted network by default.

---

# 9. Trusted HTTP networks

Some deployments intentionally use HTTP on a private trusted network.

Examples:

```text
localhost
private LAN
trusted container network
trusted reverse-proxy network
```

Clio therefore supports an explicit trusted-network allowlist:

```text
CLIO_TRUSTED_HTTP_NETWORKS
```

The value is a comma-separated list of CIDR networks.

Example:

```text
CLIO_TRUSTED_HTTP_NETWORKS=127.0.0.0/8,::1/128,10.10.10.0/24
```

Requests originating from one of these networks may use HTTP when:

```text
CLIO_REQUIRE_HTTPS=true
```

All other clients must use HTTPS.

An empty trusted-network list means:

> HTTPS is required for all non-local connections.

Trusted networks must be explicitly configured.

Clio must not infer that arbitrary RFC1918 addresses are trusted merely because they are private addresses.

---

# 10. Localhost

The following are considered localhost by default:

```text
127.0.0.0/8
::1/128
```

Localhost requests may use HTTP when HTTPS enforcement is enabled.

This permits:

```text
curl http://localhost:8080/...
```

while still protecting remote clients.

---

# 11. Private networks are configurable, not implicitly trusted

Private network ranges may be explicitly added to:

```text
CLIO_TRUSTED_HTTP_NETWORKS
```

For example:

```text
CLIO_TRUSTED_HTTP_NETWORKS=127.0.0.0/8,::1/128,10.10.10.0/24
```

Clio must not automatically trust:

```text
10.0.0.0/8
172.16.0.0/12
192.168.0.0/16
```

unless configured.

This prevents unexpectedly allowing unauthenticated HTTP credential transmission simply because a client happens to use an RFC1918 address.

---

# 12. HTTPS detection

Clio must determine whether a request is effectively HTTPS.

The implementation must support two deployment modes.

## 12.1 Direct HTTPS

Clio itself terminates TLS.

The connection is considered HTTPS when the Clio HTTP server receives the request over TLS.

## 12.2 Reverse-proxy TLS termination

A reverse proxy such as Caddy may terminate TLS and forward HTTP to Clio.

In this case Clio may use:

```text
X-Forwarded-Proto
```

or an equivalent standard proxy header to determine the original client protocol.

However, Clio must only trust forwarded protocol headers from configured trusted proxy networks.

---

# 13. Trusted proxies

The following optional configuration is supported:

```text
CLIO_TRUST_PROXY=false

CLIO_TRUSTED_PROXY_NETWORKS=
```

Example:

```text
CLIO_TRUST_PROXY=true

CLIO_TRUSTED_PROXY_NETWORKS=10.10.10.0/24
```

When:

```text
CLIO_TRUST_PROXY=true
```

Clio may trust the forwarded protocol information only when the immediate peer belongs to one of the configured trusted proxy networks.

If the request comes from an untrusted source, forwarded headers must be ignored for HTTPS-security decisions.

This prevents a client from simply sending:

```text
X-Forwarded-Proto: https
```

to bypass HTTPS enforcement.

---

# 14. Reverse-proxy example

A typical deployment may therefore be:

```text
Internet
   │
 HTTPS
   │
   ▼
 Caddy
   │
 HTTP/private network
   │
   ▼
 Clio
```

Example configuration:

```text
CLIO_AUTH_ENABLED=true

CLIO_AUTH_USER=admin

CLIO_AUTH_PASSWORD_HASH=<hash>

CLIO_REQUIRE_HTTPS=true

CLIO_TRUST_PROXY=true

CLIO_TRUSTED_PROXY_NETWORKS=10.10.10.0/24
```

Caddy terminates HTTPS.

Clio trusts the proxy's indication that the original connection was HTTPS.

An ordinary HTTP client connecting directly to Clio from an untrusted network is rejected.

---

# 15. HTTP rejection

When HTTPS is required but the request is not permitted because it is:

* non-HTTPS
* not localhost
* not from a configured trusted HTTP network
* not correctly identified as HTTPS through a trusted proxy

Clio returns:

```text
403 Forbidden
```

The response should clearly indicate that HTTPS is required.

Clio must not issue a redirect automatically when authentication credentials may already have been sent over the insecure connection.

The preferred behaviour is to reject the request.

---

# 16. Health endpoint

When authentication is enabled:

```text
/health
/api/v1/health
```

remain subject to the same authentication and transport-security rules as all other endpoints.

This keeps the model simple.

A deployment that requires unauthenticated health checks may place a reverse proxy or external health-check mechanism in front of Clio.

---

# 17. API clients

API clients use standard Basic Authentication.

Example:

```bash
curl \
  -u admin:password \
  https://clio.example.com/api/v1/metadata
```

The API behaves identically whether authentication is enabled or disabled, except for the authentication requirement.

The authentication layer must not change API response structures.

---

# 18. Static content

When authentication is enabled, authentication applies to:

* HTML pages
* Markdown pages
* directories
* table views
* forms
* records
* API endpoints
* static Clio assets
* Markdown renderer assets

No public/private content model exists in v1.

---

# 19. CORS

Authentication does not introduce a CORS requirement.

Clio remains a same-origin application.

Browser requests from pages served by Clio use the same origin.

Cross-origin browser access is outside the authentication addendum and is not required.

---

# 20. Brute-force protection

Clio v1 does not implement an internal account lockout system.

Protection against repeated authentication attempts is expected to be provided by:

* reverse proxy
* firewall
* network policy
* deployment environment

The implementation should avoid adding complex stateful brute-force protection to the core service.

Basic request logging is sufficient.

---

# 21. Logging

Authentication failures should be logged at an appropriate warning level.

Logs must not contain:

* plaintext passwords
* Authorization headers
* password hashes

Logs may include:

* timestamp
* source address
* request path
* authentication failure reason category

The reason category must not reveal credentials or sensitive authentication details to clients.

---

# 22. Security documentation

The README and `/help` must document:

* whether authentication is enabled
* how to configure it
* HTTPS requirements
* trusted HTTP networks
* trusted proxy behaviour
* the fact that HTTP Basic Authentication requires transport security
* the trusted nature of published HTML
* how to generate a password hash

The `/help` endpoint should describe the authentication requirements relevant to API clients, without exposing the configured password hash.

---

# 23. Configuration examples

## Open trusted LAN deployment

```text
CLIO_AUTH_ENABLED=true

CLIO_AUTH_USER=admin

CLIO_AUTH_PASSWORD_HASH=<hash>

CLIO_REQUIRE_HTTPS=true

CLIO_TRUSTED_HTTP_NETWORKS=127.0.0.0/8,::1/128,10.10.10.0/24
```

This permits HTTP from localhost and the configured private LAN.

Remote clients must use HTTPS.

## HTTPS reverse-proxy deployment

```text
CLIO_AUTH_ENABLED=true

CLIO_AUTH_USER=admin

CLIO_AUTH_PASSWORD_HASH=<hash>

CLIO_REQUIRE_HTTPS=true

CLIO_TRUST_PROXY=true

CLIO_TRUSTED_PROXY_NETWORKS=10.10.10.0/24
```

## Fully trusted isolated deployment

It is possible to disable HTTPS enforcement explicitly:

```text
CLIO_AUTH_ENABLED=true

CLIO_REQUIRE_HTTPS=false
```

This is an explicit administrator decision.

The README should warn that Basic Authentication without TLS exposes credentials to network observers.

---

# 24. Implementation constraints

The authentication implementation must remain small.

Do not introduce:

* user-management tables
* sessions
* JWT
* OAuth
* OIDC
* identity providers
* password-reset infrastructure
* authentication plugins
* authorization middleware frameworks

A small HTTP middleware/filter/interceptor is preferred.

The authentication layer should sit in front of the existing application routes.

Conceptually:

```text
HTTP request
     ↓
transport/security check
     ↓
authentication check
     ↓
existing Clio routing/API
```

The existing Clio API and domain code should not need to understand authentication state.

---

# 25. Tests

The implementation must add automated tests for:

### Authentication

* auth disabled
* auth enabled
* valid credentials
* missing credentials
* invalid credentials
* correct `WWW-Authenticate` response
* protected API
* protected HTML
* protected static assets

### HTTPS

* HTTPS accepted
* HTTP localhost accepted
* HTTP trusted network accepted
* HTTP untrusted network rejected
* HTTPS required when configured
* `CLIO_REQUIRE_HTTPS=false` allows HTTP
* forwarded HTTPS accepted from trusted proxy
* forwarded HTTPS ignored from untrusted proxy
* spoofed `X-Forwarded-Proto` cannot bypass trusted-proxy rules

### Configuration

* invalid CIDR rejected
* invalid proxy configuration rejected
* missing password hash rejected when authentication is enabled
* sensible defaults

---

# 26. Definition of done

This addendum is complete when:

* Clio can optionally require authentication
* exactly one username/password identity exists
* passwords are stored only as secure hashes
* Basic Authentication works with browsers and API clients
* HTTPS enforcement is configurable
* trusted HTTP networks are configurable
* localhost is supported
* trusted reverse proxies are supported
* forwarded HTTPS information cannot be spoofed by untrusted clients
* authentication applies consistently to the Clio application
* authentication does not change the API/domain model
* no user/role/authorization subsystem has been introduced
* automated tests cover authentication and transport-security rules

---

# 27. Product boundary

The purpose of this addendum is:

> **Protect Clio without turning Clio into an identity-management product.**

The resulting model should remain understandable as:

```text
Clio
 │
 ├── optional Basic Auth
 │
 ├── HTTPS policy
 │
 └── existing Clio application
```

Nothing beyond this is required for v1.

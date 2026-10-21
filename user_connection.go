// Copyright 2025 bluefunda
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// Principal headers sent by cai-llm-router (its internal/mcp/principal.go).
// Duplicated, not imported — they are the wire contract; treat as frozen.
const headerPrincipalUserID = "X-BF-User-Id"

const (
	// maxDiscoveredServices caps how many services a connection with no
	// service list loads from the gateway catalog.
	maxDiscoveredServices = 20
	// maxMetadataBytes caps one $metadata document.
	maxMetadataBytes = 20 << 20
	// sessionLoadTimeout bounds fetching a user's metadata for a new session.
	sessionLoadTimeout = 90 * time.Second
	// userCacheTTL is how long a user's loaded metadata is reused across
	// sessions (each chat turn opens a new one) before it is fetched again.
	userCacheTTL    = 10 * time.Minute
	userCacheMaxLen = 256
)

// UserConnection is one user's own OData system, as stored by cai-bff
// (internal/integrations/odatacreds).
type UserConnection struct {
	Host     string   `json:"host"`
	Client   string   `json:"client"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	Services []string `json:"services"`
}

// errODataNotConnected means cai-bff has no connection stored for the user —
// distinct from a lookup failure so the user gets a "connect first" message.
var errODataNotConnected = errors.New("OData is not connected for this user")

// fetchUserConnection calls cai-bff's internal lookup for userID.
func fetchUserConnection(ctx context.Context, bffURL, internalSecret, userID string) (*UserConnection, error) {
	target := strings.TrimRight(bffURL, "/") + "/internal/odata/credentials?userID=" + url.QueryEscape(userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("building odata credentials request: %w", err)
	}
	if internalSecret != "" {
		req.Header.Set("X-Internal-Secret", internalSecret)
	}

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("odata credentials request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errODataNotConnected
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading odata credentials response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("odata credentials lookup returned %d", resp.StatusCode)
	}

	var conn UserConnection
	if err := json.Unmarshal(body, &conn); err != nil {
		return nil, fmt.Errorf("parsing odata credentials response: %w", err)
	}
	return &conn, nil
}

// serviceURL resolves one configured service to its base URL on the user's
// host. A service may be:
//   - a name, e.g. API_BUSINESS_PARTNER → <host>/sap/opu/odata/sap/<name>
//   - a path, e.g. /sap/opu/odata4/sap/api_x/srvd_a2x/sap/x/0001 → <host><path>
//   - a full URL, which must be on the same host as the connection: the
//     user's password is only ever sent to the host they connected.
func serviceURL(host, service string) (string, error) {
	base, err := url.Parse(strings.TrimRight(host, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return "", fmt.Errorf("invalid host %q", host)
	}
	service = strings.TrimSuffix(strings.TrimSpace(service), "/$metadata")
	service = strings.TrimRight(service, "/")

	switch {
	case service == "":
		return "", errors.New("empty service")
	case strings.HasPrefix(service, "http://") || strings.HasPrefix(service, "https://"):
		u, err := url.Parse(service)
		if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
			return "", fmt.Errorf("service %q is not on the connected host %s", service, base.Host)
		}
		u.RawQuery, u.Fragment = "", ""
		u.Path = strings.TrimRight(u.Path, "/")
		return u.String(), nil
	case strings.HasPrefix(service, "/"):
		return base.Scheme + "://" + base.Host + service, nil
	case strings.ContainsAny(service, "/?#"):
		return "", fmt.Errorf("invalid service name %q", service)
	default:
		return base.Scheme + "://" + base.Host + "/sap/opu/odata/sap/" + service, nil
	}
}

// publicOnlyDialer refuses connections to loopback, private, link-local and
// other non-public addresses. Checked on the resolved IP at dial time, so a
// hostname that resolves (or rebinds) to an internal address is refused too.
func publicOnlyDialer() *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !isPublicIP(ip) {
				return fmt.Errorf("connections to %s are not allowed", host)
			}
			return nil
		},
	}
}

var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isPublicIP(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() &&
		!ip.IsInterfaceLocalMulticast() && !carrierGradeNAT.Contains(ip)
}

// userHTTPClient is the client for everything sent to a user's system: no
// redirects (the password must not follow one elsewhere), no proxy and, unless
// allowPrivate, public addresses only.
func userHTTPClient(allowPrivate bool) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	}
	if !allowPrivate {
		transport.DialContext = publicOnlyDialer().DialContext
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// remoteStore is a MetadataStore reading $metadata live from a user's own
// system. IDs are service base URLs; BaseURLFor returns the ID itself.
type remoteStore struct {
	conn     *UserConnection
	services []string
	http     *http.Client
	// firstErr is the first $metadata fetch failure. load() skips a file it
	// can't read, so this is what tells the user why nothing loaded.
	firstErr error
}

func (s *remoteStore) ListXMLFiles(context.Context) ([]string, error) { return s.services, nil }

func (s *remoteStore) BaseURLFor(id string) string { return id }

func (s *remoteStore) GetXMLContent(ctx context.Context, id string) (string, error) {
	body, err := s.get(ctx, id+"/$metadata", "application/xml", nil)
	if err != nil && s.firstErr == nil {
		s.firstErr = err
	}
	return string(body), err
}

// get sends an authenticated GET to the user's system.
func (s *remoteStore) get(ctx context.Context, target, accept string, query url.Values) ([]byte, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, vs := range query {
		q[k] = vs
	}
	if s.conn.Client != "" {
		q.Set("sap-client", s.conn.Client)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.conn.Username, s.conn.Password)
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "OData-MCP-Client/1.0")

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", u.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// SAP explains a failure in the body; keep a short excerpt for the user.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := statusError(u.Path, resp.StatusCode)
		if msg := sapErrorMessage(snippet); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return nil, &httpStatusError{status: resp.StatusCode, err: err}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxMetadataBytes {
		return nil, fmt.Errorf("%s is larger than %d MB", u.Path, maxMetadataBytes>>20)
	}
	return body, nil
}

// httpStatusError keeps the HTTP status of a failed request to the user's
// system, so callers can tell "this service isn't there" from "nothing works".
type httpStatusError struct {
	status int
	err    error
}

func (e *httpStatusError) Error() string { return e.err.Error() }
func (e *httpStatusError) Unwrap() error { return e.err }

// statusError explains a non-200 answer from the user's system.
func statusError(path string, status int) error {
	switch {
	case status == http.StatusUnauthorized:
		return errors.New("the system rejected the username or password — update the OData connection in Settings")
	case status == http.StatusForbidden:
		return fmt.Errorf("your user isn't authorized for %s", path)
	case status == http.StatusNotFound:
		return fmt.Errorf("service not found: %s", path)
	case status >= 300 && status < 400:
		return fmt.Errorf("%s redirected (HTTP %d) — check the host in the OData connection", path, status)
	default:
		return fmt.Errorf("%s returned HTTP %d", path, status)
	}
}

// sapErrorMessage pulls the message out of an OData error body — JSON
// {"error":{"message":{"value":...}}} (v2) or {"error":{"message":...}} (v4),
// or XML <message>...</message> — capped to a short line.
func sapErrorMessage(body []byte) string {
	var msg string
	var v struct {
		Error struct {
			Message json.RawMessage `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &v) == nil && len(v.Error.Message) > 0 {
		var nested struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(v.Error.Message, &nested) == nil && nested.Value != "" {
			msg = nested.Value
		} else {
			_ = json.Unmarshal(v.Error.Message, &msg)
		}
	} else if start := strings.Index(string(body), "<message"); start >= 0 {
		rest := string(body)[start:]
		if gt := strings.Index(rest, ">"); gt >= 0 {
			if end := strings.Index(rest, "</message>"); end > gt {
				msg = rest[gt+1 : end]
			}
		}
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}

// discoverServices lists services from the SAP Gateway catalog, for a
// connection that names none. Only each entry's path is used, on the user's
// host — the catalog's own host may be an internal name.
func (s *remoteStore) discoverServices(ctx context.Context) ([]string, error) {
	catalog := strings.TrimRight(s.conn.Host, "/") + "/sap/opu/odata/IWFND/CATALOGSERVICE;v=2/ServiceCollection"
	body, err := s.get(ctx, catalog, "application/json", url.Values{
		"$format": {"json"},
		"$select": {"ServiceUrl"},
		"$top":    {fmt.Sprint(maxDiscoveredServices)},
	})
	if err != nil {
		// Some gateways reject $select/$top on the catalog; retry plainly.
		body, err = s.get(ctx, catalog, "application/json", url.Values{"$format": {"json"}})
	}
	if err != nil {
		err = fmt.Errorf("listing services from the gateway catalog: %w", err)
		// Naming the services helps only when the catalog itself is missing or closed
		// to this user — not when the system rejects every request (e.g. a license
		// or logon error), where the services would fail the same way.
		var status *httpStatusError
		if errors.As(err, &status) && (status.status == http.StatusNotFound || status.status == http.StatusForbidden) {
			err = fmt.Errorf("%w — add the service names to the OData connection instead", err)
		}
		return nil, err
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gateway catalog response is not JSON: %w", err)
	}
	var services []string
	for _, rec := range extractRecords(raw) {
		m, _ := rec.(map[string]any)
		u, err := url.Parse(configString(m, "ServiceUrl"))
		if err != nil || u.Path == "" {
			continue
		}
		services = append(services, u.Path)
		if len(services) == maxDiscoveredServices {
			break
		}
	}
	return services, nil
}

// loadUserHandlers builds Handlers for one user's own connection.
func loadUserHandlers(ctx context.Context, cfg *Config, conn *UserConnection) (*Handlers, error) {
	store := &remoteStore{conn: conn, http: userHTTPClient(cfg.AllowPrivateHosts)}

	names := conn.Services
	if len(names) == 0 {
		discovered, err := store.discoverServices(ctx)
		if err != nil {
			return nil, err
		}
		if len(discovered) == 0 {
			return nil, errors.New("the gateway catalog lists no services — add the service names to the OData connection")
		}
		names = discovered
	}

	seen := make(map[string]bool, len(names))
	for _, name := range names {
		u, err := serviceURL(conn.Host, name)
		if err != nil {
			return nil, err
		}
		if !seen[u] {
			seen[u] = true
			store.services = append(store.services, u)
		}
	}

	var query url.Values
	if conn.Client != "" {
		query = url.Values{"sap-client": {conn.Client}}
	}
	h := NewHandlers(cfg)
	err := h.load(ctx, store, func(baseURL string) *ODataClient {
		c := newODataClient(baseURL, conn.Username, conn.Password, "basic")
		c.http = store.http
		c.query = query
		return c
	})
	if err != nil || len(h.services) == 0 {
		if store.firstErr != nil {
			return nil, store.firstErr
		}
		if err != nil {
			return nil, err
		}
		return nil, errors.New("none of the connection's services could be loaded")
	}
	return h, nil
}

// userCache reuses a user's loaded metadata across sessions. The key covers
// the whole connection, so a changed password or service list reloads.
type userCache struct {
	mu      sync.Mutex
	entries map[string]userCacheEntry
}

type userCacheEntry struct {
	h       *Handlers
	expires time.Time
}

func newUserCache() *userCache { return &userCache{entries: make(map[string]userCacheEntry)} }

func userCacheKey(userID string, conn *UserConnection) string {
	sum := sha256.New()
	for _, part := range append([]string{userID, conn.Host, conn.Client, conn.Username, conn.Password}, conn.Services...) {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func (c *userCache) get(key string) *Handlers {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.expires) {
		return e.h
	}
	return nil
}

func (c *userCache) put(key string, h *Handlers) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= userCacheMaxLen {
		var oldest string
		for k, e := range c.entries {
			if oldest == "" || e.expires.Before(c.entries[oldest].expires) {
				oldest = k
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = userCacheEntry{h: h, expires: now.Add(userCacheTTL)}
}

// sessionServers builds the MCP server for each new SSE/HTTP session.
type sessionServers struct {
	cfg    *Config
	shared *Handlers
	cache  *userCache
}

// serverFor returns the server for the session opened by req. cai-llm-router
// builds a new MCP client per chat request with the caller's principal on
// every request, so a session belongs to one user for its whole life.
//
//   - Per-user connections off, or no X-BF-User-Id: the shared server.
//   - The user has a connection: a server over their own system.
//   - No connection, or it can't be loaded: a server whose tools explain what
//     to fix. Never the shared system — a user must not get answers from a
//     system they didn't connect.
func (s *sessionServers) serverFor(req *http.Request) *mcp.Server {
	userID := req.Header.Get(headerPrincipalUserID)
	if userID == "" || s.cfg.CAIBFFInternalURL == "" {
		return buildServer(s.shared)
	}

	ctx, cancel := context.WithTimeout(req.Context(), sessionLoadTimeout)
	defer cancel()

	conn, err := fetchUserConnection(ctx, s.cfg.CAIBFFInternalURL, s.cfg.InternalServiceSecret, userID)
	switch {
	case errors.Is(err, errODataNotConnected):
		logger.L.Info("Session's user has not connected OData", zap.String("userID", userID))
		return buildServer(errorHandlers(s.cfg, errors.New("OData is not connected — connect SAP Analytics in Settings first")))
	case err != nil:
		logger.L.Error("OData connection lookup failed", zap.String("userID", userID), zap.Error(err))
		return buildServer(errorHandlers(s.cfg, errors.New("couldn't look up your OData connection, please try again")))
	}

	key := userCacheKey(userID, conn)
	if h := s.cache.get(key); h != nil {
		return buildServer(h)
	}

	start := time.Now()
	h, err := loadUserHandlers(ctx, s.cfg, conn)
	if err != nil {
		logger.L.Warn("Loading user's OData services failed", zap.String("userID", userID), zap.Error(err))
		return buildServer(errorHandlers(s.cfg, fmt.Errorf("couldn't load your OData services: %w", err)))
	}
	logger.L.Info("Loaded user's OData services",
		zap.String("userID", userID),
		zap.Int("services", len(h.services)),
		zap.Int("entities", len(h.allEntityNames)),
		zap.Duration("duration", time.Since(start)),
	)
	s.cache.put(key, h)
	return buildServer(h)
}

// errorHandlers is a Handlers whose every data tool answers with err.
func errorHandlers(cfg *Config, err error) *Handlers {
	h := NewHandlers(cfg)
	h.sessionErr = err
	return h
}

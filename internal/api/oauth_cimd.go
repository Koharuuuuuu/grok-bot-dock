package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
)

const (
	cimdMaxDocumentBytes = 64 << 10
	cimdMaxRedirects     = 16
	cimdMaxCacheEntries  = 512
	cimdMaxCacheLifetime = 5 * time.Minute
)

type cimdCacheEntry struct {
	client  store.OAuthClient
	expires time.Time
}

type cimdDocument struct {
	ClientID                string          `json:"client_id"`
	ClientName              string          `json:"client_name"`
	RedirectURIs            []string        `json:"redirect_uris"`
	TokenEndpointAuthMethod string          `json:"token_endpoint_auth_method"`
	ClientSecret            json.RawMessage `json:"client_secret"`
	ClientSecretExpiresAt   json.RawMessage `json:"client_secret_expires_at"`
	GrantTypes              []string        `json:"grant_types"`
	ResponseTypes           []string        `json:"response_types"`
}

func validCIMDClientID(raw string) bool {
	if raw == "" || len(raw) > 2048 || raw != strings.TrimSpace(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return false
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func isPublicMetadataIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip.Is4() {
		for _, raw := range []string{
			"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
			"192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		} {
			if netip.MustParsePrefix(raw).Contains(ip) {
				return false
			}
		}
		return true
	}
	if !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, raw := range []string{"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}

func cimdHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialCIMDHost(ctx, network, address, net.DefaultResolver.LookupNetIP, dialer.DialContext)
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type metadataLookup func(context.Context, string, string) ([]netip.Addr, error)
type metadataDial func(context.Context, string, string) (net.Conn, error)

func dialCIMDHost(ctx context.Context, network, address string, lookup metadataLookup, dial metadataDial) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("client metadata host resolution failed")
	}
	for _, ip := range ips {
		if !isPublicMetadataIP(ip) {
			return nil, errors.New("client metadata host resolves to a non-public address")
		}
	}
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func (s *Server) resolveOAuthAuthorizationClient(ctx context.Context, clientID string) (store.OAuthClient, error) {
	client, err := s.store.OAuthClient(ctx, clientID)
	if err == nil {
		if client.DisabledAt != "" || client.Registration != "cimd" {
			return client, nil
		}
		client, err = s.fetchCIMDClient(ctx, clientID)
		if err != nil {
			return store.OAuthClient{}, err
		}
		if err = s.store.UpsertOAuthMetadataClient(ctx, client.ID, client.Name, client.Redirects); err != nil {
			return store.OAuthClient{}, err
		}
		return client, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.OAuthClient{}, err
	}
	if !validCIMDClientID(clientID) {
		return store.OAuthClient{}, err
	}
	return s.fetchCIMDClient(ctx, clientID)
}

func (s *Server) fetchCIMDClient(ctx context.Context, clientID string) (store.OAuthClient, error) {
	if !validCIMDClientID(clientID) {
		return store.OAuthClient{}, errors.New("invalid CIMD client identifier")
	}
	now := time.Now()
	s.cimdMu.Lock()
	if entry, ok := s.cimdCache[clientID]; ok && now.Before(entry.expires) {
		s.cimdMu.Unlock()
		return entry.client, nil
	}
	delete(s.cimdCache, clientID)
	s.cimdMu.Unlock()

	select {
	case s.cimdFetch <- struct{}{}:
		defer func() { <-s.cimdFetch }()
	case <-ctx.Done():
		return store.OAuthClient{}, ctx.Err()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	metadata, lifetime, err := s.fetchCIMD(requestCtx, clientID)
	if err != nil {
		return store.OAuthClient{}, err
	}
	client := store.OAuthClient{ID: metadata.ClientID, Name: metadata.ClientName, Redirects: metadata.RedirectURIs, Registration: "cimd"}
	if lifetime > 0 {
		s.cimdMu.Lock()
		if len(s.cimdCache) >= cimdMaxCacheEntries {
			for key, entry := range s.cimdCache {
				if !now.Before(entry.expires) {
					delete(s.cimdCache, key)
				}
			}
			if len(s.cimdCache) >= cimdMaxCacheEntries {
				for key := range s.cimdCache {
					delete(s.cimdCache, key)
					break
				}
			}
		}
		s.cimdCache[clientID] = cimdCacheEntry{client: client, expires: now.Add(lifetime)}
		s.cimdMu.Unlock()
	}
	return client, nil
}

func fetchCIMDMetadata(ctx context.Context, clientID string) (cimdDocument, time.Duration, error) {
	client := cimdHTTPClient(5 * time.Second)
	defer client.CloseIdleConnections()
	return fetchCIMDMetadataWithHTTPClient(ctx, clientID, client)
}

func fetchCIMDMetadataWithHTTPClient(ctx context.Context, clientID string, client *http.Client) (cimdDocument, time.Duration, error) {
	if !validCIMDClientID(clientID) {
		return cimdDocument{}, 0, errors.New("invalid CIMD client identifier")
	}
	u, _ := url.Parse(clientID)
	if host := u.Hostname(); host != "" {
		if ip, err := netip.ParseAddr(host); err == nil && !isPublicMetadataIP(ip) {
			return cimdDocument{}, 0, errors.New("CIMD client identifier resolves to a non-public address")
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return cimdDocument{}, 0, err
	}
	request.Header.Set("Accept", "application/json, application/*+json")
	response, err := client.Do(request)
	if err != nil {
		return cimdDocument{}, 0, errors.New("CIMD metadata fetch failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return cimdDocument{}, 0, fmt.Errorf("CIMD metadata endpoint returned HTTP %d", response.StatusCode)
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (contentType != "application/json" && !(strings.HasPrefix(contentType, "application/") && strings.HasSuffix(contentType, "+json"))) {
		return cimdDocument{}, 0, errors.New("CIMD metadata response must be JSON")
	}
	if response.ContentLength > cimdMaxDocumentBytes {
		return cimdDocument{}, 0, errors.New("CIMD metadata response is too large")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cimdMaxDocumentBytes+1))
	if err != nil || len(body) > cimdMaxDocumentBytes {
		return cimdDocument{}, 0, errors.New("CIMD metadata response is invalid or too large")
	}
	var metadata cimdDocument
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err = decoder.Decode(&metadata); err != nil {
		return cimdDocument{}, 0, errors.New("CIMD metadata response is invalid JSON")
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return cimdDocument{}, 0, errors.New("CIMD metadata response contains trailing data")
	}
	if err = validateCIMDMetadata(clientID, metadata); err != nil {
		return cimdDocument{}, 0, err
	}
	return metadata, parseCIMDCacheLifetime(response.Header), nil
}

func validateCIMDMetadata(clientID string, metadata cimdDocument) error {
	if metadata.ClientID != clientID || strings.TrimSpace(metadata.ClientName) == "" || len(metadata.ClientName) > 128 {
		return errors.New("CIMD metadata identity or client_name is invalid")
	}
	if metadata.TokenEndpointAuthMethod != "none" || len(metadata.ClientSecret) > 0 || len(metadata.ClientSecretExpiresAt) > 0 {
		return errors.New("CIMD metadata requests an unsupported client authentication method")
	}
	if len(metadata.RedirectURIs) == 0 || len(metadata.RedirectURIs) > cimdMaxRedirects {
		return errors.New("CIMD metadata redirect_uris are invalid")
	}
	for _, redirect := range metadata.RedirectURIs {
		if len(redirect) > 2048 || !validOAuthRedirectURI(redirect) {
			return errors.New("CIMD metadata contains an invalid redirect URI")
		}
	}
	if len(metadata.GrantTypes) > 0 && !containsExact(metadata.GrantTypes, "authorization_code") {
		return errors.New("CIMD metadata does not support authorization_code")
	}
	if len(metadata.ResponseTypes) > 0 && !containsExact(metadata.ResponseTypes, "code") {
		return errors.New("CIMD metadata does not support code responses")
	}
	return nil
}

func parseCIMDCacheLifetime(header http.Header) time.Duration {
	var maxAge int64 = -1
	for _, value := range header.Values("Cache-Control") {
		for _, raw := range strings.Split(value, ",") {
			parts := strings.SplitN(strings.TrimSpace(raw), "=", 2)
			directive := strings.ToLower(parts[0])
			if directive == "no-store" || directive == "no-cache" {
				return 0
			}
			if directive == "max-age" && len(parts) == 2 {
				seconds, err := strconv.ParseInt(strings.Trim(parts[1], `"`), 10, 64)
				if err != nil || seconds <= 0 {
					return 0
				}
				maxAge = seconds
			}
		}
	}
	if maxAge <= 0 {
		return 0
	}
	if maxAge > int64(cimdMaxCacheLifetime/time.Second) {
		return cimdMaxCacheLifetime
	}
	return time.Duration(maxAge) * time.Second
}

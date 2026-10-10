package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type OAuthClient struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	Redirects    []string `json:"redirect_uris"`
	Registration string   `json:"registration_type"`
	Confidential bool     `json:"confidential"`
	SecretHash   string   `json:"-"`
	CreatedAt    string   `json:"created_at"`
	DisabledAt   string   `json:"disabled_at,omitempty"`
}

type OAuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	Resource    string
	Scopes      []string
	ExpiresAt   time.Time
}

type OAuthCodeBinding struct {
	ClientID    string
	RedirectURI string
	Resource    string
}

type OAuthGrant struct {
	ID        int64    `json:"id"`
	ClientID  string   `json:"client_id"`
	Name      string   `json:"client_name"`
	Resource  string   `json:"resource"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
	ExpiresAt string   `json:"expires_at"`
	RevokedAt string   `json:"revoked_at,omitempty"`
}

func (s *Store) migrateOAuth(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS oauth_clients (client_id TEXT PRIMARY KEY, name TEXT NOT NULL, redirect_uris_json TEXT NOT NULL, client_secret_hash TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, disabled_at TEXT NOT NULL DEFAULT '', registration_type TEXT NOT NULL DEFAULT 'manual')`,
		`CREATE TABLE IF NOT EXISTS oauth_grants (id INTEGER PRIMARY KEY, owner_subject TEXT NOT NULL, client_id TEXT NOT NULL REFERENCES oauth_clients(client_id), resource TEXT NOT NULL, scopes_json TEXT NOT NULL, created_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS oauth_grants_client_idx ON oauth_grants(client_id, revoked_at)`,
		`CREATE TABLE IF NOT EXISTS oauth_authorization_codes (code_hash TEXT PRIMARY KEY, grant_id INTEGER NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE, client_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, pkce_challenge TEXT NOT NULL, resource TEXT NOT NULL, scopes_json TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS oauth_codes_expiry_idx ON oauth_authorization_codes(expires_at)`,
		`CREATE TABLE IF NOT EXISTS oauth_access_tokens (token_hash TEXT PRIMARY KEY, grant_id INTEGER NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE, client_id TEXT NOT NULL, resource TEXT NOT NULL, scopes_json TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS oauth_tokens_grant_idx ON oauth_access_tokens(grant_id, revoked_at)`,
		`CREATE INDEX IF NOT EXISTS oauth_tokens_expiry_idx ON oauth_access_tokens(expires_at)`,
		`CREATE TABLE IF NOT EXISTS oauth_audit_events (id INTEGER PRIMARY KEY, event_at TEXT NOT NULL, event_type TEXT NOT NULL, client_id TEXT NOT NULL DEFAULT '', result TEXT NOT NULL, subject_ref TEXT NOT NULL DEFAULT '')`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(2, datetime('now'))`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var hasRegistrationType bool
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(oauth_clients)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &columnType, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "registration_type" {
			hasRegistrationType = true
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if !hasRegistrationType {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE oauth_clients ADD COLUMN registration_type TEXT NOT NULL DEFAULT 'manual'`); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(3, datetime('now'))`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RegisterOAuthClient(ctx context.Context, id, name string, redirects []string, secretHash string) error {
	encoded, err := json.Marshal(redirects)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO oauth_clients(client_id,name,redirect_uris_json,client_secret_hash,created_at) VALUES(?,?,?,?,?)`, id, name, string(encoded), secretHash, time.Now().UTC().Format(timestampFormat))
	return err
}

func (s *Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	var c OAuthClient
	var redirects string
	err := s.db.QueryRowContext(ctx, `SELECT client_id,name,redirect_uris_json,client_secret_hash,created_at,disabled_at,registration_type FROM oauth_clients WHERE client_id=?`, id).Scan(&c.ID, &c.Name, &redirects, &c.SecretHash, &c.CreatedAt, &c.DisabledAt, &c.Registration)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(redirects), &c.Redirects)
	return c, err
}

func (s *Store) OAuthClients(ctx context.Context) ([]OAuthClient, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT client_id,name,redirect_uris_json,client_secret_hash,created_at,disabled_at,registration_type FROM oauth_clients ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clients := []OAuthClient{}
	for rows.Next() {
		var c OAuthClient
		var redirects string
		if err := rows.Scan(&c.ID, &c.Name, &redirects, &c.SecretHash, &c.CreatedAt, &c.DisabledAt, &c.Registration); err != nil {
			return nil, err
		}
		c.Confidential = c.SecretHash != ""
		c.SecretHash = ""
		if err := json.Unmarshal([]byte(redirects), &c.Redirects); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

// UpsertOAuthMetadataClient stores validated public-client metadata discovered
// from a CIMD URL. A material change invalidates existing grants and tokens so
// consent is obtained again for the changed identity or redirect set.
func (s *Store) UpsertOAuthMetadataClient(ctx context.Context, id, name string, redirects []string) error {
	encoded, err := json.Marshal(redirects)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(timestampFormat)
	var oldName, oldRedirects, registration, disabledAt string
	err = tx.QueryRowContext(ctx, `SELECT name,redirect_uris_json,registration_type,disabled_at FROM oauth_clients WHERE client_id=?`, id).Scan(&oldName, &oldRedirects, &registration, &disabledAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO oauth_clients(client_id,name,redirect_uris_json,client_secret_hash,created_at,registration_type) VALUES(?,?,?,'',?,'cimd')`, id, name, string(encoded), now)
	case err != nil:
		return err
	case registration != "cimd":
		return ErrOAuthClientTypeConflict
	case disabledAt != "":
		return ErrOAuthDisabled
	default:
		changed := oldName != name || oldRedirects != string(encoded)
		_, err = tx.ExecContext(ctx, `UPDATE oauth_clients SET name=?,redirect_uris_json=? WHERE client_id=? AND registration_type='cimd' AND disabled_at=''`, name, string(encoded), id)
		if err == nil && changed {
			if _, err = tx.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at=? WHERE client_id=? AND revoked_at=''`, now, id); err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE oauth_access_tokens SET revoked_at=? WHERE client_id=? AND revoked_at=''`, now, id)
			}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RotateOAuthClientSecret(ctx context.Context, id, secretHash string) error {
	if secretHash == "" {
		return errors.New("empty OAuth client secret hash")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE oauth_clients SET client_secret_hash=? WHERE client_id=? AND client_secret_hash<>'' AND disabled_at=''`, secretHash, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		var secretHash string
		var disabledAt string
		err = s.db.QueryRowContext(ctx, `SELECT 1,client_secret_hash,disabled_at FROM oauth_clients WHERE client_id=?`, id).Scan(&exists, &secretHash, &disabledAt)
		if err != nil {
			return err
		}
		if disabledAt != "" {
			return ErrOAuthDisabled
		}
		if secretHash == "" {
			return ErrOAuthPublicClient
		}
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DisableOAuthClient(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(timestampFormat)
	res, err := tx.ExecContext(ctx, `UPDATE oauth_clients SET disabled_at=? WHERE client_id=? AND disabled_at=''`, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at=? WHERE client_id=? AND revoked_at=''`, now, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_access_tokens SET revoked_at=? WHERE client_id=? AND revoked_at=''`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateOAuthCode(ctx context.Context, codeHash string, code OAuthCode, grantExpiry time.Time) (int64, error) {
	encoded, _ := json.Marshal(code.Scopes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(timestampFormat)
	if _, err = tx.ExecContext(ctx, `DELETE FROM oauth_authorization_codes WHERE expires_at<?`, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM oauth_access_tokens WHERE expires_at<? OR revoked_at<>''`, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE expires_at<?`, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM oauth_audit_events WHERE event_at<?`, time.Now().UTC().Add(-90*24*time.Hour).Format(timestampFormat)); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO oauth_grants(owner_subject,client_id,resource,scopes_json,created_at,expires_at) SELECT 'owner',client_id,?,?,?,? FROM oauth_clients WHERE client_id=? AND disabled_at=''`, code.Resource, string(encoded), now, grantExpiry.UTC().Format(timestampFormat), code.ClientID)
	if err != nil {
		return 0, err
	}
	grantID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_authorization_codes(code_hash,grant_id,client_id,redirect_uri,pkce_challenge,resource,scopes_json,expires_at) VALUES(?,?,?,?,?,?,?,?)`, codeHash, grantID, code.ClientID, code.RedirectURI, code.Challenge, code.Resource, string(encoded), code.ExpiresAt.UTC().Format(timestampFormat))
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return grantID, nil
}

func (s *Store) ExchangeOAuthCode(ctx context.Context, codeHash string, binding OAuthCodeBinding, tokenHash string, tokenExpiry time.Time) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var grantID int64
	var scopesJSON string
	// OAuth 2.1 token requests may omit redirect_uri. When supplied, it must
	// still match the URI bound to the authorization code.
	err = tx.QueryRowContext(ctx, `SELECT c.grant_id,c.scopes_json FROM oauth_authorization_codes c JOIN oauth_grants g ON g.id=c.grant_id JOIN oauth_clients cl ON cl.client_id=c.client_id WHERE c.code_hash=? AND c.client_id=? AND (?='' OR c.redirect_uri=?) AND c.resource=? AND c.consumed_at='' AND c.expires_at>? AND g.revoked_at='' AND cl.disabled_at=''`, codeHash, binding.ClientID, binding.RedirectURI, binding.RedirectURI, binding.Resource, time.Now().UTC().Format(timestampFormat)).Scan(&grantID, &scopesJSON)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(timestampFormat)
	res, err := tx.ExecContext(ctx, `UPDATE oauth_authorization_codes SET consumed_at=? WHERE code_hash=? AND consumed_at='' AND expires_at>?`, now, codeHash, now)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, sql.ErrNoRows
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_access_tokens(token_hash,grant_id,client_id,resource,scopes_json,expires_at) VALUES(?,?,?,?,?,?)`, tokenHash, grantID, binding.ClientID, binding.Resource, scopesJSON, tokenExpiry.UTC().Format(timestampFormat))
	if err != nil {
		return nil, err
	}
	var scopes []string
	if err = json.Unmarshal([]byte(scopesJSON), &scopes); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return scopes, nil
}

func (s *Store) CheckOAuthCode(ctx context.Context, codeHash string, binding OAuthCodeBinding, challenge string) bool {
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT c.pkce_challenge FROM oauth_authorization_codes c JOIN oauth_grants g ON g.id=c.grant_id JOIN oauth_clients cl ON cl.client_id=c.client_id WHERE c.code_hash=? AND c.client_id=? AND (?='' OR c.redirect_uri=?) AND c.resource=? AND c.consumed_at='' AND c.expires_at>? AND g.revoked_at='' AND cl.disabled_at=''`, codeHash, binding.ClientID, binding.RedirectURI, binding.RedirectURI, binding.Resource, time.Now().UTC().Format(timestampFormat)).Scan(&found)
	return err == nil && found == challenge
}

func (s *Store) OAuthToken(ctx context.Context, tokenHash, resource string) ([]string, error) {
	var raw string
	now := time.Now().UTC().Format(timestampFormat)
	err := s.db.QueryRowContext(ctx, `SELECT t.scopes_json FROM oauth_access_tokens t JOIN oauth_grants g ON g.id=t.grant_id JOIN oauth_clients c ON c.client_id=t.client_id WHERE t.token_hash=? AND t.resource=? AND t.expires_at>? AND g.expires_at>? AND t.revoked_at='' AND g.revoked_at='' AND c.disabled_at=''`, tokenHash, resource, now, now).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var scopes []string
	err = json.Unmarshal([]byte(raw), &scopes)
	return scopes, err
}

func (s *Store) RevokeOAuthToken(ctx context.Context, tokenHash, clientID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oauth_access_tokens SET revoked_at=? WHERE token_hash=? AND client_id=? AND revoked_at=''`, time.Now().UTC().Format(timestampFormat), tokenHash, clientID)
	return err
}

func (s *Store) OAuthGrants(ctx context.Context) ([]OAuthGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id,g.client_id,c.name,g.resource,g.scopes_json,g.created_at,g.expires_at,g.revoked_at FROM oauth_grants g JOIN oauth_clients c ON c.client_id=g.client_id ORDER BY g.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []OAuthGrant{}
	for rows.Next() {
		var g OAuthGrant
		var raw string
		if err := rows.Scan(&g.ID, &g.ClientID, &g.Name, &g.Resource, &raw, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &g.Scopes); err != nil {
			return nil, err
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

func (s *Store) RevokeOAuthGrant(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(timestampFormat)
	res, err := tx.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at=? WHERE id=? AND revoked_at=''`, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_access_tokens SET revoked_at=? WHERE grant_id=? AND revoked_at=''`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) OAuthAudit(ctx context.Context, eventType, clientID, result string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO oauth_audit_events(event_at,event_type,client_id,result,subject_ref) VALUES(?,?,?,?, 'owner')`, time.Now().UTC().Format(timestampFormat), eventType, clientID, result)
	return err
}

var ErrOAuthDisabled = errors.New("oauth client disabled")
var ErrOAuthPublicClient = errors.New("public OAuth client has no client secret")
var ErrOAuthClientTypeConflict = errors.New("oauth client registration type conflict")

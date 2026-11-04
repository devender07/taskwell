package oauth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/auth"
)

// errBadGrant is a code that's unknown, expired, used, issued to another client or redirect URI, or whose PKCE
// verifier doesn't match.
var errBadGrant = errors.New("invalid grant")

type client struct {
	ID           string
	Name         string
	RedirectURIs []string
}

// redirects reports whether uri is one of the client's registered redirect URIs, exactly.
func (c *client) redirects(uri string) bool { return slices.Contains(c.RedirectURIs, uri) }

// model owns every OAuth query.
type model struct{ db *pgxpool.Pool }

func (m *model) createClient(ctx context.Context, name string, uris []string) (string, error) {
	var id string
	if err := m.db.QueryRow(ctx, `insert into oauth_clients (name, redirect_uris) values ($1, $2) returning id::text`,
		name, uris).Scan(&id); err != nil {
		return "", fmt.Errorf("create oauth client: %w", err)
	}
	return id, nil
}

// client returns the client with the id, or nil when there is none.
func (m *model) client(ctx context.Context, id string) (*client, error) {
	c := client{ID: id}
	err := m.db.QueryRow(ctx, `select name, redirect_uris from oauth_clients where id = $1`, id).Scan(&c.Name, &c.RedirectURIs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find oauth client: %w", err)
	}
	return &c, nil
}

// createCode stores an authorization code's hash, valid for 10 minutes.
func (m *model) createCode(ctx context.Context, codeHash []byte, clientID, userID, redirectURI, challenge string) error {
	if _, err := m.db.Exec(ctx, `
		insert into oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, expires_at)
		values ($1, $2, $3, $4, $5, now() + interval '10 minutes')`, codeHash, clientID, userID, redirectURI, challenge); err != nil {
		return fmt.Errorf("create oauth code: %w", err)
	}
	return nil
}

// redeem deletes the code and creates a personal API token named after the client, in one transaction, and returns
// the token. errBadGrant when the code doesn't fit clientID and redirectURI or verifier fails PKCE (nothing changes);
// auth.ErrTooManyTokens at the token cap.
func (m *model) redeem(ctx context.Context, codeHash []byte, clientID, redirectURI, verifier string) (string, error) {
	var tok string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var userID, name, challenge string
		err := tx.QueryRow(ctx, `
			delete from oauth_codes o using oauth_clients c
			where o.code_hash = $1 and o.expires_at > now() and o.client_id = $2 and o.redirect_uri = $3 and c.id = o.client_id
			returning o.user_id::text, left(c.name, 60), o.code_challenge`, codeHash, clientID, redirectURI).
			Scan(&userID, &name, &challenge)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !pkceOK(verifier, challenge) {
			return errBadGrant
		}
		if err != nil {
			return fmt.Errorf("redeem oauth code: %w", err)
		}
		tok, err = auth.NewAPIToken(ctx, tx, userID, name)
		return err
	})
	return tok, err
}

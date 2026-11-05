package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/auth"
	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

type Handler struct {
	model        *model
	cfg          *sysconfig.Store
	lim          web.Limiter
	authenticate func(*http.Request) (string, error)

	// APIURL is the API's public URL, the authorization server's; "" means app_base_url's host serves the API.
	APIURL string
}

// New takes the auth handler's Authenticate.
func New(db *pgxpool.Pool, cfg *sysconfig.Store, authenticate func(*http.Request) (string, error)) *Handler {
	return &Handler{model: &model{db: db}, cfg: cfg, authenticate: authenticate,
		lim: web.Limiter{DB: db, Name: "oauth_register", Burst: 10, Per: 6 * time.Minute}}
}

// metadata is the RFC 8414 authorization server metadata.
func (h *Handler) metadata(w http.ResponseWriter, r *http.Request) error {
	base, err := h.cfg.Get(r.Context(), "app_base_url")
	if err != nil {
		return err
	}
	app, api := strings.TrimRight(base, "/"), strings.TrimRight(h.APIURL, "/")
	if api == "" {
		api = app
	}
	web.JSON(w, http.StatusOK, map[string]any{
		"issuer":                                api,
		"authorization_endpoint":                app + "/oauth/authorize", // the consent page, in the web app
		"token_endpoint":                        api + "/api/oauth/token",
		"registration_endpoint":                 api + "/api/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"taskwell"},
	})
	return nil
}

// oauthErr answers 400 with an RFC 6749 / 7591 error.
func oauthErr(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	web.JSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": desc})
}

// register is RFC 7591 dynamic registration of a public client.
func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	if stop, err := web.Limited(w, r, h.lim, web.ClientIP(r)); err != nil || stop {
		return err
	}
	var in struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		oauthErr(w, "invalid_client_metadata", "The body must be JSON client metadata.")
		return nil
	}
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if doc.Len(name) > 100 {
		oauthErr(w, "invalid_client_metadata", "client_name must be 100 characters or fewer.")
		return nil
	}
	if len(in.RedirectURIs) < 1 || len(in.RedirectURIs) > 5 {
		oauthErr(w, "invalid_redirect_uri", "Send 1 to 5 redirect_uris.")
		return nil
	}
	for _, u := range in.RedirectURIs {
		if !redirectOK(u) {
			oauthErr(w, "invalid_redirect_uri", "Redirect URIs must be https, or http on localhost, 127.0.0.1 or [::1], without a fragment.")
			return nil
		}
	}
	id, err := h.model.createClient(r.Context(), name, in.RedirectURIs)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, map[string]any{
		"client_id": id, "client_name": name, "redirect_uris": in.RedirectURIs, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code"}, "response_types": []string{"code"},
	})
	return nil
}

// request validates an authorization request's fields (query or form) and returns its client, or answers 400.
func (h *Handler) request(w http.ResponseWriter, r *http.Request) (*client, error) {
	id := r.FormValue("client_id")
	var c *client
	if web.IsUUID(id) {
		var err error
		if c, err = h.model.client(r.Context(), id); err != nil {
			return nil, err
		}
	}
	msg := ""
	switch {
	case c == nil:
		msg = "This app isn't registered with Taskwell. Try connecting again."
	case !c.redirects(r.FormValue("redirect_uri")):
		msg = "This app's redirect URL isn't registered."
	case r.FormValue("response_type") != "code":
		msg = "This app asked for an unsupported response type."
	case r.FormValue("code_challenge") == "" || len(r.FormValue("code_challenge")) > 128:
		msg = "This app didn't send a valid PKCE code challenge."
	case r.FormValue("code_challenge_method") != "S256":
		msg = "This app must use the S256 PKCE method."
	}
	if msg != "" {
		web.Err(w, http.StatusBadRequest, msg)
		return nil, nil
	}
	return c, nil
}

// check returns what the consent page shows.
func (h *Handler) check(w http.ResponseWriter, r *http.Request, _ string) error {
	c, err := h.request(w, r)
	if c == nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"client_name": c.Name, "redirect_uri": r.FormValue("redirect_uri")})
	return nil
}

// authorize records the user's answer and returns where to send the browser: the client's redirect URI with a code,
// or with access_denied.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, userID string) error {
	c, err := h.request(w, r)
	if c == nil {
		return err
	}
	redirect := r.FormValue("redirect_uri")
	q := url.Values{}
	if r.FormValue("allow") == "true" {
		code := secret.NewToken()
		if err := h.model.createCode(r.Context(), secret.Hash(code), c.ID, userID, redirect, r.FormValue("code_challenge")); err != nil {
			return err
		}
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	if s := r.FormValue("state"); s != "" {
		q.Set("state", s)
	}
	web.JSON(w, http.StatusOK, map[string]string{"redirect": withQuery(redirect, q)})
	return nil
}

// token trades an authorization code and its PKCE verifier for a personal API token.
func (h *Handler) token(w http.ResponseWriter, r *http.Request) error {
	if r.PostFormValue("grant_type") != "authorization_code" {
		if r.PostFormValue("grant_type") == "" {
			oauthErr(w, "invalid_request", "grant_type is required.")
		} else {
			oauthErr(w, "unsupported_grant_type", "Only authorization_code is supported.")
		}
		return nil
	}
	code, redirect, clientID, verifier := r.PostFormValue("code"), r.PostFormValue("redirect_uri"),
		r.PostFormValue("client_id"), r.PostFormValue("code_verifier")
	if code == "" || redirect == "" || clientID == "" || verifier == "" {
		oauthErr(w, "invalid_request", "code, redirect_uri, client_id and code_verifier are required.")
		return nil
	}
	tok := ""
	err := errBadGrant
	if web.IsUUID(clientID) {
		tok, err = h.model.redeem(r.Context(), secret.Hash(code), clientID, redirect, verifier)
	}
	switch {
	case errors.Is(err, errBadGrant):
		oauthErr(w, "invalid_grant", "The code is invalid, expired or already used, or doesn't match this request.")
		return nil
	case errors.Is(err, auth.ErrTooManyTokens):
		oauthErr(w, "invalid_grant", "Too many API tokens. Revoke one in Settings → API tokens.")
		return nil
	case err != nil:
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	web.JSON(w, http.StatusOK, map[string]string{"access_token": tok, "token_type": "Bearer", "scope": "taskwell"})
	return nil
}

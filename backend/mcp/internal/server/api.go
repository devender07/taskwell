package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const errBadToken = "Your Tether token is invalid or revoked."

// api calls the user service's REST API as the caller: every request carries their Authorization header.
type api struct {
	base   string // TETHER_API_URL, no trailing slash
	auth   string // "Bearer tth_…"
	client *http.Client
}

// get decodes the JSON answer to GET path?query into out.
func (a *api) get(ctx context.Context, path string, query url.Values, out any) error {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return a.do(ctx, http.MethodGet, path, nil, out)
}

// post sends form as FormData-equivalent urlencoded fields and decodes the JSON answer into out.
func (a *api) post(ctx context.Context, path string, form url.Values, out any) error {
	return a.do(ctx, http.MethodPost, path, form, out)
}

// del sends DELETE path and decodes a JSON answer, if any, into out.
func (a *api) del(ctx context.Context, path string, out any) error {
	return a.do(ctx, http.MethodDelete, path, nil, out)
}

func (a *api) do(ctx context.Context, method, path string, form url.Values, out any) error {
	data, _, err := a.send(ctx, method, path, form, 8<<20)
	if err != nil || out == nil || len(data) == 0 {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode tether api: %w", err)
	}
	return nil
}

// file returns the body and Content-Type of GET path, up to limit bytes.
func (a *api) file(ctx context.Context, path string, limit int64) ([]byte, string, error) {
	return a.send(ctx, http.MethodGet, path, nil, limit)
}

// send makes the request and returns the answer's body and Content-Type, or the API's error.
func (a *api) send(ctx context.Context, method, path string, form url.Values, limit int64) ([]byte, string, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", a.auth)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("call tether api: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, "", fmt.Errorf("read tether api: %w", err)
	}
	if res.StatusCode == http.StatusUnauthorized {
		return nil, "", errors.New(errBadToken)
	}
	if res.StatusCode >= 400 {
		return nil, "", apiError(res.StatusCode, data)
	}
	return data, res.Header.Get("Content-Type"), nil
}

// apiError is the API's message: "error", or each of "errors" as "field: message".
func apiError(status int, data []byte) error {
	var e struct {
		Error  string            `json:"error"`
		Errors map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(data, &e) // a non-JSON body falls through to the status below
	if e.Error != "" {
		return errors.New(e.Error)
	}
	if len(e.Errors) > 0 {
		msgs := make([]string, 0, len(e.Errors))
		for _, k := range slices.Sorted(maps.Keys(e.Errors)) {
			msgs = append(msgs, cmp.Or(toolField[k], k)+": "+e.Errors[k])
		}
		return errors.New(strings.Join(msgs, "\n"))
	}
	return fmt.Errorf("Tether answered %d %s.", status, http.StatusText(status))
}

// toolField names the API's form fields as the tools call them.
var toolField = map[string]string{"status_id": "status", "assignee_id": "assignee", "label_ids": "labels",
	"role_id": "role", "user_id": "member", "issue": "other"}

// ws is the API path of the workspace with the slug, plus the escaped parts after it.
func ws(slug string, parts ...string) string {
	p := "/api/workspaces/" + url.PathEscape(slug)
	for _, s := range parts {
		p += "/" + url.PathEscape(s)
	}
	return p
}

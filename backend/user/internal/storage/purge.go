package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// cloudflareAPI is Cloudflare's API; tests point it at httptest.
var cloudflareAPI = "https://api.cloudflare.com/client/v4"

var purgeClient = &http.Client{Timeout: 10 * time.Second}

// purge removes the object id from Cloudflare's edge cache, where the file Worker (cdn/) keeps each object for a year
// under <files_cdn_url>/<id>, so a deleted file stops being served on links issued before the delete. It does nothing
// until files_cdn_url, files_cdn_zone_id and files_cdn_api_token are set. A failure is only logged: the object is
// gone from the bucket either way, and a link to it lasts two days at most.
func (s *Store) purge(ctx context.Context, id string) {
	var v [3]string
	for i, k := range []string{"files_cdn_url", "files_cdn_zone_id", "files_cdn_api_token"} {
		var err error
		if v[i], err = s.cfg.Get(ctx, k); err != nil {
			slog.WarnContext(ctx, "edge cache purge skipped", "id", id, "err", err)
			return
		}
		if v[i] == "" {
			return
		}
	}
	if err := purgeURL(ctx, v[1], v[2], strings.TrimRight(v[0], "/")+"/"+id); err != nil {
		slog.WarnContext(ctx, "edge cache purge failed", "id", id, "err", err)
	}
}

// purgeURL asks Cloudflare to drop the URL from every data center's cache (purge by single file).
func purgeURL(ctx context.Context, zone, token, file string) error {
	body, err := json.Marshal(map[string][]string{"files": {file}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudflareAPI+"/zones/"+url.PathEscape(zone)+"/purge_cache", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := purgeClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare purge: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	var out struct {
		Success bool `json:"success"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(b, &out) != nil || !out.Success {
		return fmt.Errorf("cloudflare purge: %d %s", res.StatusCode, b)
	}
	return nil
}

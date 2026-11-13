package server

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFiles checks that attach_file sends the issue's project and id with the decoded bytes as the multipart "file",
// with the caller's token, and that list_issue_attachments maps the API's files.
func TestFiles(t *testing.T) {
	const token = "Bearer tth_test"
	png := []byte("\x89PNG\r\n\x1a\nfake")
	var fields map[string]string
	var gotName string
	var gotBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			http.Error(w, `{"error":"Not signed in."}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.RequestURI() {
		case "GET /api/workspaces/acme/issues/ENG-7":
			_, _ = w.Write([]byte(`{"id":"i7","identifier":"ENG-7","project":{"key":"ENG"}}`))
		case "POST /api/workspaces/acme/files":
			mr, err := r.MultipartReader()
			if err != nil {
				t.Errorf("multipart: %v", err)
				return
			}
			fields = map[string]string{}
			for part, err := mr.NextPart(); err == nil; part, err = mr.NextPart() {
				b, _ := io.ReadAll(part)
				if part.FormName() == "file" {
					gotName, gotBytes = part.FileName(), b
					continue
				}
				fields[part.FormName()] = string(b)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"f1","url":"/api/files/f1/shot.png","name":"shot.png","content_type":"image/png","size":12,"image":true}`))
		case "GET /api/workspaces/acme/projects/ENG/files?issue=i7":
			_, _ = w.Write([]byte(`{"folder":null,"path":[],"folders":[],"files":[{"id":"f1","name":"shot.png",
				"content_type":"image/png","size":12,"uploader":{"id":"u1","name":"Ada"},"created_at":"2026-11-07T10:00:00Z",
				"url":"/api/files/f1/shot.png","can_edit":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cs := connect(t, srv, token)

	text, isErr := call(t, cs, "attach_file", map[string]any{"workspace": "acme", "identifier": "ENG-7",
		"filename": "shot.png", "content_base64": base64.StdEncoding.EncodeToString(png)})
	if isErr {
		t.Fatalf("attach_file: %s", text)
	}
	if fields["project"] != "ENG" || fields["issue"] != "i7" || gotName != "shot.png" || !bytes.Equal(gotBytes, png) {
		t.Errorf("sent fields %v, file %q %q", fields, gotName, gotBytes)
	}
	if !strings.Contains(text, `"url":"https://app.test/api/files/f1/shot.png"`) {
		t.Errorf("attach_file = %s", text)
	}

	text, isErr = call(t, cs, "list_issue_attachments", map[string]any{"workspace": "acme", "identifier": "ENG-7"})
	want := `[{"id":"f1","name":"shot.png","content_type":"image/png","size":12,"uploader":"Ada",` +
		`"created_at":"2026-11-07T10:00:00Z","url":"https://app.test/api/files/f1/shot.png","can_edit":true}]`
	if isErr || text != want {
		t.Errorf("list_issue_attachments = %s, want %s", text, want)
	}

	if text, isErr := call(t, cs, "upload_project_file", map[string]any{"workspace": "acme", "project": "ENG",
		"filename": "big.png", "content_base64": strings.Repeat("A", base64.StdEncoding.EncodedLen(maxUpload+1))}); !isErr {
		t.Errorf("oversize upload_project_file = %s, want an error", text)
	}
}

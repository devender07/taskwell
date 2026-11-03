package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testsView is the MCP Apps view (ext-apps) that hosts such as Claude Desktop render for get_test_run and
// list_test_cases; text-only clients keep reading the JSON.
//
//go:embed tests.html
var testsView string

const (
	testsViewURI  = "ui://tether/tests.html"
	appMIMEType   = "text/html;profile=mcp-app"
	maxScreenshot = 5 << 20
)

// viewMeta links a tool to the tests view; app-only tools are hidden from the model.
func viewMeta(appOnly bool) mcp.Meta {
	ui := map[string]any{"resourceUri": testsViewURI}
	if appOnly {
		ui["visibility"] = []string{"app"}
	}
	return mcp.Meta{"ui": ui, "ui/resourceUri": testsViewURI}
}

// appTools registers the tests view and the screenshot loader it calls.
func (t *tools) appTools(s *mcp.Server) {
	// ponytail: the ext-apps client loads from unpkg (pinned); bundle it into tests.html if a host blocks the CDN.
	s.AddResource(&mcp.Resource{URI: testsViewURI, Name: "Tether tests", MIMEType: appMIMEType},
		func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: testsViewURI, MIMEType: appMIMEType, Text: testsView,
				Meta: mcp.Meta{"ui": map[string]any{"csp": map[string]any{"resourceDomains": []string{"https://unpkg.com"}}}}}}}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_test_screenshot", Annotations: readOnly, Meta: viewMeta(true),
		Description: "Load a test result's screenshot for the tests view."}, t.getTestScreenshot)
}

type screenshotIn struct {
	URL string `json:"url" jsonschema:"a screenshot URL from get_test_run"`
}

// getTestScreenshot fetches a Tether file with the caller's token: the view's iframe has no Tether sign-in. Only
// /api/files paths, so the view can't make the server fetch anything else.
func (t *tools) getTestScreenshot(ctx context.Context, _ *mcp.CallToolRequest, in screenshotIn) (*mcp.CallToolResult, any, error) {
	path := strings.TrimPrefix(in.URL, t.app)
	if !strings.HasPrefix(path, "/api/files/") || strings.Contains(path, "..") {
		return nil, nil, fmt.Errorf("Not a Tether screenshot: %s", in.URL)
	}
	data, mime, err := t.api.file(ctx, path, maxScreenshot)
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(mime, "image/") {
		return nil, nil, fmt.Errorf("Not an image: %s", in.URL)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: mime}}}, nil, nil
}

// viewResult answers with v as JSON text for the model and as structured content for the view.
func viewResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("encode result: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: json.RawMessage(b)}, nil, nil
}

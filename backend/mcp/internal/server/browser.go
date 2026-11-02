package server

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const browserGuide = "Open Tether's sandboxed test browser on an issue to record a test case step by step: nothing to install and " +
	"no Tether AI spent, you decide each action and Tether keeps it as a replayable step. It starts at url (a page on one of the " +
	"workspace's test sites; the first site when left out). Each action answers the page: URL, title, numbered interactive " +
	"elements (the ref for browser_click, browser_type, browser_select and browser_fill_password) and the visible text. " +
	"Flow: sign in with browser_fill_password and an account label from list_test_accounts (never ask the user for a password), " +
	"follow the case's steps, add browser_assert_text / browser_assert_no_text for the expected result (negative checks catch " +
	"most edge cases: a draft must not show in the other inbox), then browser_save to the case from list_test_cases and " +
	"run_recorded_tests to prove it passes. A failed action records nothing. The session ends after 10 idle minutes, 30 in all, " +
	"on browser_save or browser_close. When playwright-cli is available you can record with it and save_test_script instead."

// browserTools registers the browser session tools.
func (t *tools) browserTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "browser_open", Description: browserGuide}, t.browserOpen)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_navigate", Description: "Go to a URL on one of the workspace's test sites."},
		browserAct[browserNavigateIn](t, "navigate"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_click", Description: "Click the element with the ref."}, browserAct[browserRefIn](t, "click"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_type", Description: "Type text into the element with the ref, replacing what's in it. " +
		"Never a password: use browser_fill_password."}, browserAct[browserTypeIn](t, "type"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_select", Description: "Choose an option, by value or text, in the select with the ref."},
		browserAct[browserSelectIn](t, "select"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_press", Description: "Press a key: Enter, Tab, Escape, Backspace, Delete, Space, an arrow " +
		"key, PageUp, PageDown, Home or End."}, browserAct[browserPressIn](t, "press"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_wait", Description: "Wait 1-5 seconds for the page to change."}, browserAct[browserWaitIn](t, "wait"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_fill_password", Description: "Type a test account's password into the password field " +
		"with the ref. Tether types it; you never see it. The account must be allowed on this page's site (list_test_accounts)."},
		browserAct[browserPasswordIn](t, "fill_password"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_assert_text", Description: "Check the text is on the page (waits up to 5 s). " +
		"Recorded only when it holds now."}, browserAct[browserTextIn](t, "assert_text"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_assert_no_text", Description: "Check the text is not on the page (waits up to 5 s for it " +
		"to go). Recorded only when it holds now."}, browserAct[browserTextIn](t, "assert_no_text"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_snapshot", Annotations: readOnly, Description: "Read the page again; records nothing."},
		browserAct[browserIn](t, "snapshot"))
	mcp.AddTool(s, &mcp.Tool{Name: "browser_screenshot", Annotations: readOnly, Description: "See the page as an image."}, t.browserScreenshot)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_save", Description: "Save the session's recorded steps on a test case and close the session. " +
		"check is text that's on the page only when the expected result holds. Replaces the case's recorded steps and Playwright code. " +
		"Then call run_recorded_tests and get_test_run."}, t.browserSave)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_close", Annotations: destructive, Description: "Close the session without saving."}, t.browserClose)
}

// --- inputs ---

type browserIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	SessionID string `json:"session_id" jsonschema:"from browser_open"`
}

type browserNavigateIn struct {
	browserIn
	URL string `json:"url" jsonschema:"a URL on one of the workspace's test sites"`
}

type browserRefIn struct {
	browserIn
	Ref int `json:"ref" jsonschema:"the element's number in the latest page"`
}

type browserTypeIn struct {
	browserRefIn
	Text string `json:"text"`
}

type browserSelectIn struct {
	browserRefIn
	Value string `json:"value" jsonschema:"the option's value or text"`
}

type browserPressIn struct {
	browserIn
	Key string `json:"key" jsonschema:"such as Enter"`
}

type browserWaitIn struct {
	browserIn
	Seconds int `json:"seconds" jsonschema:"1-5"`
}

type browserPasswordIn struct {
	browserRefIn
	Account string `json:"account" jsonschema:"a test account label from list_test_accounts"`
}

type browserTextIn struct {
	browserIn
	Text string `json:"text" jsonschema:"up to 200 characters"`
}

type browserOpenIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	URL        string `json:"url,omitempty" jsonschema:"where to start, on a test site; the first test site when left out"`
}

type browserSaveIn struct {
	browserIn
	CaseID string `json:"case_id" jsonschema:"from list_test_cases"`
	Check  string `json:"check" jsonschema:"text on the page only when the expected result holds; up to 200 characters"`
}

// browserInput is an action's input: its session and the action's fields.
type browserInput interface {
	session() browserIn
	form() url.Values
}

func (b browserIn) session() browserIn { return b }
func (b browserIn) form() url.Values   { return url.Values{} }

func (b browserNavigateIn) form() url.Values { return url.Values{"url": {b.URL}} }
func (b browserRefIn) form() url.Values      { return url.Values{"ref": {strconv.Itoa(b.Ref)}} }
func (b browserTypeIn) form() url.Values {
	return url.Values{"ref": {strconv.Itoa(b.Ref)}, "text": {b.Text}}
}
func (b browserSelectIn) form() url.Values {
	return url.Values{"ref": {strconv.Itoa(b.Ref)}, "value": {b.Value}}
}
func (b browserPressIn) form() url.Values { return url.Values{"key": {b.Key}} }
func (b browserWaitIn) form() url.Values  { return url.Values{"seconds": {strconv.Itoa(b.Seconds)}} }
func (b browserPasswordIn) form() url.Values {
	return url.Values{"ref": {strconv.Itoa(b.Ref)}, "account": {b.Account}}
}
func (b browserTextIn) form() url.Values { return url.Values{"text": {b.Text}} }

// --- API shapes ---

type browserPage struct {
	ID   string `json:"id"`
	Page struct {
		Elements string `json:"elements"`
	} `json:"page"`
	Error string `json:"error"`
	Steps *int   `json:"steps"`
}

// text is the page for the model: the session, a failed action and the steps recorded so far, then the page.
func (p browserPage) text() *mcp.CallToolResult {
	var b strings.Builder
	if p.ID != "" {
		fmt.Fprintf(&b, "Session: %s\n", p.ID)
	}
	if p.Error != "" {
		fmt.Fprintf(&b, "Failed, nothing recorded: %s\n", p.Error)
	}
	if p.Steps != nil {
		fmt.Fprintf(&b, "Recorded steps: %d\n", *p.Steps)
	}
	b.WriteString("\n" + p.Page.Elements)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: b.String()}}}
}

func sessionPath(in browserIn, parts ...string) string {
	return ws(in.Workspace, append([]string{"browser-sessions", strings.TrimSpace(in.SessionID)}, parts...)...)
}

// --- tools ---

func (t *tools) browserOpen(ctx context.Context, _ *mcp.CallToolRequest, in browserOpenIn) (*mcp.CallToolResult, any, error) {
	var p browserPage
	form := url.Values{}
	if u := strings.TrimSpace(in.URL); u != "" {
		form.Set("url", u)
	}
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "browser-sessions"), form, &p); err != nil {
		return nil, nil, err
	}
	return p.text(), nil, nil
}

// browserAct is the tool for one action.
func browserAct[In browserInput](t *tools, action string) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		form := in.form()
		form.Set("action", action)
		var p browserPage
		if err := t.api.post(ctx, sessionPath(in.session(), "actions"), form, &p); err != nil {
			return nil, nil, err
		}
		return p.text(), nil, nil
	}
}

func (t *tools) browserScreenshot(ctx context.Context, _ *mcp.CallToolRequest, in browserIn) (*mcp.CallToolResult, any, error) {
	data, mime, err := t.api.file(ctx, sessionPath(in, "screenshot"), maxScreenshot)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: mime}}}, nil, nil
}

func (t *tools) browserSave(ctx context.Context, _ *mcp.CallToolRequest, in browserSaveIn) (*mcp.CallToolResult, any, error) {
	var c testCase
	if err := t.api.post(ctx, sessionPath(in.browserIn, "save"), url.Values{"case_id": {strings.TrimSpace(in.CaseID)}, "check": {in.Check}}, &c); err != nil {
		return nil, nil, err
	}
	return result(c)
}

func (t *tools) browserClose(ctx context.Context, _ *mcp.CallToolRequest, in browserIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, sessionPath(in), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"closed": in.SessionID})
}

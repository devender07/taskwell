package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pollEvery is how often get_test_run checks a run it waits for.
var pollEvery = 3 * time.Second

// finished are the statuses a test run ends in.
var finished = []string{"passed", "failed", "error", "cancelled", "limit_reached"}

const testingHow = " Three ways to test an issue: (1) run_ai_test has Tether's AI drive a browser against the workspace's test site, " +
	"then get_test_run with wait_seconds until it finishes; (2) test it yourself with your own tools, recording the outcome with " +
	"start_manual_test_run and mark_test_result for each result; (3) record a case's browser steps with save_test_script, then " +
	"run_recorded_tests replays them with no AI spend. A failed result becomes a bug through create_issue: the case's " +
	"title as the title, the result's summary and reproduction steps as the description."

// testTools registers the AI testing tools: test cases, runs, sites, accounts and usage.
func (t *tools) testTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_test_cases", Annotations: readOnly, Meta: viewMeta(false),
		Description: "List an issue's test cases with their ids, steps, expected result and last result. " +
			"recorded means the case has browser steps (from a passed AI run or save_test_script) that replay without the AI; " +
			"has_playwright means save_test_script stored Playwright code for it." + testingHow}, t.listTestCases)
	mcp.AddTool(s, &mcp.Tool{Name: "create_test_case",
		Description: "Add a test case to an issue: a title, the steps to follow and the expected result."}, t.createTestCase)
	mcp.AddTool(s, &mcp.Tool{Name: "create_test_cases",
		Description: "Add several test cases to an issue in one call, in order. Stops at the first error and says which were created."},
		t.createTestCases)
	mcp.AddTool(s, &mcp.Tool{Name: "update_test_case",
		Description: "Change a test case. Only the fields you give change; steps replaces all steps."}, t.updateTestCase)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_test_case", Annotations: destructive, Description: "Delete a test case."}, t.deleteTestCase)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_test_cases",
		Description: "Put an issue's test cases in a new order: case_ids lists every case once, in order."}, t.reorderTestCases)
	mcp.AddTool(s, &mcp.Tool{Name: "run_ai_test",
		Description: "Start an AI test run on an issue: Tether's AI tests every case (or case_id only) in a sandboxed browser against " +
			"the workspace's test site, spending the workspace's monthly AI limit. regenerate has the AI rewrite the cases from the issue first. " +
			"Then call get_test_run with the run id and wait_seconds until it finishes." + testingHow}, t.runAITest)
	mcp.AddTool(s, &mcp.Tool{Name: "start_manual_test_run",
		Description: "Start a test run that you carry out yourself: it returns one pending result per test case, with result ids. " +
			"Test each case with your own tools, then call mark_test_result for each. The run finishes when every result is marked." + testingHow},
		t.startManualTestRun)
	mcp.AddTool(s, &mcp.Tool{Name: "mark_test_result",
		Description: "Record a manual run's result: passed, failed or skipped, with a note saying what you saw (always for a failure). " +
			"AI runs are marked by Tether.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, t.markTestResult)
	mcp.AddTool(s, &mcp.Tool{Name: "get_test_run", Annotations: readOnly, Meta: viewMeta(false),
		Description: "Read a test run: status, counts, cost, error, and each result with its summary, reproduction steps and screenshots. " +
			"wait_seconds (up to 120) waits for a running run to finish, checking every 3 seconds; call again if it is still running."},
		t.getTestRun)
	mcp.AddTool(s, &mcp.Tool{Name: "list_test_runs", Annotations: readOnly,
		Description: "List an issue's latest 10 test runs, newest first, with their status and counts."}, t.listTestRuns)
	mcp.AddTool(s, &mcp.Tool{Name: "cancel_test_run",
		Description: "Cancel a run that hasn't finished. An AI run in progress stops after its current step."}, t.cancelTestRun)
	mcp.AddTool(s, &mcp.Tool{Name: "save_test_script", Description: recordingGuide}, t.saveTestScript)
	mcp.AddTool(s, &mcp.Tool{Name: "clear_test_script", Annotations: destructive,
		Description: "Remove a test case's recorded steps and Playwright code; runs then need the AI to test it."}, t.clearTestScript)
	mcp.AddTool(s, &mcp.Tool{Name: "run_recorded_tests",
		Description: "Start a recorded test run on an issue: Tether replays each recorded case's steps in a sandboxed browser against " +
			"the workspace's test site, with no AI and no AI spend. A replay or assertion that fails fails the case with the reason, " +
			"the steps so far and screenshots; unrecorded cases are skipped. Then call get_test_run with the run id and wait_seconds " +
			"until it finishes."}, t.runRecordedTests)
	mcp.AddTool(s, &mcp.Tool{Name: "get_playwright_spec", Annotations: readOnly,
		Description: "Get an issue's test cases as a Playwright spec (TypeScript) a developer can run locally with " +
			"npx playwright test: stored Playwright code first, else code generated from the recorded steps, else test.fixme."},
		t.getPlaywrightSpec)
	mcp.AddTool(s, &mcp.Tool{Name: "ai_test_usage", Annotations: readOnly,
		Description: "The workspace's AI testing spend this month, its monthly limit and when it resets."}, t.aiTestUsage)
	mcp.AddTool(s, &mcp.Tool{Name: "list_test_sites", Annotations: readOnly,
		Description: "List the sites AI test runs may visit."}, t.listTestSites)
	mcp.AddTool(s, &mcp.Tool{Name: "add_test_site",
		Description: "Allow AI test runs to visit a site (an https:// base URL). The first site is where runs start."}, t.addTestSite)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_test_site", Annotations: destructive, Description: "Remove a test site."}, t.deleteTestSite)
	mcp.AddTool(s, &mcp.Tool{Name: "list_test_accounts", Annotations: readOnly,
		Description: "List the accounts AI test runs can sign in with. Passwords are never shown."}, t.listTestAccounts)
	mcp.AddTool(s, &mcp.Tool{Name: "save_test_account",
		Description: "Add a sign-in account for AI test runs, or change one (account_id). " +
			"A new account needs a password; leave it out when changing one to keep it."}, t.saveTestAccount)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_test_account", Annotations: destructive,
		Description: "Remove a test account."}, t.deleteTestAccount)
}

const recordingGuide = "Record a test case's browser steps so Tether replays it without AI (run_recorded_tests) and exports it to Playwright. " +
	"Explore the site with your own browser tools first; Tether spends no AI here. " +
	"steps start with navigate (a URL on one of the workspace's test sites, from list_test_sites); tools: navigate {url}, " +
	"click {target}, type {target, text}, select {target, value}, press {key}, wait {seconds 1-5}, fill_password {target, account}, " +
	"assert_text {text} and assert_no_text {text} (wait up to 5 s for the text to appear / disappear). " +
	"target is {css, tag, name}: prefer a stable css selector matching one element (data-testid, an id without long digit runs, " +
	"name, aria-label, placeholder) and always give tag (button, a, input, select, textarea, ...) and the accessible name " +
	"(aria-label, label text, placeholder, inner text) as the fallback. " +
	"Never put a password in a step: use fill_password with an account label from list_test_accounts (when there is none, ask the " +
	"user to add one in Settings → AI testing). Add assert_text / assert_no_text after the steps that matter: negative checks catch " +
	"most edge cases (a draft must not show in the other inbox). check is a text that's on the page only when the expected result holds. " +
	"When playwright-cli is available, record with it: snapshot for element refs, each action prints its Playwright code, " +
	"generate-locator for assertion locators. Send that code as playwright (the test's body, or a whole test(...) block), with " +
	"passwords as process.env.TETHER_PASSWORD_<LABEL> (the account label upper-cased, other characters as _), never a literal; and " +
	"translate each action to a step for the no-AI replay: role button/link/textbox/combobox/checkbox plus accessible name becomes " +
	"target {tag, name} (check the real tag with playwright-cli eval \"el => el.tagName\" eN, and use a data-testid/id/name selector " +
	"as css when eval shows one). Leave playwright out to keep the stored code, or send \"\" to clear it. " +
	"Then call run_recorded_tests and get_test_run to prove it passes."

// --- inputs ---

type caseIn struct {
	Title    string   `json:"title"`
	Steps    []string `json:"steps" jsonschema:"what to do, one step each"`
	Expected string   `json:"expected" jsonschema:"what should happen"`
}

type createCaseIn struct {
	Workspace  string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string   `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Title      string   `json:"title"`
	Steps      []string `json:"steps" jsonschema:"what to do, one step each"`
	Expected   string   `json:"expected" jsonschema:"what should happen"`
}

type createCasesIn struct {
	Workspace  string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string   `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Cases      []caseIn `json:"cases"`
}

type updateCaseIn struct {
	Workspace  string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string   `json:"identifier" jsonschema:"the case's issue identifier, such as ENG-142"`
	CaseID     string   `json:"case_id" jsonschema:"from list_test_cases"`
	Title      *string  `json:"title,omitempty"`
	Steps      []string `json:"steps,omitempty"`
	Expected   *string  `json:"expected,omitempty"`
}

type caseIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	CaseID    string `json:"case_id" jsonschema:"from list_test_cases"`
}

type reorderIn struct {
	Workspace  string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string   `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	CaseIDs    []string `json:"case_ids" jsonschema:"every case id, in the new order"`
}

type aiRunIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	CaseID     string `json:"case_id,omitempty" jsonschema:"test only this case"`
	Regenerate bool   `json:"regenerate,omitempty" jsonschema:"the AI rewrites the cases from the issue before testing; not with case_id"`
}

type stepIn struct {
	Tool   string `json:"tool" jsonschema:"navigate, click, type, select, press, wait, fill_password, assert_text or assert_no_text"`
	URL    string `json:"url,omitempty" jsonschema:"navigate: a URL on a test site"`
	Target *struct {
		CSS  string `json:"css" jsonschema:"a selector matching only this element, or empty"`
		Tag  string `json:"tag" jsonschema:"the element's tag, such as button or input"`
		Name string `json:"name" jsonschema:"its accessible name: aria-label, label text, placeholder or inner text"`
	} `json:"target,omitempty" jsonschema:"click, type, select and fill_password: the element"`
	Text    string `json:"text,omitempty" jsonschema:"type: the text to type; assert_text and assert_no_text: the text to look for"`
	Value   string `json:"value,omitempty" jsonschema:"select: the option's value"`
	Key     string `json:"key,omitempty" jsonschema:"press: a key such as Enter"`
	Seconds int    `json:"seconds,omitempty" jsonschema:"wait: 1-5"`
	Account string `json:"account,omitempty" jsonschema:"fill_password: a test account label from list_test_accounts"`
}

type scriptIn struct {
	Workspace  string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	CaseID     string   `json:"case_id" jsonschema:"from list_test_cases"`
	Steps      []stepIn `json:"steps" jsonschema:"1-60 steps, the first a navigate"`
	Check      string   `json:"check" jsonschema:"text on the page only when the expected result holds; up to 200 characters"`
	Playwright *string  `json:"playwright,omitempty" jsonschema:"the case's test as Playwright TypeScript (body or whole test); empty clears it"`
}

type markIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	RunID     string `json:"run_id"`
	ResultID  string `json:"result_id" jsonschema:"from the run's results"`
	Status    string `json:"status" jsonschema:"passed, failed or skipped"`
	Note      string `json:"note,omitempty" jsonschema:"what you saw; up to 2000 characters"`
}

type runIn struct {
	Workspace   string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	RunID       string `json:"run_id"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"0–120: wait this long for a running run to finish"`
}

type runIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	RunID     string `json:"run_id"`
}

type siteIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	BaseURL   string `json:"base_url" jsonschema:"such as https://staging.example.com"`
}

type siteIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	SiteID    string `json:"site_id" jsonschema:"from list_test_sites"`
}

type accountIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	AccountID string `json:"account_id,omitempty" jsonschema:"the account to change, from list_test_accounts; leave out to add one"`
	Label     string `json:"label" jsonschema:"such as Admin user"`
	Username  string `json:"username"`
	Password  string `json:"password,omitempty" jsonschema:"required for a new account; leave out to keep the saved one"`
}

type accountIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	AccountID string `json:"account_id" jsonschema:"from list_test_accounts"`
}

// --- API shapes and outputs ---

type testCase struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Steps      []string `json:"steps"`
	Expected   string   `json:"expected"`
	Source     string   `json:"source,omitempty"`
	LastResult *struct {
		Status string `json:"status"`
		Kind   string `json:"kind"`
		At     string `json:"at"`
	} `json:"last_result,omitempty"`
	Recorded      bool `json:"recorded,omitempty"`
	HasPlaywright bool `json:"has_playwright,omitempty"`
}

type testRun struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Issue struct {
		Identifier string `json:"identifier"`
	} `json:"issue"`
	Status     string   `json:"status"`
	Error      *string  `json:"error"`
	StartedBy  *person  `json:"started_by"`
	CreatedAt  string   `json:"created_at"`
	FinishedAt *string  `json:"finished_at"`
	CostUSD    *float64 `json:"cost_usd"`
	Counts     struct {
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
		Error   int `json:"error"`
		Pending int `json:"pending"`
	} `json:"counts"`
	Results []struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Steps        []string `json:"steps"`
		Expected     string   `json:"expected"`
		Status       string   `json:"status"`
		Summary      *string  `json:"summary"`
		Reproduction []string `json:"reproduction"`
		MarkedBy     *person  `json:"marked_by"`
		Log          []struct {
			ScreenshotURL *string `json:"screenshot_url"`
		} `json:"log"`
	} `json:"results"`
}

type runOut struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Issue      string      `json:"issue"`
	Status     string      `json:"status"`
	Error      string      `json:"error,omitempty"`
	StartedBy  string      `json:"started_by,omitempty"`
	CreatedAt  string      `json:"created_at"`
	FinishedAt string      `json:"finished_at,omitempty"`
	CostUSD    *float64    `json:"cost_usd,omitempty"`
	Counts     any         `json:"counts"`
	URL        string      `json:"url,omitempty"`
	Results    []resultOut `json:"results,omitempty"`
}

type resultOut struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Steps        []string `json:"steps,omitempty"`
	Expected     string   `json:"expected,omitempty"`
	Status       string   `json:"status"`
	Summary      string   `json:"summary,omitempty"`
	Reproduction []string `json:"reproduction,omitempty"`
	MarkedBy     string   `json:"marked_by,omitempty"`
	Screenshots  []string `json:"screenshots,omitempty"`
}

func (t *tools) runOut(slug string, r testRun) runOut {
	o := runOut{ID: r.ID, Kind: r.Kind, Issue: r.Issue.Identifier, Status: r.Status, StartedBy: name(r.StartedBy),
		CreatedAt: r.CreatedAt, CostUSD: r.CostUSD, Counts: r.Counts, URL: t.issueURL(slug, r.Issue.Identifier)}
	if r.Error != nil {
		o.Error = *r.Error
	}
	if r.FinishedAt != nil {
		o.FinishedAt = *r.FinishedAt
	}
	for _, x := range r.Results {
		ro := resultOut{ID: x.ID, Title: x.Title, Steps: x.Steps, Expected: x.Expected, Status: x.Status,
			Reproduction: x.Reproduction, MarkedBy: name(x.MarkedBy)}
		if x.Summary != nil {
			ro.Summary = *x.Summary
		}
		for _, l := range x.Log {
			if l.ScreenshotURL != nil {
				ro.Screenshots = append(ro.Screenshots, t.app+*l.ScreenshotURL) // the app serves /api/files
			}
		}
		o.Results = append(o.Results, ro)
	}
	return o
}

// --- cases ---

func (t *tools) cases(ctx context.Context, slug, identifier string) ([]testCase, error) {
	var cs []testCase
	return cs, t.api.get(ctx, ws(slug, "issues", identifier, "test-cases"), nil, &cs)
}

func (t *tools) listTestCases(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	cs, err := t.cases(ctx, in.Workspace, in.Identifier)
	if err != nil {
		return nil, nil, err
	}
	return viewResult(struct {
		Issue string     `json:"issue"`
		URL   string     `json:"url"`
		Cases []testCase `json:"cases"`
	}{in.Identifier, t.issueURL(in.Workspace, in.Identifier), cs})
}

func caseForm(c caseIn) url.Values {
	return url.Values{"title": {c.Title}, "steps": c.Steps, "expected": {c.Expected}}
}

func (t *tools) createTestCase(ctx context.Context, _ *mcp.CallToolRequest, in createCaseIn) (*mcp.CallToolResult, any, error) {
	var c testCase
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "test-cases"), caseForm(caseIn{Title: in.Title, Steps: in.Steps, Expected: in.Expected}), &c); err != nil {
		return nil, nil, err
	}
	return result(c)
}

func (t *tools) createTestCases(ctx context.Context, _ *mcp.CallToolRequest, in createCasesIn) (*mcp.CallToolResult, any, error) {
	if len(in.Cases) == 0 {
		return nil, nil, errors.New("Give at least one case.")
	}
	created := []testCase{}
	for k, c := range in.Cases {
		var tc testCase
		if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "test-cases"), caseForm(c), &tc); err != nil {
			ids := make([]string, len(created))
			for i, c := range created {
				ids[i] = fmt.Sprintf("%q (%s)", c.Title, c.ID)
			}
			done := "None were created."
			if len(ids) > 0 {
				done = fmt.Sprintf("Created %d: %s.", len(ids), strings.Join(ids, ", "))
			}
			return nil, nil, fmt.Errorf("Case %d %q failed: %w\n%s", k+1, c.Title, err, done)
		}
		created = append(created, tc)
	}
	return result(created)
}

func (t *tools) updateTestCase(ctx context.Context, _ *mcp.CallToolRequest, in updateCaseIn) (*mcp.CallToolResult, any, error) {
	// The API replaces title, steps and expected together, so the fields left out are read first.
	cs, err := t.cases(ctx, in.Workspace, in.Identifier)
	if err != nil {
		return nil, nil, err
	}
	k := slices.IndexFunc(cs, func(c testCase) bool { return c.ID == strings.TrimSpace(in.CaseID) })
	if k < 0 {
		return nil, nil, fmt.Errorf("%s has no test case %q. Its case ids come from list_test_cases.", in.Identifier, in.CaseID)
	}
	c := caseIn{Title: cs[k].Title, Steps: cs[k].Steps, Expected: cs[k].Expected}
	if in.Title != nil {
		c.Title = *in.Title
	}
	if in.Steps != nil {
		c.Steps = in.Steps
	}
	if in.Expected != nil {
		c.Expected = *in.Expected
	}
	var tc testCase
	if err := t.api.post(ctx, ws(in.Workspace, "test-cases", cs[k].ID), caseForm(c), &tc); err != nil {
		return nil, nil, err
	}
	return result(tc)
}

func (t *tools) deleteTestCase(ctx context.Context, _ *mcp.CallToolRequest, in caseIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "test-cases", in.CaseID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.CaseID})
}

func (t *tools) reorderTestCases(ctx context.Context, _ *mcp.CallToolRequest, in reorderIn) (*mcp.CallToolResult, any, error) {
	var cs []testCase
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "test-cases", "order"), url.Values{"ids": in.CaseIDs}, &cs); err != nil {
		return nil, nil, err
	}
	return result(cs)
}

func (t *tools) saveTestScript(ctx context.Context, _ *mcp.CallToolRequest, in scriptIn) (*mcp.CallToolResult, any, error) {
	sc, err := json.Marshal(map[string]any{"steps": in.Steps, "check": in.Check})
	if err != nil {
		return nil, nil, fmt.Errorf("encode script: %w", err)
	}
	form := url.Values{"script": {string(sc)}}
	if in.Playwright != nil {
		form.Set("playwright", *in.Playwright)
	}
	var c testCase
	if err := t.api.post(ctx, ws(in.Workspace, "test-cases", strings.TrimSpace(in.CaseID), "script"), form, &c); err != nil {
		return nil, nil, err
	}
	return result(c)
}

func (t *tools) clearTestScript(ctx context.Context, _ *mcp.CallToolRequest, in caseIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "test-cases", strings.TrimSpace(in.CaseID), "script"), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"cleared": in.CaseID})
}

// --- runs ---

func (t *tools) runRecordedTests(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	var r testRun
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "test-runs"), url.Values{"kind": {"replay"}}, &r); err != nil {
		return nil, nil, err
	}
	return result(t.runOut(in.Workspace, r))
}

func (t *tools) getPlaywrightSpec(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	spec, _, err := t.api.file(ctx, ws(in.Workspace, "issues", in.Identifier, "playwright"), 1<<20)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(spec)}}}, nil, nil
}

func (t *tools) runAITest(ctx context.Context, _ *mcp.CallToolRequest, in aiRunIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"kind": {"ai"}}
	if in.CaseID != "" {
		form.Set("case_id", strings.TrimSpace(in.CaseID))
	}
	if in.Regenerate {
		form.Set("regenerate", "true")
	}
	var r testRun
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "test-runs"), form, &r); err != nil {
		return nil, nil, err
	}
	return result(t.runOut(in.Workspace, r))
}

func (t *tools) startManualTestRun(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	var r testRun
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "test-runs"), url.Values{"kind": {"manual"}}, &r); err != nil {
		return nil, nil, err
	}
	return result(t.runOut(in.Workspace, r))
}

func (t *tools) markTestResult(ctx context.Context, _ *mcp.CallToolRequest, in markIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"status": {strings.ToLower(strings.TrimSpace(in.Status))}, "note": {in.Note}}
	var r testRun
	if err := t.api.post(ctx, ws(in.Workspace, "test-runs", in.RunID, "results", in.ResultID), form, &r); err != nil {
		return nil, nil, err
	}
	return result(t.runOut(in.Workspace, r))
}

func (t *tools) getTestRun(ctx context.Context, _ *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, any, error) {
	deadline := time.Now().Add(time.Duration(min(max(in.WaitSeconds, 0), 120)) * time.Second)
	for {
		var r testRun
		if err := t.api.get(ctx, ws(in.Workspace, "test-runs", in.RunID), nil, &r); err != nil {
			return nil, nil, err
		}
		left := time.Until(deadline)
		if slices.Contains(finished, r.Status) || left <= 0 {
			return viewResult(t.runOut(in.Workspace, r))
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(min(pollEvery, left)):
		}
	}
}

func (t *tools) listTestRuns(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Runs    []testRun `json:"runs"`
		HasMore bool      `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "issues", in.Identifier, "test-runs"), nil, &res); err != nil {
		return nil, nil, err
	}
	rs := make([]runOut, len(res.Runs))
	for k, r := range res.Runs {
		rs[k] = t.runOut(in.Workspace, r)
		rs[k].URL = ""
	}
	return result(rs)
}

func (t *tools) cancelTestRun(ctx context.Context, _ *mcp.CallToolRequest, in runIDIn) (*mcp.CallToolResult, any, error) {
	var r testRun
	if err := t.api.post(ctx, ws(in.Workspace, "test-runs", in.RunID, "cancel"), url.Values{}, &r); err != nil {
		return nil, nil, err
	}
	r.Results = nil
	return result(t.runOut(in.Workspace, r))
}

func (t *tools) aiTestUsage(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var u struct {
		SpentUSD float64 `json:"spent_usd"`
		LimitUSD float64 `json:"limit_usd"`
		ResetsAt string  `json:"resets_at"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "ai-usage"), nil, &u); err != nil {
		return nil, nil, err
	}
	return result(u)
}

// --- sites and accounts ---

type site struct {
	ID      string `json:"id"`
	BaseURL string `json:"base_url"`
}

func (t *tools) listTestSites(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var ss []site
	if err := t.api.get(ctx, ws(in.Workspace, "test-sites"), nil, &ss); err != nil {
		return nil, nil, err
	}
	return result(ss)
}

func (t *tools) addTestSite(ctx context.Context, _ *mcp.CallToolRequest, in siteIn) (*mcp.CallToolResult, any, error) {
	var s site
	if err := t.api.post(ctx, ws(in.Workspace, "test-sites"), url.Values{"base_url": {strings.TrimSpace(in.BaseURL)}}, &s); err != nil {
		return nil, nil, err
	}
	return result(s)
}

func (t *tools) deleteTestSite(ctx context.Context, _ *mcp.CallToolRequest, in siteIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "test-sites", in.SiteID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.SiteID})
}

type account struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
}

func (t *tools) listTestAccounts(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var as []account
	if err := t.api.get(ctx, ws(in.Workspace, "test-accounts"), nil, &as); err != nil {
		return nil, nil, err
	}
	return result(as)
}

func (t *tools) saveTestAccount(ctx context.Context, _ *mcp.CallToolRequest, in accountIn) (*mcp.CallToolResult, any, error) {
	path := ws(in.Workspace, "test-accounts")
	if id := strings.TrimSpace(in.AccountID); id != "" {
		path = ws(in.Workspace, "test-accounts", id)
	}
	var a account
	form := url.Values{"label": {in.Label}, "username": {in.Username}, "password": {in.Password}}
	if err := t.api.post(ctx, path, form, &a); err != nil {
		return nil, nil, err
	}
	return result(a)
}

func (t *tools) deleteTestAccount(ctx context.Context, _ *mcp.CallToolRequest, in accountIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "test-accounts", in.AccountID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.AccountID})
}

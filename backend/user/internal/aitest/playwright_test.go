package aitest

import (
	"strings"
	"testing"
)

// TestPlaywrightSpec builds a spec from a script with every tool, stored code (a body and a whole test) and an
// unrecorded case, and checks the lines.
func TestPlaywrightSpec(t *testing.T) {
	sc := &script{Check: "Drafts", Steps: []scriptStep{
		{Tool: "navigate", URL: "https://app.acme.test/login"},
		{Tool: "type", Target: &locator{CSS: `input[name="email"]`, Tag: "input", Name: "Email"}, Text: "qa@acme.test"},
		{Tool: "fill_password", Target: &locator{Tag: "input", Name: "Password"}, Account: "QA user"},
		{Tool: "click", Target: &locator{Tag: "button", Name: "Sign in"}},
		{Tool: "click", Target: &locator{Tag: "li", Name: "Inbox"}},
		{Tool: "click", Target: &locator{Tag: "button", Name: "Drafts {n}"}},
		{Tool: "select", Target: &locator{Tag: "select", Name: "Folder"}, Value: "drafts"},
		{Tool: "press", Key: "Enter"},
		{Tool: "wait", Seconds: 2},
		{Tool: "assert_text", Text: "My inbox"},
		{Tool: "assert_no_text", Text: `Draft "A"`},
	}}
	body, whole := "await page.goto('https://app.acme.test');", "test('Own', async ({ page }) => {\n  await page.goto('/');\n});"
	cs := []exportCase{
		{caseFields: caseFields{Title: "Sign in", Steps: []string{"Open the app", "Sign in\nas QA"}, Expected: "Inbox shows"}, script: sc},
		{caseFields: caseFields{Title: "Coded", Steps: []string{"x"}, Expected: "y"}, script: sc, playwright: &body},
		{caseFields: caseFields{Title: "Whole", Steps: []string{"x"}, Expected: "y"}, playwright: &whole},
		{caseFields: caseFields{Title: "Later", Steps: []string{"Open drafts"}, Expected: "Empty"}},
	}
	spec := playwrightSpec("VIS-3", "Drafts leak", []Site{{BaseURL: "https://app.acme.test"}}, cs)
	if spec != playwrightSpec("VIS-3", "Drafts leak", []Site{{BaseURL: "https://app.acme.test"}}, cs) {
		t.Error("not deterministic")
	}
	for _, want := range []string{
		"// VIS-3 Drafts leak\n",
		"// Exported by Taskwell from the issue's test cases. Test site: https://app.acme.test\n",
		"// Run: npm i -D @playwright/test && npx playwright test VIS-3.spec.ts\n",
		`// Passwords come from env vars: TASKWELL_PASSWORD_QA_USER (test account "QA user").` + "\n",
		"import { test, expect } from '@playwright/test';\n",
		`test.describe("VIS-3 Drafts leak", () => {` + "\n",
		`  test("Sign in", async ({ page }) => {` + "\n",
		"    // 1. Open the app\n",
		"    // 2. Sign in as QA\n", // a newline can't end the comment
		`    await page.goto("https://app.acme.test/login");` + "\n",
		`    await page.locator("input[name=\"email\"]").fill("qa@acme.test");` + "\n",
		`    await page.getByRole("textbox", { name: "Password", exact: true }).fill(process.env.TASKWELL_PASSWORD_QA_USER ?? '');` + "\n",
		`    await page.getByRole("button", { name: "Sign in", exact: true }).click();` + "\n",
		`    await page.locator("li", { hasText: "Inbox" }).first().click();` + "\n",
		`    await page.getByRole("button", { name: new RegExp("^Drafts \\d+$") }).click();` + "\n",
		`    await page.getByRole("combobox", { name: "Folder", exact: true }).selectOption("drafts");` + "\n",
		`    await page.keyboard.press("Enter");` + "\n",
		"    await page.waitForTimeout(2000);\n",
		`    await expect(page.getByText("My inbox").first()).toBeVisible();` + "\n",
		`    await expect(page.getByText("Draft \"A\"")).toHaveCount(0);` + "\n",
		"    // Expected: Inbox shows\n",
		`    await expect(page.getByText("Drafts").first()).toBeVisible();` + "\n  });\n",
		`  test("Coded", async ({ page }) => {` + "\n" + body + "\n  });\n",
		"\n" + whole + "\n",
		`  test.fixme("Later", async () => {` + "\n    // 1. Open drafts\n    // Expected: Empty\n  });\n});\n",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec lacks %q:\n%s", want, spec)
		}
	}
}

func TestCheckScript(t *testing.T) {
	hosts, labels := []string{"app.acme.test"}, []string{"QA user"}
	nav := scriptStep{Tool: "navigate", URL: "https://app.acme.test/login"}
	btn := &locator{Tag: "button", Name: "Go"}
	tests := []struct {
		name  string
		steps []scriptStep
		check string
		want  string
	}{
		{"valid", []scriptStep{nav, {Tool: "click", Target: btn}, {Tool: "fill_password", Target: btn, Account: "QA user"},
			{Tool: "assert_no_text", Text: "x"}, {Tool: "wait", Seconds: 5}, {Tool: "press", Key: "Enter"}}, "Done", ""},
		{"no steps", nil, "Done", "Add at least one step."},
		{"too many", make([]scriptStep, 61), "Done", "Use 60 steps or fewer."},
		{"first not navigate", []scriptStep{{Tool: "click", Target: btn}}, "Done", "Step 1: start with a navigate step."},
		{"no check", []scriptStep{nav}, " ", "Add a check: text that's on the page only when the expected result is met."},
		{"long check", []scriptStep{nav}, strings.Repeat("x", 201), "Check must be 200 characters or fewer."},
		{"other host", []scriptStep{nav, {Tool: "navigate", URL: "https://x.test"}}, "Done",
			"Step 2: https://x.test isn't a registered test site. Add it in Settings → AI testing."},
		{"not http", []scriptStep{{Tool: "navigate", URL: "javascript://app.acme.test"}}, "Done",
			"Step 1: javascript://app.acme.test isn't a registered test site. Add it in Settings → AI testing."},
		{"no target", []scriptStep{nav, {Tool: "click"}}, "Done", "Step 2: give the target's tag and its css or name."},
		{"tag only", []scriptStep{nav, {Tool: "type", Target: &locator{Tag: "input"}}}, "Done", "Step 2: give the target's tag and its css or name."},
		{"long css", []scriptStep{nav, {Tool: "click", Target: &locator{Tag: "a", CSS: strings.Repeat("a", 501)}}}, "Done",
			"Step 2: css must be 500 characters or fewer."},
		{"long text", []scriptStep{nav, {Tool: "type", Target: btn, Text: strings.Repeat("a", 1001)}}, "Done",
			"Step 2: text must be 1000 characters or fewer."},
		{"long value", []scriptStep{nav, {Tool: "select", Target: btn, Value: strings.Repeat("a", 201)}}, "Done",
			"Step 2: value must be 200 characters or fewer."},
		{"no key", []scriptStep{nav, {Tool: "press"}}, "Done", "Step 2: key must be 1–30 characters."},
		{"long wait", []scriptStep{nav, {Tool: "wait", Seconds: 6}}, "Done", "Step 2: wait 1 to 5 seconds."},
		{"empty assert", []scriptStep{nav, {Tool: "assert_text", Text: " "}}, "Done", "Step 2: text must be 1–200 characters."},
		{"unknown account", []scriptStep{nav, {Tool: "fill_password", Target: btn, Account: "Admin"}}, "Done",
			`Step 2: no test account is labelled "Admin". Add it in Settings → AI testing.`},
		{"unknown tool", []scriptStep{nav, {Tool: "hover"}}, "Done", `Step 2: unknown step "hover".`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := checkScript(script{Steps: tt.steps, Check: tt.check}, hosts, labels)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	// Each step keeps only its own fields: a password typed into a fill_password step is dropped.
	sc, _ := checkScript(script{Steps: []scriptStep{{Tool: "navigate", URL: "https://app.acme.test", Target: btn},
		{Tool: "fill_password", Target: btn, Account: "QA user", Text: "hunter2"}}, Check: "x"}, hosts, labels)
	if sc.Steps[0].Target != nil || sc.Steps[1].Text != "" {
		t.Errorf("cleaned %+v", sc.Steps)
	}
}

func TestCheckPlaywright(t *testing.T) {
	tests := []struct{ name, code, want string }{
		{"env password", "await page.getByLabel('Password').fill(process.env.TASKWELL_PASSWORD_QA ?? '');", ""},
		{"literal password", "await page.getByRole('textbox', { name: 'Password' }).fill('hunter2');",
			"Use process.env for passwords, never a literal."},
		{"template literal", "await page.locator('#password').fill(`x`);", "Use process.env for passwords, never a literal."},
		{"other literal", "await page.getByLabel('Email').fill('qa@acme.test');", ""},
		{"too long", strings.Repeat("é", 20001), "Use 20000 characters or fewer."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkPlaywright(tt.code); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

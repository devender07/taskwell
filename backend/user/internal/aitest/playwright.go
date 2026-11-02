package aitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// roles maps a recorded tag to the ARIA role Playwright's getByRole finds it by.
var roles = map[string]string{"button": "button", "a": "link", "input": "textbox", "textarea": "textbox", "select": "combobox"}

var nonConst = regexp.MustCompile(`[^A-Z0-9]+`)

// playwrightSpec writes the issue's cases as a Playwright test file: per case, the Playwright code an MCP client
// saved (wrapped in a test when it's only the body), else a test generated from its script, else a test.fixme with
// the steps as comments. Same input, same output; no AI.
func playwrightSpec(identifier, title string, sites []Site, cs []exportCase) string {
	var b strings.Builder
	line := func(indent int, format string, args ...any) {
		b.WriteString(strings.Repeat("  ", indent))
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}
	bases := make([]string, len(sites))
	for i, s := range sites {
		bases[i] = s.BaseURL
	}
	var envs []string
	seen := map[string]bool{}
	for _, c := range cs {
		if c.script == nil {
			continue
		}
		for _, s := range c.script.Steps {
			if s.Tool == "fill_password" && !seen[s.Account] {
				seen[s.Account] = true
				envs = append(envs, fmt.Sprintf("%s (test account %q)", passwordEnv(s.Account), s.Account))
			}
		}
	}
	line(0, "// %s %s", identifier, comment(title))
	line(0, "// Exported by Tether from the issue's test cases. Test site: %s", comment(strings.Join(bases, ", ")))
	line(0, "// Run: npm i -D @playwright/test && npx playwright test %s.spec.ts", identifier)
	if len(envs) > 0 {
		line(0, "// Passwords come from env vars: %s.", comment(strings.Join(envs, ", ")))
	}
	line(0, "import { test, expect } from '@playwright/test';")
	line(0, "")
	line(0, "test.describe(%s, () => {", js(identifier+" "+title))
	for k, c := range cs {
		if k > 0 {
			line(0, "")
		}
		if c.playwright != nil {
			code := strings.TrimSpace(*c.playwright)
			if strings.HasPrefix(code, "test(") {
				line(0, "%s", code)
				continue
			}
			line(1, "test(%s, async ({ page }) => {", js(c.Title))
			line(0, "%s", code)
			line(1, "});")
			continue
		}
		if c.script == nil {
			line(1, "test.fixme(%s, async () => {", js(c.Title))
		} else {
			line(1, "test(%s, async ({ page }) => {", js(c.Title))
		}
		for i, s := range c.Steps {
			line(2, "// %d. %s", i+1, comment(s))
		}
		if c.script != nil {
			for _, s := range c.script.Steps {
				line(2, "%s", stepJS(s))
			}
		}
		line(2, "// Expected: %s", comment(c.Expected))
		if c.script != nil {
			line(2, "await expect(page.getByText(%s).first()).toBeVisible();", js(c.script.Check))
		}
		line(1, "});")
	}
	line(0, "});")
	return b.String()
}

func stepJS(s scriptStep) string {
	switch s.Tool {
	case "navigate":
		return "await page.goto(" + js(s.URL) + ");"
	case "click":
		return "await " + locatorJS(s.Target) + ".click();"
	case "type":
		return "await " + locatorJS(s.Target) + ".fill(" + js(s.Text) + ");"
	case "select":
		return "await " + locatorJS(s.Target) + ".selectOption(" + js(s.Value) + ");"
	case "press":
		return "await page.keyboard.press(" + js(s.Key) + ");"
	case "wait":
		return fmt.Sprintf("await page.waitForTimeout(%d);", s.Seconds*1000)
	case "fill_password":
		return "await " + locatorJS(s.Target) + ".fill(process.env." + passwordEnv(s.Account) + " ?? '');"
	case "assert_text":
		return "await expect(page.getByText(" + js(s.Text) + ").first()).toBeVisible();"
	case "assert_no_text":
		return "await expect(page.getByText(" + js(s.Text) + ")).toHaveCount(0);"
	}
	return "// " + comment(s.describe())
}

// locatorJS finds the target as the replay does: its selector, else its role and name, else its tag and text.
func locatorJS(l *locator) string {
	if l == nil {
		return "page"
	}
	if l.CSS != "" {
		return "page.locator(" + js(l.CSS) + ")"
	}
	if role := roles[l.Tag]; role != "" {
		return "page.getByRole(" + js(role) + ", { name: " + js(l.Name) + ", exact: true })"
	}
	return "page.locator(" + js(l.Tag) + ", { hasText: " + js(l.Name) + " }).first()"
}

// passwordEnv is the env var holding the account's password: TETHER_PASSWORD_ and the label as a constant.
func passwordEnv(label string) string {
	return "TETHER_PASSWORD_" + strings.Trim(nonConst.ReplaceAllString(strings.ToUpper(label), "_"), "_")
}

// js is s as a JS string literal.
func js(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

// comment keeps s on one line, so text from a case can't end a // comment and become code.
func comment(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", " ", " ", " ", " ").Replace(s)
}

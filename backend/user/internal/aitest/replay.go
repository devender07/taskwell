package aitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// script is a case recorded for replay without Claude (by a passed AI run, or written by an MCP client and saved
// through the API): the actions, each element found again by a selector or its tag and name, texts that must or
// mustn't be on the page along the way, and a text that's on the page only when the expected result is met.
type script struct {
	Steps []scriptStep `json:"steps"`
	Check string       `json:"check"`
}

type scriptStep struct {
	Tool    string   `json:"tool"` // navigate, click, type, select, press, wait, fill_password, assert_text, assert_no_text
	URL     string   `json:"url,omitempty"`
	Target  *locator `json:"target,omitempty"`
	Text    string   `json:"text,omitempty"` // typed or asserted text; never a password (fill_password keeps the account label)
	Value   string   `json:"value,omitempty"`
	Key     string   `json:"key,omitempty"`
	Seconds int      `json:"seconds,omitempty"`
	Account string   `json:"account,omitempty"`
}

type locator struct {
	CSS  string `json:"css"` // a selector matching only this element when it was recorded, or ""
	Tag  string `json:"tag"`
	Name string `json:"name"`
}

// locateJS describes the element with the ref so a replay can find it again: a unique selector from a stable
// attribute (ids with long digit runs look generated, so they're skipped), plus its tag and name.
func locateJS(ref int) string {
	return `(() => {` + elementsJS + `
  const el = document.querySelector('[data-tether-ref="` + strconv.Itoa(ref) + `"]'); if (!el) return null;
  const tag = el.tagName.toLowerCase();
  const only = s => { try { return document.querySelectorAll(s).length === 1 ? s : '' } catch { return '' } };
  let css = el.id && !/\d{3,}/.test(el.id) ? only('#' + CSS.escape(el.id)) : '';
  for (const a of ['data-testid', 'data-test', 'name', 'aria-label', 'placeholder']) {
    const v = el.getAttribute(a); if (!css && v) css = only(tag + '[' + a + '=' + JSON.stringify(v) + ']');
  }
  if (!css && !nameOf(el)) { // nothing to find it by but where it is: its path from the nearest stable id
    const path = []; let e = el;
    for (; e && e !== document.body && !(e.id && !/\d{3,}/.test(e.id)); e = e.parentElement)
      path.unshift(e.tagName.toLowerCase() + ':nth-child(' + ([...e.parentElement.children].indexOf(e) + 1) + ')');
    css = only((e && e !== document.body ? '#' + CSS.escape(e.id) : 'body') + ' > ' + path.join(' > '));
  }
  return {css, tag, name: nameOf(el).replace(/\d+/g, '{n}')}; // counts change ("Drafts 14"): never record them
})()`
}

// replayRef is the ref resolveJS gives the element it finds; stateJS numbers from 1.
const replayRef = 0

// resolveJS finds the recorded element (its selector, else the first visible element with its tag and name) and
// tags it with replayRef.
func resolveJS(l *locator) string {
	b, _ := json.Marshal(l)
	return `(() => {` + elementsJS + `
  const L = ` + string(b) + `;
  document.querySelectorAll('[data-tether-ref="` + strconv.Itoa(replayRef) + `"]').forEach(e => e.removeAttribute('data-tether-ref'));
  let found = null;
  if (L.css) { const m = [...document.querySelectorAll(L.css)].filter(visible); if (m.length === 1) found = m[0]; }
  // ponytail: first match wins, so "Page 1" and "Page 2" both resolve to the first; record a css target if that bites
  const bare = s => s.replace(/\d+/g, '{n}');
  if (!found) found = [...document.querySelectorAll(sel)].find(el => visible(el) && el.tagName.toLowerCase() === L.tag && bare(nameOf(el)) === bare(L.name)) || null;
  if (!found) return false;
  found.setAttribute('data-tether-ref', '` + strconv.Itoa(replayRef) + `'); return true;
})()`
}

// locate records the element with the ref; nil when it's gone.
func (p *page) locate(ctx context.Context, ref int) *locator {
	var l *locator
	if p.eval(ctx, locateJS(ref), &l) != nil {
		return nil
	}
	return l
}

// waitFor polls js (which returns a boolean) for up to d, as pages render after a click or a navigation.
func (p *page) waitFor(ctx context.Context, js string, d time.Duration) (bool, error) {
	end := time.Now().Add(d)
	for {
		var ok bool
		if err := p.eval(ctx, js, &ok); err != nil {
			return false, err
		}
		if ok || time.Now().After(end) {
			return ok, nil
		}
		if !sleep(ctx, 250*time.Millisecond) {
			return false, ctx.Err()
		}
	}
}

func hasTextJS(text string) string {
	b, _ := json.Marshal(text)
	return `(() => !!document.body && document.body.innerText.includes(` + string(b) + `))()`
}

func noTextJS(text string) string {
	b, _ := json.Marshal(text)
	return `(() => !document.body || !document.body.innerText.includes(` + string(b) + `))()`
}

// replay runs the case's script without Claude, logging each step with a screenshot. why is "" when it passed;
// otherwise it says which step's element isn't there, which assertion failed or that the check text didn't
// appear, a last step logs it with the page's screenshot (plus then, when Claude takes over the case), and repro
// is the steps up to the failing one. err is a broken browser or a stopped run.
func (r *Runner) replay(ctx context.Context, j *job, x Result, sc *script, logged *int, then string) (why string, repro []string, err error) {
	db := context.WithoutCancel(ctx)
	c, err := r.browser(ctx)
	if err != nil {
		return "", nil, err
	}
	pg, err := c.newPage(ctx, r.o.ProxyURL, j.runID, j.token, r.o.IgnoreCertErrors, func(f []byte) { r.o.Live.publish(j.runID, f) })
	if err != nil {
		return "", nil, err
	}
	defer pg.close()
	hosts := siteHosts(j.sites)
	fail := func(why string, done int) (string, []string, error) {
		for _, s := range sc.Steps[:done] {
			repro = append(repro, s.describe())
		}
		return why, repro, r.logStep(ctx, pg, j, x, logged, strings.TrimSpace(why+" "+then))
	}
	for i, s := range sc.Steps {
		if cancelled, err := r.model.cancelRequested(db, j.runID); err != nil || cancelled {
			if err == nil {
				err = errCancelled
			}
			return "", nil, err
		}
		action := "replay " + s.describe()
		if s.Target != nil {
			found, err := pg.waitFor(ctx, resolveJS(s.Target), 5*time.Second)
			if err != nil {
				return "", nil, err
			}
			if !found {
				return fail(fmt.Sprintf("The recorded step \"%s\" no longer matches the page.", s.describe()), i+1)
			}
		}
		switch s.Tool {
		case "navigate":
			u, perr := url.Parse(s.URL)
			if perr != nil || !allowedHost(u.Hostname(), hosts) {
				return fail("A recorded URL isn't a registered test site any more.", i+1)
			}
			err = pg.navigate(ctx, s.URL)
		case "click":
			err = pg.click(ctx, replayRef)
		case "type":
			err = pg.typeText(ctx, replayRef, s.Text)
		case "select":
			err = pg.selectOption(ctx, replayRef, s.Value)
		case "press":
			err = pg.press(ctx, s.Key)
		case "wait":
			sleep(ctx, time.Duration(min(max(s.Seconds, 1), 5))*time.Second)
		case "fill_password":
			var pw string
			if pw, err = r.accountPassword(ctx, j, pg, s.Account); err == nil {
				err = pg.fillPassword(ctx, replayRef, pw)
			}
		case "assert_text", "assert_no_text":
			js, msg := hasTextJS(s.Text), "Expected \""+s.Text+"\" on the page."
			if s.Tool == "assert_no_text" {
				js, msg = noTextJS(s.Text), "Expected \""+s.Text+"\" not to be on the page."
			}
			found, err := pg.waitFor(ctx, js, 5*time.Second)
			if err != nil {
				return "", nil, err
			}
			if !found {
				return fail(msg, i+1)
			}
		}
		var pe pageError
		if errors.As(err, &pe) {
			return fail(fmt.Sprintf("The recorded step \"%s\" failed: %s", s.describe(), pe), i+1)
		}
		if err != nil {
			return "", nil, err
		}
		if err := r.logStep(ctx, pg, j, x, logged, action); err != nil {
			return "", nil, err
		}
	}
	found, err := pg.waitFor(ctx, hasTextJS(sc.Check), 5*time.Second)
	if err != nil {
		return "", nil, err
	}
	if !found {
		return fail(fmt.Sprintf("After the recorded steps, %q isn't on the page.", sc.Check), len(sc.Steps))
	}
	return "", nil, nil
}

// logStep adds a step with the page's URL and a screenshot to the result's log.
func (r *Runner) logStep(ctx context.Context, pg *page, j *job, x Result, logged *int, action string) error {
	var u string
	_ = pg.eval(ctx, `location.href`, &u)
	shot, err := pg.screenshot(ctx)
	if err != nil {
		return err
	}
	if err := r.model.addStep(context.WithoutCancel(ctx), j, r.o.UploadDir, x.ID, *logged, action, u, shot); err != nil {
		return err
	}
	*logged++
	return nil
}

// describe is the step as the log shows it, never the typed text (an asserted text is shown).
func (s scriptStep) describe() string {
	el := ""
	if s.Target != nil {
		el = fmt.Sprintf("%s %q", s.Target.Tag, s.Target.Name)
	}
	switch s.Tool {
	case "navigate":
		return "navigate " + s.URL
	case "click":
		return "click " + el
	case "type":
		return "type into " + el
	case "select":
		return fmt.Sprintf("select %q in %s", s.Value, el)
	case "press":
		return "press " + s.Key
	case "wait":
		return fmt.Sprintf("wait %ds", s.Seconds)
	case "fill_password":
		return fmt.Sprintf("fill the password of %q into %s", s.Account, el)
	case "assert_text":
		return fmt.Sprintf("check %q is on the page", s.Text)
	case "assert_no_text":
		return fmt.Sprintf("check %q isn't on the page", s.Text)
	}
	return strings.TrimSpace(s.Tool + " " + el)
}

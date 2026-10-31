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

// script is a passed AI case recorded for replay without Claude: the actions that worked, each element found again
// by a selector or its tag and name, and a text that's on the page only when the expected result is met.
type script struct {
	Steps []scriptStep `json:"steps"`
	Check string       `json:"check"`
}

type scriptStep struct {
	Tool    string   `json:"tool"` // navigate, click, type, select, press, wait, fill_password
	URL     string   `json:"url,omitempty"`
	Target  *locator `json:"target,omitempty"`
	Text    string   `json:"text,omitempty"` // typed text; never a password (fill_password keeps the account label)
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
  return {css, tag, name: nameOf(el)};
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
  if (!found) found = [...document.querySelectorAll(sel)].find(el => visible(el) && el.tagName.toLowerCase() === L.tag && nameOf(el) === L.name) || null;
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

// replay runs the case's script without Claude, logging each step with a screenshot. ok is false with why when a
// step's element isn't there or the check text doesn't appear; err is a broken browser or a stopped run.
func (r *Runner) replay(ctx context.Context, j *job, x Result, sc *script, logged *int) (ok bool, why string, err error) {
	db := context.WithoutCancel(ctx)
	c, err := r.browser(ctx)
	if err != nil {
		return false, "", err
	}
	pg, err := c.newPage(ctx, r.o.ProxyURL, j.runID, j.token, func(f []byte) { r.o.Live.publish(j.runID, f) })
	if err != nil {
		return false, "", err
	}
	defer pg.close()
	hosts := make([]string, len(j.sites))
	for i, s := range j.sites {
		hosts[i] = s.Host
	}
	for _, s := range sc.Steps {
		if cancelled, err := r.model.cancelRequested(db, j.runID); err != nil || cancelled {
			if err == nil {
				err = errCancelled
			}
			return false, "", err
		}
		action := "replay " + s.describe()
		if s.Target != nil {
			found, err := pg.waitFor(ctx, resolveJS(s.Target), 5*time.Second)
			if err != nil {
				return false, "", err
			}
			if !found {
				return false, fmt.Sprintf("The recorded step \"%s\" no longer matches the page.", s.describe()), nil
			}
		}
		switch s.Tool {
		case "navigate":
			u, perr := url.Parse(s.URL)
			if perr != nil || !allowedHost(u.Hostname(), hosts) {
				return false, "A recorded URL isn't a registered test site any more.", nil
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
			if pw, err = r.accountPassword(ctx, j, s.Account); err == nil {
				err = pg.typeText(ctx, replayRef, pw)
			}
		}
		var pe pageError
		if errors.As(err, &pe) {
			return false, fmt.Sprintf("The recorded step \"%s\" failed: %s", s.describe(), pe), nil
		}
		if err != nil {
			return false, "", err
		}
		if err := r.logStep(ctx, pg, j, x, logged, action); err != nil {
			return false, "", err
		}
	}
	found, err := pg.waitFor(ctx, hasTextJS(sc.Check), 5*time.Second)
	if err != nil {
		return false, "", err
	}
	if !found {
		return false, fmt.Sprintf("After the recorded steps, %q isn't on the page.", sc.Check), nil
	}
	return true, "", nil
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

// describe is the step as the log shows it, never the typed text.
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
	}
	return strings.TrimSpace(s.Tool + " " + el)
}

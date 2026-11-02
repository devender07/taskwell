package aitest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// cdp speaks the Chrome DevTools Protocol over one browser WebSocket; pages are flattened sessions on it.
// ponytail: raw CDP for the ~15 methods below; switch to chromedp if the method list grows past a page.
type cdp struct {
	ws   *websocket.Conn
	next atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan cdpMsg
	sessions map[string]func(method string, params json.RawMessage)
	err      error // set when the socket is gone
}

type cdpMsg struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    any             `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// dialChrome connects to the browser at chromeURL (http://chrome:9222). Chrome only answers DevTools requests whose
// Host is an IP or localhost, so a name is resolved first.
func dialChrome(ctx context.Context, chromeURL string) (*cdp, error) {
	u, err := url.Parse(chromeURL)
	if err != nil {
		return nil, fmt.Errorf("CHROME_URL: %w", err)
	}
	host, port := u.Hostname(), u.Port()
	if net.ParseIP(host) == nil {
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("resolve chrome: %w", err)
		}
		host = ips[0]
	}
	addr := net.JoinHostPort(host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/json/version", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chrome version: %w", err)
	}
	defer resp.Body.Close()
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("chrome version: %w", err)
	}
	wsURL, err := url.Parse(v.WS)
	if err != nil {
		return nil, fmt.Errorf("chrome websocket url: %w", err)
	}
	wsURL.Host = addr
	ws, _, err := websocket.Dial(ctx, wsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("chrome websocket: %w", err)
	}
	ws.SetReadLimit(64 << 20) // screenshots
	c := &cdp{ws: ws, pending: map[int64]chan cdpMsg{}, sessions: map[string]func(string, json.RawMessage){}}
	go c.read()
	return c, nil
}

func (c *cdp) read() {
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			c.err = fmt.Errorf("chrome connection: %w", err)
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var m struct {
			cdpMsg
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		c.mu.Lock()
		if m.ID != 0 {
			if ch := c.pending[m.ID]; ch != nil {
				delete(c.pending, m.ID)
				ch <- m.cdpMsg
			}
		} else if h := c.sessions[m.SessionID]; h != nil {
			go h(m.Method, m.Params) // handlers call back into c, so never on this goroutine
		}
		c.mu.Unlock()
	}
}

func (c *cdp) broken() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *cdp) close() { _ = c.ws.CloseNow() }

// call sends method to the session ("" for the browser) and decodes the result into out (when not nil).
func (c *cdp) call(ctx context.Context, session, method string, params, out any) error {
	id := c.next.Add(1)
	ch := make(chan cdpMsg, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	data, err := json.Marshal(cdpMsg{ID: id, Method: method, SessionID: session, Params: params})
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case m, ok := <-ch:
		switch {
		case !ok:
			return fmt.Errorf("%s: chrome connection closed", method)
		case m.Error != nil:
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		case out != nil:
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// page is one tab in its own browser context (an empty profile), proxied through the egress proxy as the run.
type page struct {
	c                            *cdp
	contextID, targetID, session string

	frame func([]byte)

	mu        sync.Mutex
	requested map[string]bool // hosts the page asked for, so the browser's own traffic stays out of the step log
}

const viewportW, viewportH = 1280, 800

// newPage opens a tab in a fresh browser context whose requests go through proxyURL, answering the proxy's
// challenge with the run's id and token, and passes each screencast frame (JPEG) to frame.
func (c *cdp) newPage(ctx context.Context, proxyURL, runID, token string, insecure bool, frame func([]byte)) (*page, error) {
	p := &page{c: c, requested: map[string]bool{}, frame: frame}
	if err := c.call(ctx, "", "Target.createBrowserContext", map[string]any{
		"disposeOnDetach": true, "proxyServer": proxyURL, "proxyBypassList": "<-loopback>",
	}, &struct {
		ID *string `json:"browserContextId"`
	}{&p.contextID}); err != nil {
		return nil, err
	}
	err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": p.contextID},
		&struct {
			ID *string `json:"targetId"`
		}{&p.targetID})
	if err == nil {
		err = c.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": p.targetID, "flatten": true},
			&struct {
				ID *string `json:"sessionId"`
			}{&p.session})
	}
	if err != nil {
		p.close()
		return nil, err
	}
	c.mu.Lock()
	c.sessions[p.session] = func(method string, params json.RawMessage) { p.event(runID, token, method, params) }
	c.mu.Unlock()
	steps := []struct {
		method string
		params any
	}{
		{"Fetch.enable", map[string]any{"handleAuthRequests": true, "patterns": []map[string]string{{"urlPattern": "*"}}}},
		{"Page.enable", nil},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": viewportW, "height": viewportH, "deviceScaleFactor": 1, "mobile": false}},
		{"Browser.setDownloadBehavior", map[string]any{"behavior": "deny", "browserContextId": p.contextID}},
		{"Page.startScreencast", map[string]any{"format": "jpeg", "quality": 50, "maxWidth": viewportW, "maxHeight": viewportH}},
	}
	if insecure {
		steps = append(steps, struct {
			method string
			params any
		}{"Security.setIgnoreCertificateErrors", map[string]any{"ignore": true}})
	}
	for _, s := range steps {
		session := p.session
		if s.method == "Browser.setDownloadBehavior" {
			session = ""
		}
		if err := c.call(ctx, session, s.method, s.params, nil); err != nil {
			p.close()
			return nil, err
		}
	}
	return p, nil
}

// event answers the page's paused requests, the proxy's auth challenge and JavaScript dialogs.
func (p *page) event(runID, token, method string, params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL string `json:"url"`
		} `json:"request"`
		AuthChallenge struct {
			Source string `json:"source"`
		} `json:"authChallenge"`
		Data      []byte `json:"data"` // screencast frame, base64 in the JSON
		SessionID int    `json:"sessionId"`
	}
	_ = json.Unmarshal(params, &e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch method {
	case "Fetch.requestPaused":
		if u, err := url.Parse(e.Request.URL); err == nil {
			p.mu.Lock()
			p.requested[strings.ToLower(u.Hostname())] = true
			p.mu.Unlock()
		}
		_ = p.c.call(ctx, p.session, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID}, nil)
	case "Fetch.authRequired":
		resp := map[string]any{"response": "CancelAuth"} // the site's own HTTP auth isn't supported
		if e.AuthChallenge.Source == "Proxy" {
			resp = map[string]any{"response": "ProvideCredentials", "username": runID, "password": token}
		}
		_ = p.c.call(ctx, p.session, "Fetch.continueWithAuth", map[string]any{"requestId": e.RequestID, "authChallengeResponse": resp}, nil)
	case "Page.screencastFrame":
		p.frame(e.Data)
		_ = p.c.call(ctx, p.session, "Page.screencastFrameAck", map[string]any{"sessionId": e.SessionID}, nil)
	case "Page.javascriptDialogOpening":
		_ = p.c.call(ctx, p.session, "Page.handleJavaScriptDialog", map[string]any{"accept": true}, nil)
	}
}

// asked reports whether the page requested host.
func (p *page) asked(host string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requested[host]
}

// close disposes of the page's browser context (cookies, storage and all).
func (p *page) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.c.mu.Lock()
	delete(p.c.sessions, p.session)
	p.c.mu.Unlock()
	if p.contextID != "" {
		_ = p.c.call(ctx, "", "Target.disposeBrowserContext", map[string]any{"browserContextId": p.contextID}, nil)
	}
}

// eval runs a JavaScript expression in the page and decodes its value into out (when not nil).
func (p *page) eval(ctx context.Context, expr string, out any) error {
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := p.c.call(ctx, p.session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, &r); err != nil {
		return err
	}
	if r.Exception != nil {
		return fmt.Errorf("page script: %s", r.Exception.Text)
	}
	if out != nil && len(r.Result.Value) > 0 {
		return json.Unmarshal(r.Result.Value, out)
	}
	return nil
}

// settle waits up to 10s for the page to finish loading, then a moment for scripts to render.
func (p *page) settle(ctx context.Context) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var state string
		if err := p.eval(ctx, "document.readyState", &state); err == nil && state == "complete" {
			break
		}
		if !sleep(ctx, 200*time.Millisecond) {
			return
		}
	}
	sleep(ctx, 400*time.Millisecond)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *page) navigate(ctx context.Context, u string) error {
	var r struct {
		ErrorText string `json:"errorText"`
	}
	if err := p.c.call(ctx, p.session, "Page.navigate", map[string]any{"url": u}, &r); err != nil {
		return err
	}
	if r.ErrorText != "" {
		return pageError("Couldn't load the page: " + r.ErrorText)
	}
	p.settle(ctx)
	return nil
}

// pageError is a failed action Claude should hear about (the tool result), not a broken browser.
type pageError string

func (e pageError) Error() string { return string(e) }

// center scrolls the element with the ref into view and returns its center.
func (p *page) center(ctx context.Context, ref int) (x, y float64, err error) {
	var pt *struct{ X, Y float64 }
	err = p.eval(ctx, `(() => { const el = document.querySelector('[data-tether-ref="`+strconv.Itoa(ref)+`"]');
		if (!el) return null; el.scrollIntoView({block: 'center', inline: 'center'});
		const r = el.getBoundingClientRect(); return {X: r.x + r.width / 2, Y: r.y + r.height / 2}; })()`, &pt)
	if err != nil {
		return 0, 0, err
	}
	if pt == nil {
		return 0, 0, pageError(fmt.Sprintf("No element [%d] on the page. Use a ref from the latest page state.", ref))
	}
	return pt.X, pt.Y, nil
}

func (p *page) click(ctx context.Context, ref int) error {
	x, y, err := p.center(ctx, ref)
	if err != nil {
		return err
	}
	for _, typ := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		if err := p.c.call(ctx, p.session, "Input.dispatchMouseEvent", map[string]any{
			"type": typ, "x": x, "y": y, "button": "left", "clickCount": 1,
		}, nil); err != nil {
			return err
		}
	}
	p.settle(ctx)
	return nil
}

// typeText focuses the element, selects what's in it and types text over it.
func (p *page) typeText(ctx context.Context, ref int, text string) error {
	x, y, err := p.center(ctx, ref)
	if err != nil {
		return err
	}
	for _, typ := range []string{"mousePressed", "mouseReleased"} {
		if err := p.c.call(ctx, p.session, "Input.dispatchMouseEvent", map[string]any{
			"type": typ, "x": x, "y": y, "button": "left", "clickCount": 1,
		}, nil); err != nil {
			return err
		}
	}
	if err := p.eval(ctx, `(() => { const el = document.querySelector('[data-tether-ref="`+strconv.Itoa(ref)+`"]');
		if (el && el.select) el.select(); else document.execCommand('selectAll'); })()`, nil); err != nil {
		return err
	}
	return p.c.call(ctx, p.session, "Input.insertText", map[string]any{"text": text}, nil)
}

func (p *page) selectOption(ctx context.Context, ref int, value string) error {
	v, _ := json.Marshal(value)
	var ok bool
	err := p.eval(ctx, `(() => { const el = document.querySelector('[data-tether-ref="`+strconv.Itoa(ref)+`"]');
		if (!el || el.tagName !== 'SELECT') return false; const v = `+string(v)+`;
		const o = [...el.options].find(o => o.value === v || o.text.trim() === v); if (!o) return false;
		el.value = o.value; el.dispatchEvent(new Event('input', {bubbles: true})); el.dispatchEvent(new Event('change', {bubbles: true}));
		return true; })()`, &ok)
	if err == nil && !ok {
		err = pageError(fmt.Sprintf("[%d] isn't a select with that option.", ref))
	}
	return err
}

// keys are what press accepts: CDP key, code, Windows key code and the text it types.
var keys = map[string]struct {
	code string
	vk   int
	text string
}{
	"Enter": {"Enter", 13, "\r"}, "Tab": {"Tab", 9, ""}, "Escape": {"Escape", 27, ""}, "Backspace": {"Backspace", 8, ""},
	"Delete": {"Delete", 46, ""}, "ArrowUp": {"ArrowUp", 38, ""}, "ArrowDown": {"ArrowDown", 40, ""},
	"ArrowLeft": {"ArrowLeft", 37, ""}, "ArrowRight": {"ArrowRight", 39, ""}, "Space": {"Space", 32, " "},
	"PageDown": {"PageDown", 34, ""}, "PageUp": {"PageUp", 33, ""}, "Home": {"Home", 36, ""}, "End": {"End", 35, ""},
}

func (p *page) press(ctx context.Context, key string) error {
	k, ok := keys[key]
	if !ok {
		return pageError("Unknown key. Use one of Enter, Tab, Escape, Backspace, Delete, Space, the arrow keys, PageUp, PageDown, Home or End.")
	}
	name := key
	if key == "Space" {
		name = " "
	}
	down := map[string]any{"type": "keyDown", "key": name, "code": k.code, "windowsVirtualKeyCode": k.vk}
	if k.text != "" {
		down["text"] = k.text
	}
	if err := p.c.call(ctx, p.session, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	if err := p.c.call(ctx, p.session, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": name, "code": k.code, "windowsVirtualKeyCode": k.vk}, nil); err != nil {
		return err
	}
	p.settle(ctx)
	return nil
}

// screenshot is the viewport as a JPEG.
func (p *page) screenshot(ctx context.Context) ([]byte, error) {
	var r struct {
		Data string `json:"data"`
	}
	if err := p.c.call(ctx, p.session, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 60}, &r); err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return nil, errors.New("screenshot isn't base64")
	}
	return b, nil
}

type pageState struct {
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Text     string   `json:"text"`
	Elements []string `json:"elements"`
}

// elementsJS declares what stateJS, locateJS and resolveJS share: which elements are interactive, whether one is
// visible, and its accessible-ish name.
const elementsJS = `
  const sel = 'a[href],button,input,select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=option],[role=switch],[contenteditable=""],[contenteditable=true],[onclick]';
  const clean = s => String(s || '').trim().replace(/\s+/g, ' ').slice(0, 80);
  const visible = el => { const r = el.getBoundingClientRect(), st = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && st.visibility !== 'hidden' && st.display !== 'none' && el.type !== 'hidden'; };
  const nameOf = el => { const tag = el.tagName.toLowerCase(), type = (el.getAttribute('type') || 'text').toLowerCase();
    return clean(el.getAttribute('aria-label') || (el.labels && el.labels[0] && el.labels[0].innerText) || el.placeholder ||
      el.innerText || (tag === 'input' && (type === 'submit' || type === 'button') ? el.value : '') || el.title || el.name); };
`

// stateJS tags the visible interactive elements with data-tether-ref and lists them, never a password's value.
const stateJS = `(() => {` + elementsJS + `
  document.querySelectorAll('[data-tether-ref]').forEach(e => e.removeAttribute('data-tether-ref'));
  const out = []; let n = 0;
  for (const el of document.querySelectorAll(sel)) {
    if (n >= 200) break;
    if (!visible(el)) continue;
    n++; el.setAttribute('data-tether-ref', String(n));
    const tag = el.tagName.toLowerCase(), type = (el.getAttribute('type') || 'text').toLowerCase();
    const kind = el.getAttribute('role') || (tag === 'input' ? 'input type=' + type : tag);
    const name = nameOf(el);
    let line = '[' + n + '] ' + kind + ' "' + name + '"';
    if (tag === 'textarea' || (tag === 'input' && !['password', 'checkbox', 'radio', 'submit', 'button', 'file'].includes(type))) line += ' value="' + clean(el.value) + '"';
    if (type === 'checkbox' || type === 'radio') line += el.checked ? ' checked' : ' unchecked';
    if (tag === 'select') line += ' selected="' + clean(el.selectedOptions[0] && el.selectedOptions[0].text) + '" options=' + JSON.stringify([...el.options].slice(0, 30).map(o => clean(o.text)));
    if (el.disabled) line += ' disabled';
    if (tag === 'a') line += ' href="' + String(el.getAttribute('href')).slice(0, 120) + '"';
    out.push(line);
  }
  return {url: location.href, title: document.title, text: document.body ? document.body.innerText.slice(0, 6000) : '', elements: out};
})()`

func (p *page) state(ctx context.Context) (pageState, error) {
	var s pageState
	err := p.eval(ctx, stateJS, &s)
	return s, err
}

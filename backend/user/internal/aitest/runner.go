package aitest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

const (
	runTimeout      = 10 * time.Minute
	maxStepsPerCase = 30
	msgRestart      = "The test runner restarted. Run it again."
)

var (
	errCancelled = errors.New("run cancelled")
	errStopping  = errors.New("tester stopping")
	errLimitHit  = errors.New("monthly limit reached")
)

// RunnerOptions configure the tester.
type RunnerOptions struct {
	Send      sendFunc // nil without ANTHROPIC_API_KEY: the tester stays idle and the API answers 503
	Key       []byte   // from Key(TEST_SECRET_KEY), opens test account passwords
	UploadDir string
	ChromeURL string // the browser's DevTools endpoint, http://chrome:9222
	ProxyURL  string // how Chrome reaches Proxy, http://tester:3128
	Browsers  int    // runs at once
	Proxy     *Proxy
	Live      *Live  // fans the page's screencast out to viewers
	LiveURL   string // how the user API reaches Live, stored on each claimed run
}

// Runner claims queued AI runs and runs them: Claude writes the cases when the issue needs them, then drives a
// page per case through the egress proxy until it calls finish.
type Runner struct {
	model *model
	cfg   *sysconfig.Store
	o     RunnerOptions

	mu     sync.Mutex
	chrome *cdp
}

func NewRunner(db *pgxpool.Pool, cfg *sysconfig.Store, o RunnerOptions) *Runner {
	return &Runner{model: &model{db: db}, cfg: cfg, o: o}
}

// Run takes runs until stop ends, then lets each running one finish its current step and end it as restarted.
func (r *Runner) Run(stop context.Context) {
	if r.o.Send == nil {
		slog.Warn("ANTHROPIC_API_KEY isn't set: the tester won't check in or take AI runs")
		<-stop.Done()
		return
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(r.o.Browsers, 1))
	beat, poll := time.NewTicker(30*time.Second), time.NewTicker(2*time.Second)
	defer beat.Stop()
	defer poll.Stop()
	r.beat(stop)
	for {
		select {
		case <-stop.Done():
			wg.Wait()
			r.mu.Lock()
			if r.chrome != nil {
				r.chrome.close()
			}
			r.mu.Unlock()
			return
		case <-beat.C:
			r.beat(stop)
		case <-poll.C:
		}
		for len(slots) < cap(slots) {
			limit, err := r.cfg.GetInt(stop, "ai_test_workspace_concurrency")
			if err != nil || limit < 1 {
				limit = 2
			}
			id, err := r.model.claim(stop, limit, r.o.LiveURL)
			if err != nil && stop.Err() == nil {
				slog.Error("claim failed", "err", err)
			}
			if id == "" {
				break
			}
			slots <- struct{}{}
			wg.Go(func() {
				defer func() { <-slots }()
				r.execute(stop, id)
			})
		}
	}
}

// beat checks in (the API takes AI runs only while a tester has) and ends runs a crashed tester left behind.
func (r *Runner) beat(ctx context.Context) {
	if err := r.model.heartbeat(ctx); err != nil && ctx.Err() == nil {
		slog.Error("heartbeat failed", "err", err)
	}
	if err := r.model.failStale(ctx); err != nil && ctx.Err() == nil {
		slog.Error("stale runs", "err", err)
	}
}

func (r *Runner) execute(stop context.Context, runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	slog.Info("ai run started", "run", runID)
	status, msg := r.runJob(ctx, stop, runID)
	r.o.Live.end(runID)
	if err := r.model.finishAI(context.Background(), runID, status, msg); err != nil {
		slog.Error("finish ai run", "run", runID, "err", err)
		return
	}
	slog.Info("ai run finished", "run", runID, "status", status)
}

// runJob runs the claimed run and returns how it ends.
func (r *Runner) runJob(ctx, stop context.Context, runID string) (status, msg string) {
	db := context.WithoutCancel(ctx) // writes still land after the time limit
	j, err := r.model.job(db, runID)
	if err != nil {
		slog.Error("ai run", "run", runID, "err", err)
		return "error", "Something went wrong. Run it again."
	}
	var generated []caseFields
	if j.status == "generating" {
		m, err := r.call(ctx, stop, j, generateParams(j))
		if err != nil {
			_, status, msg := r.failure(runID, err)
			return status, msg
		}
		if generated, err = parseCases(m); errors.Is(err, errRefused) {
			return "error", "Claude declined to write test cases for this issue."
		} else if err != nil {
			slog.Error("ai run", "run", runID, "err", err)
			return "error", "Claude's test cases couldn't be read. Run it again."
		}
	}
	if err := r.model.startCases(db, j, generated); err != nil {
		slog.Error("ai run", "run", runID, "err", err)
		return "error", "Something went wrong. Run it again."
	}
	hosts := make([]string, len(j.sites))
	for i, s := range j.sites {
		hosts[i] = s.Host
	}
	r.o.Proxy.open(runID, j.token, hosts)
	defer r.o.Proxy.close(runID)
	results, err := r.model.pending(db, runID)
	if err != nil {
		slog.Error("ai run", "run", runID, "err", err)
		return "error", "Something went wrong. Run it again."
	}
	for _, x := range results {
		if err := r.model.setResult(db, j, x.ID, "running", nil, nil); err != nil {
			slog.Error("ai run", "run", runID, "err", err)
			return "error", "Something went wrong. Run it again."
		}
		pass, summary, repro, err := r.runCase(ctx, stop, j, x)
		if err != nil {
			caseMsg, status, msg := r.failure(runID, err)
			if caseMsg != "" {
				if err := r.model.setResult(db, j, x.ID, "error", &caseMsg, nil); err != nil {
					slog.Error("ai run", "run", runID, "err", err)
				}
			}
			if status == "" {
				break // the time limit: the run ends on its results so far
			}
			return status, msg
		}
		if err := r.model.setResult(db, j, x.ID, pass, &summary, repro); err != nil {
			slog.Error("ai run", "run", runID, "err", err)
			return "error", "Something went wrong. Run it again."
		}
	}
	status, msg, err = r.model.aiOutcome(db, runID)
	if err != nil {
		slog.Error("ai run", "run", runID, "err", err)
		return "error", "Something went wrong. Run it again."
	}
	return status, msg
}

// failure maps a run-ending err to the current case's message ("" leaves it to be skipped) and the run's status
// and message; status "" means the time limit, after which the run ends on its results.
func (r *Runner) failure(runID string, err error) (caseMsg, status, msg string) {
	var ce errClaude
	switch {
	case errors.Is(err, errCancelled):
		return "", "cancelled", ""
	case errors.Is(err, errStopping):
		return "", "error", msgRestart
	case errors.Is(err, errLimitHit):
		return "", "limit_reached", msgLimit
	case errors.Is(err, context.DeadlineExceeded):
		return "Time limit reached", "", ""
	case errors.As(err, &ce):
		slog.Error("claude call failed", "run", runID, "status", ce.status, "type", ce.typ)
		return "Claude couldn't be reached.", "error", "Claude couldn't be reached. Run it again."
	}
	slog.Error("ai run: browser", "run", runID, "err", err)
	r.mu.Lock()
	if r.chrome != nil && r.chrome.broken() != nil {
		r.chrome = nil
	}
	r.mu.Unlock()
	return "The browser stopped responding.", "error", "The test browser stopped responding. Run it again."
}

// call checks the run may go on, sends p, and adds the response's cost to the run and the workspace.
func (r *Runner) call(ctx, stop context.Context, j *job, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	db := context.WithoutCancel(ctx)
	if stop.Err() != nil {
		return nil, errStopping
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cancelled, err := r.model.cancelRequested(db, j.runID)
	if err != nil {
		return nil, err
	}
	if cancelled {
		return nil, errCancelled
	}
	limit, err := limitUSD(db, r.cfg)
	if err != nil {
		return nil, err
	}
	n, err := spent(db, r.model.db, j.wsID)
	if err != nil {
		return nil, err
	}
	// ponytail: soft limit, runs racing on one workspace may overshoot it by a step each.
	if n >= int64(limit*1e6) {
		return nil, errLimitHit
	}
	m, err := r.o.Send(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := r.model.addUsage(db, j, usageOf(m)); err != nil {
		return nil, err
	}
	return m, nil
}

func (r *Runner) browser(ctx context.Context) (*cdp, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chrome != nil && r.chrome.broken() == nil {
		return r.chrome, nil
	}
	c, err := dialChrome(ctx, r.o.ChromeURL)
	if err != nil {
		return nil, err
	}
	r.chrome = c
	return c, nil
}

// runCase drives one case until Claude calls finish ("passed" or "failed"), or returns "error" with a summary for
// a case that couldn't finish. err ends the whole run.
func (r *Runner) runCase(ctx, stop context.Context, j *job, x Result) (status, summary string, repro []string, err error) {
	db := context.WithoutCancel(ctx)
	c, err := r.browser(ctx)
	if err != nil {
		return "", "", nil, err
	}
	pg, err := c.newPage(ctx, r.o.ProxyURL, j.runID, j.token, func(f []byte) { r.o.Live.publish(j.runID, f) })
	if err != nil {
		return "", "", nil, err
	}
	defer pg.close()
	p := stepParams()
	msgs := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(caseIntro(j, x)))}
	var last *anthropic.BetaToolResultBlockParam
	var elements []string
	logged := 0
	seenBlocked := map[string]bool{}
	for range maxStepsPerCase {
		p.Messages = msgs
		m, err := r.call(ctx, stop, j, p)
		if err != nil {
			return "", "", nil, err
		}
		if m.StopReason == anthropic.BetaStopReasonRefusal {
			return "error", "Claude declined to run this case.", nil, nil
		}
		msgs = append(msgs, m.ToParam())
		var uses []anthropic.BetaToolUseBlock
		for _, b := range m.Content {
			if u, ok := b.AsAny().(anthropic.BetaToolUseBlock); ok {
				uses = append(uses, u)
			}
		}
		if len(uses) == 0 {
			msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("Call exactly one tool.")))
			continue
		}
		var in toolInput
		if err := json.Unmarshal([]byte(uses[0].JSON.Input.Raw()), &in); err != nil {
			in = toolInput{}
		}
		slog.Info("ai step", "run", j.runID, "tool", uses[0].Name, "ref", in.Ref) // never the typed text
		if uses[0].Name == "finish" {
			switch in.Result {
			case "pass":
				return "passed", in.Summary, nil, nil
			case "blocked":
				return "error", in.Summary, nil, nil
			}
			return "failed", in.Summary, in.Reproduction, nil
		}
		note, action, err := r.act(ctx, pg, j, uses[0].Name, in, elements)
		if err != nil {
			return "", "", nil, err
		}
		for _, host := range r.o.Proxy.takeBlocked(j.runID) {
			if seenBlocked[host] || !pg.asked(host) {
				continue
			}
			seenBlocked[host] = true
			blocked := "Blocked: " + host + " isn't a registered test site"
			note = strings.TrimSpace(note + "\n" + blocked + ".")
			if err := r.model.addStep(db, j, r.o.UploadDir, x.ID, logged, blocked, "", nil); err != nil {
				return "", "", nil, err
			}
			logged++
		}
		st, err := pg.state(ctx)
		if err != nil && c.broken() != nil {
			return "", "", nil, err
		}
		elements = st.Elements
		shot, err := pg.screenshot(ctx)
		if err != nil {
			return "", "", nil, err
		}
		if err := r.model.addStep(db, j, r.o.UploadDir, x.ID, logged, action, st.URL, shot); err != nil {
			return "", "", nil, err
		}
		logged++
		tr := &anthropic.BetaToolResultBlockParam{
			ToolUseID: uses[0].ID,
			Content: []anthropic.BetaToolResultBlockParamContentUnion{
				{OfText: &anthropic.BetaTextBlockParam{Text: stateText(note, st)}},
				{OfImage: &anthropic.BetaImageBlockParam{Source: anthropic.BetaImageBlockParamSourceUnion{
					OfBase64: &anthropic.BetaBase64ImageSourceParam{Data: base64.StdEncoding.EncodeToString(shot), MediaType: "image/jpeg"}}}},
			},
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(), // the history so far is read at cache price next step
		}
		if last != nil {
			last.CacheControl = anthropic.BetaCacheControlEphemeralParam{}
		}
		last = tr
		blocks := []anthropic.BetaContentBlockParamUnion{{OfToolResult: tr}}
		for _, u := range uses[1:] {
			blocks = append(blocks, anthropic.NewBetaToolResultBlock(u.ID, "Ignored: call one tool per turn.", true))
		}
		msgs = append(msgs, anthropic.NewBetaUserMessage(blocks...))
	}
	return "error", "Step limit reached", nil, nil
}

// act does one browser action and returns what happened for Claude (note: a refused or failed action) and the
// step log's description of it, which never includes typed text.
func (r *Runner) act(ctx context.Context, pg *page, j *job, tool string, in toolInput, elements []string) (note, action string, err error) {
	el := describe(in.Ref, elements)
	switch tool {
	case "navigate":
		action = "navigate " + in.URL
		u, perr := url.Parse(in.URL)
		hosts := make([]string, len(j.sites))
		for i, s := range j.sites {
			hosts[i] = s.Host
		}
		if perr != nil || (u.Scheme != "https" && u.Scheme != "http") || !allowedHost(u.Hostname(), hosts) {
			return blockedMsg + ".", action, nil
		}
		err = pg.navigate(ctx, u.String())
	case "click":
		action = "click " + el
		err = pg.click(ctx, in.Ref)
	case "type":
		action = "type into " + el
		err = pg.typeText(ctx, in.Ref, in.Text)
	case "select":
		action = fmt.Sprintf("select %q in %s", in.Value, el)
		err = pg.selectOption(ctx, in.Ref, in.Value)
	case "press":
		action = "press " + in.Key
		err = pg.press(ctx, in.Key)
	case "wait":
		s := min(max(in.Seconds, 1), 5)
		action = fmt.Sprintf("wait %ds", s)
		sleep(ctx, time.Duration(s)*time.Second)
	case "fill_password":
		action = fmt.Sprintf("fill the password of %q into %s", in.Account, el)
		var pw string
		if pw, err = r.accountPassword(ctx, j, in.Account); err == nil {
			err = pg.typeText(ctx, in.Ref, pw)
		}
	default:
		return "Unknown tool.", tool, nil
	}
	var pe pageError
	if errors.As(err, &pe) {
		return string(pe), action, nil
	}
	return "", action, err
}

// accountPassword opens the password of the run's workspace's account with the label.
func (r *Runner) accountPassword(ctx context.Context, j *job, label string) (string, error) {
	for _, a := range j.accounts {
		if a.label != label {
			continue
		}
		sealed, err := r.model.password(context.WithoutCancel(ctx), j.wsID, a.id)
		if err != nil {
			return "", err
		}
		pw, err := unseal(r.o.Key, j.wsID, a.id, sealed)
		if err != nil {
			slog.Error("test account password", "run", j.runID, "account", a.id, "err", err)
			return "", pageError("That account's password can't be read. Ask an admin to save it again.")
		}
		return pw, nil
	}
	return "", pageError("No test account with that label.")
}

// describe is the element's line from the page state, or just its ref.
func describe(ref int, elements []string) string {
	prefix := fmt.Sprintf("[%d] ", ref)
	for _, e := range elements {
		if strings.HasPrefix(e, prefix) {
			return e
		}
	}
	return strings.TrimSpace(prefix)
}

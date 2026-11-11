package varmq

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hub protocol 1 limits (spec/hub.md).
const (
	hubProtocol     = 1
	hubMaxLine      = 64 << 10 // bytes per stream line
	hubMaxReply     = 16 << 20 // bytes per reply body
	hubMaxInFlight  = 8        // requests run at once
	hubQueued       = 64       // frames waiting for a runner; the reader blocks past it
	hubMaxIDLen     = 128      // InstanceID bytes
	hubMaxFrameID   = 64       // request id bytes
	hubDefaultBeat  = 5 * time.Second
	hubMinBeat      = time.Second
	hubMaxBeat      = 60 * time.Second
	hubFirstRetry   = time.Second
	hubMaxRetry     = 30 * time.Second
	hubByeTimeout   = 2 * time.Second
	hubErrBodyLimit = 4 << 10
)

// HubOption configures [ConnectHub].
type HubOption func(*hubConfig)

type hubConfig struct {
	readOnly   bool
	noPayloads bool
	insecure   bool
	client     *http.Client
	heartbeat  time.Duration
	onErr      func(error)

	// test hooks: protocol floors are too slow for unit tests
	minBeat    time.Duration
	firstRetry time.Duration
	maxRetry   time.Duration
}

// WithHubReadOnly refuses every request but GET with 403: the hub can read
// the instance, not act on it.
func WithHubReadOnly() HubOption { return func(c *hubConfig) { c.readOnly = true } }

// WithHubNoPayloads answers the job routes (GET /queues/{id}/jobs and
// /queues/{id}/jobs/{jobId}) with payload null and no result.
func WithHubNoPayloads() HubOption { return func(c *hubConfig) { c.noPayloads = true } }

// WithHubClient sets the HTTP client, e.g. for custom TLS or a proxy. Its
// Timeout is ignored: the stream is long-lived.
func WithHubClient(c *http.Client) HubOption { return func(cfg *hubConfig) { cfg.client = c } }

// WithHubHeartbeat sets the heartbeat used when the hub's hello gives none
// (default 5 s, clamped to 1–60 s).
func WithHubHeartbeat(d time.Duration) HubOption { return func(c *hubConfig) { c.heartbeat = d } }

// WithHubInsecure allows plain http:// to a host that is not loopback. The
// token then travels in clear text.
func WithHubInsecure() HubOption { return func(c *hubConfig) { c.insecure = true } }

// WithHubErrHandler receives connection errors: failed dials, hub statuses
// such as 401, dropped streams. Calls are serialized. nil ignores them.
func WithHubErrHandler(f func(error)) HubOption { return func(c *hubConfig) { c.onErr = f } }

// hubStatusError is an unexpected hub reply status.
type hubStatusError struct {
	op   string
	code int
}

func (e *hubStatusError) Error() string {
	return fmt.Sprintf("varmq: hub %s: %d %s", e.op, e.code, http.StatusText(e.code))
}

var (
	errHubStale    = errors.New("varmq: hub stream: no line for 3 heartbeats")
	errHubNoStream = errors.New("varmq: hub beat: no open stream")
	errHubClosed   = errors.New("varmq: hub stream: closed by hub")
)

// ConnectHub connects this process to a hub (vartop) at hubURL and serves
// the REST API of [Handler] through it: the hub reads and acts on the
// instance with no inbound port. It dials out, keeps one stream open,
// redials with backoff when it drops, and returns ctx.Err() once ctx ends,
// after telling the hub (best effort). It returns at once on a bad config:
// hubURL not http(s), empty token, [InstanceID] empty or over 128 bytes,
// or plain http:// to a host that is not loopback without [WithHubInsecure].
//
// The token is sent as a bearer header and grants what Handler grants:
// see [WithHubReadOnly] and [WithHubNoPayloads]. Set InstanceID first when
// several processes share a host; the hub refuses a duplicate.
//
//	go varmq.ConnectHub(ctx, "https://vartop.internal", token)
//
// Nothing here runs on the job path. Protocol: spec/hub.md.
func ConnectHub(ctx context.Context, hubURL, token string, opts ...HubOption) error {
	h, err := newHub(hubURL, token, newHubConfig(opts))
	if err != nil {
		return err
	}

	h.run(ctx)
	if h.cfg.client == nil { // own transport; a caller's stays theirs
		h.client.CloseIdleConnections()
	}
	return ctx.Err()
}

type hub struct {
	cfg     hubConfig
	client  *http.Client
	handler http.Handler
	token   string
	id      string
	base    *url.URL
	stream  string
	beat    string
	bye     string
	errMu   sync.Mutex
}

// newHubConfig applies opts over the protocol defaults.
func newHubConfig(opts []HubOption) hubConfig {
	cfg := hubConfig{heartbeat: hubDefaultBeat, minBeat: hubMinBeat, firstRetry: hubFirstRetry, maxRetry: hubMaxRetry}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.heartbeat <= 0 {
		cfg.heartbeat = hubDefaultBeat
	}
	return cfg
}

func newHub(hubURL, token string, cfg hubConfig) (*hub, error) {
	u, err := url.Parse(hubURL)
	if err != nil {
		return nil, fmt.Errorf("varmq: hub url: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("varmq: hub url %q: want http(s)://host[/path]", hubURL)
	}
	if u.Scheme == "http" && !cfg.insecure && !loopback(u.Hostname()) {
		return nil, fmt.Errorf("varmq: hub url %q: plain http to a non-loopback host sends the token in clear text (WithHubInsecure allows it)", hubURL)
	}
	if token == "" || strings.ContainsFunc(token, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil, errors.New("varmq: hub token empty or not a header value")
	}
	id := InstanceID
	if id == "" || len(id) > hubMaxIDLen {
		return nil, fmt.Errorf("varmq: InstanceID must be 1 to %d bytes", hubMaxIDLen)
	}

	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	q := func(v url.Values) string { return "?" + v.Encode() }
	h := &hub{
		cfg:     cfg,
		handler: Handler(""),
		token:   token,
		id:      id,
		base:    u,
		stream:  u.JoinPath("hub", "stream").String() + q(url.Values{"instance": {id}, "protocol": {strconv.Itoa(hubProtocol)}}),
		beat:    u.JoinPath("hub", "beat").String() + q(url.Values{"instance": {id}}),
		bye:     u.JoinPath("hub", "beat").String() + q(url.Values{"instance": {id}, "bye": {"1"}}),
	}

	if cfg.client != nil {
		c := *cfg.client
		c.Timeout = 0 // the stream lives for hours; contexts bound every call
		h.client = &c
	} else {
		t, ok := http.DefaultTransport.(*http.Transport)
		if ok {
			t = t.Clone()
		} else { // wrapped (e.g. tracing): a fresh one, not a panic
			t = &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
		}
		t.MaxIdleConnsPerHost = hubMaxInFlight + 2 // replies and beats reuse connections
		h.client = &http.Client{
			Transport: t,
			// the token must not follow a redirect (maybe to plain http)
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}

	return h, nil
}

// loopback reports host is localhost or a loopback IP.
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *hub) report(err error) {
	if h.cfg.onErr == nil || err == nil {
		return
	}
	h.errMu.Lock()
	defer h.errMu.Unlock()
	h.cfg.onErr(err)
}

// run dials until ctx ends, then says bye if a stream was ever open.
func (h *hub) run(ctx context.Context) {
	var (
		ceiling time.Duration // current backoff ceiling, 0: none yet
		ever    bool
	)
	for {
		hello, err := h.session(ctx)
		ever = ever || hello
		if ctx.Err() != nil {
			break
		}
		h.report(err)

		var se *hubStatusError
		if hello {
			ceiling = 0
		}
		ceiling = h.nextBackoff(ceiling, errors.As(err, &se) && (se.code == http.StatusUnauthorized || se.code == http.StatusBadRequest))
		select {
		case <-ctx.Done():
		case <-time.After(ceiling/2 + rand.N(ceiling/2+1)):
		}
		if ctx.Err() != nil {
			break
		}
	}

	if ever {
		bctx, cancel := context.WithTimeout(context.Background(), hubByeTimeout)
		defer cancel()
		h.post(bctx, h.bye, nil, nil)
	}
}

// nextBackoff is the backoff ceiling after one at d: the first retry, then
// doubling to the cap; capped jumps to the cap (401, 400: a rotated token
// heals at that pace). The sleep is random in [d/2, d].
func (h *hub) nextBackoff(d time.Duration, capped bool) time.Duration {
	switch {
	case capped:
		return h.cfg.maxRetry
	case d == 0:
		return h.cfg.firstRetry
	default:
		return min(2*d, h.cfg.maxRetry)
	}
}

// clampBeat is the heartbeat for a hello's heartbeat_ms: the configured
// default when absent, else clamped to [minBeat, 60 s].
func (h *hub) clampBeat(ms int64) time.Duration {
	d := h.cfg.heartbeat
	if ms > 0 {
		d = time.Duration(min(ms, int64(hubMaxBeat/time.Millisecond))) * time.Millisecond
	}
	return min(max(d, h.cfg.minBeat), hubMaxBeat)
}

// hubFrame is one stream line. Unknown fields are ignored.
type hubFrame struct {
	Type        string `json:"type"`
	Protocol    int    `json:"protocol"`
	HeartbeatMS int64  `json:"heartbeat_ms"`
	ID          string `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
}

// session runs one stream until it drops or ctx ends. hello reports the
// hub's hello arrived. Every goroutine it starts has ended on return.
func (h *hub) session(ctx context.Context) (hello bool, err error) {
	sctx, cancel := context.WithCancelCause(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancel(nil)
		wg.Wait()
	}()

	beat := h.clampBeat(0)
	// the watchdog runs from the dial: a hub that never answers is dead too
	stale := time.AfterFunc(3*beat, func() { cancel(errHubStale) })
	defer stale.Stop()

	req, err := http.NewRequestWithContext(sctx, http.MethodGet, h.stream, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Accept", "application/x-ndjson")
	resp, err := h.client.Do(req)
	if err != nil {
		return false, causeOr(sctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, hubErrBodyLimit))
		return false, &hubStatusError{op: "stream", code: resp.StatusCode}
	}
	stale.Reset(3 * beat)

	// ponytail: the reader blocks once hubQueued frames wait; a hub that
	// floods slow requests past 3 heartbeats then gets a redial.
	frames := make(chan hubFrame, hubQueued)
	defer close(frames) // before wg.Wait (defers run last-first)
	for range hubMaxInFlight {
		wg.Go(func() {
			for f := range frames {
				if sctx.Err() == nil { // dead stream: the hub fails what waits
					h.serve(sctx, f)
				}
			}
		})
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4<<10), hubMaxLine+1)
	for sc.Scan() {
		stale.Reset(3 * beat)
		var f hubFrame
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			h.report(fmt.Errorf("varmq: hub frame: %w", err))
			continue
		}
		switch f.Type {
		case "hello":
			if hello {
				continue
			}
			if f.Protocol != hubProtocol {
				return hello, fmt.Errorf("varmq: hub speaks protocol %d, want %d", f.Protocol, hubProtocol)
			}
			hello, beat = true, h.clampBeat(f.HeartbeatMS)
			stale.Reset(3 * beat)
			every := beat
			wg.Go(func() { h.beatLoop(sctx, cancel, every) })
		case "request":
			if !validFrameID(f.ID) {
				h.report(fmt.Errorf("varmq: hub frame: bad request id %q", f.ID))
				continue
			}
			select {
			case frames <- f:
			case <-sctx.Done():
				return hello, context.Cause(sctx)
			}
		}
	}
	if c := context.Cause(sctx); c != nil {
		return hello, c
	}
	if err := sc.Err(); err != nil {
		return hello, fmt.Errorf("varmq: hub stream: %w", err)
	}
	return hello, errHubClosed
}

// causeOr is ctx's cause once it ended (the watchdog's, say), else err.
func causeOr(ctx context.Context, err error) error {
	if c := context.Cause(ctx); c != nil {
		return c
	}
	return err
}

// beatLoop posts a beat every interval until ctx ends. 404 (the hub lost
// the stream) and 401 end the session.
func (h *hub) beatLoop(ctx context.Context, cancel context.CancelCauseFunc, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		bctx, stop := context.WithTimeout(ctx, every)
		code, err := h.post(bctx, h.beat, nil, nil)
		stop()
		switch {
		case err != nil:
			if ctx.Err() == nil {
				h.report(err)
			}
		case code == http.StatusNoContent:
		case code == http.StatusNotFound:
			cancel(errHubNoStream)
			return
		case code == http.StatusUnauthorized:
			cancel(&hubStatusError{op: "beat", code: code})
			return
		default:
			h.report(&hubStatusError{op: "beat", code: code})
		}
	}
}

// post sends body to target with the token and hdr, and returns the status.
func (h *hub) post(ctx context.Context, target string, hdr http.Header, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, hubErrBodyLimit))
	resp.Body.Close()
	return resp.StatusCode, nil
}

// validFrameID reports id is 1 to 64 bytes of [A-Za-z0-9_-].
func validFrameID(id string) bool {
	if id == "" || len(id) > hubMaxFrameID {
		return false
	}
	for _, c := range []byte(id) {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// serve runs one request frame on the handler and posts the reply.
func (h *hub) serve(ctx context.Context, f hubFrame) {
	rec := h.run1(ctx, f)

	hdr := http.Header{}
	hdr.Set("X-Hub-Status", strconv.Itoa(rec.code))
	if ct := rec.h.Get("Content-Type"); ct != "" {
		hdr.Set("Content-Type", ct)
	}
	hdr.Set(InstanceHeader, rec.h.Get(InstanceHeader))

	target := h.base.JoinPath("hub", "reply", f.ID).String() + "?" + url.Values{"instance": {h.id}}.Encode()
	code, err := h.post(ctx, target, hdr, rec.body.Bytes())
	switch {
	case err != nil:
		if ctx.Err() == nil {
			h.report(fmt.Errorf("varmq: hub reply: %w", err))
		}
	case code == http.StatusNoContent, code == http.StatusNotFound: // 404: the hub gave up on it
	default:
		h.report(&hubStatusError{op: "reply", code: code})
	}
}

// run1 answers one frame: 400 for a bad frame, 403 for a write when read
// only, else the handler's reply (502 when over 16 MiB or not strippable).
func (h *hub) run1(ctx context.Context, f hubFrame) (rec *hubRecorder) {
	rec = &hubRecorder{h: http.Header{}, code: http.StatusOK}
	fail := func(code int, msg string) *hubRecorder {
		rec = &hubRecorder{h: http.Header{}}
		writeError(rec, code, msg)
		return rec
	}

	u, err := url.ParseRequestURI(f.Path)
	switch {
	case f.Method != http.MethodGet && f.Method != http.MethodPatch:
		return fail(http.StatusBadRequest, "hub frame: method must be GET or PATCH")
	case !strings.HasPrefix(f.Path, "/") || err != nil:
		return fail(http.StatusBadRequest, "hub frame: path must start with /")
	case h.cfg.readOnly && f.Method != http.MethodGet:
		return fail(http.StatusForbidden, "read-only hub connection")
	}

	req := (&http.Request{
		Method:     f.Method,
		URL:        u,
		RequestURI: f.Path,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       http.NoBody,
		Host:       "hub",
		RemoteAddr: "hub",
	}).WithContext(ctx)

	panicked := func() (p bool) {
		defer func() {
			if r := recover(); r != nil {
				p = true
			}
		}()
		h.handler.ServeHTTP(rec, req)
		return false
	}()
	switch {
	case panicked:
		return fail(http.StatusInternalServerError, "handler panicked")
	case rec.over:
		return fail(http.StatusBadGateway, "reply over 16 MiB")
	}
	if rec.h.Get("Content-Type") == "" && rec.body.Len() > 0 {
		rec.h.Set("Content-Type", http.DetectContentType(rec.body.Bytes()))
	}

	if h.cfg.noPayloads && f.Method == http.MethodGet && rec.code == http.StatusOK && jobRoute(u) {
		body, err := stripPayloads(rec.body.Bytes())
		if err != nil { // fail closed: never forward what can't be stripped
			return fail(http.StatusBadGateway, "reply not strippable")
		}
		rec.body.Reset()
		rec.body.Write(body)
	}
	return rec
}

// jobRoute reports u is /queues/{id}/jobs or /queues/{id}/jobs/{jobId}.
func jobRoute(u *url.URL) bool {
	s := strings.Split(u.EscapedPath(), "/")
	return (len(s) == 4 || len(s) == 5) && s[0] == "" && s[1] == "queues" && s[2] != "" && s[3] == "jobs" &&
		(len(s) == 4 || s[4] != "")
}

// stripPayloads sets payload null and removes result in a job record or a
// page of them.
func stripPayloads(body []byte) ([]byte, error) {
	strip := func(m map[string]json.RawMessage) {
		if _, ok := m["payload"]; ok {
			m["payload"] = json.RawMessage("null")
		}
		delete(m, "result")
	}
	if t := bytes.TrimSpace(body); len(t) > 0 && t[0] == '[' {
		var page []map[string]json.RawMessage
		if err := json.Unmarshal(t, &page); err != nil {
			return nil, err
		}
		for _, m := range page {
			strip(m)
		}
		return json.Marshal(page)
	}
	var one map[string]json.RawMessage
	if err := json.Unmarshal(body, &one); err != nil {
		return nil, err
	}
	strip(one)
	return json.Marshal(one)
}

// hubRecorder is a minimal http.ResponseWriter that keeps up to 16 MiB.
type hubRecorder struct {
	h     http.Header
	code  int
	wrote bool
	over  bool
	body  bytes.Buffer
}

func (r *hubRecorder) Header() http.Header { return r.h }

func (r *hubRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.code, r.wrote = code, true
	}
}

var errHubReplyTooLarge = errors.New("varmq: hub reply over 16 MiB")

func (r *hubRecorder) Write(p []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	if r.over || r.body.Len()+len(p) > hubMaxReply {
		r.over = true
		r.body.Reset()
		return 0, errHubReplyTooLarge
	}
	return r.body.Write(p)
}

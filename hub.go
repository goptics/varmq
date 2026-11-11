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
	"sync/atomic"
	"time"
)

// Hub protocol 1 limits (spec/hub.md).
const (
	hubProtocol     = 1
	hubMaxLine      = 64 << 10 // bytes per stream line
	hubMaxReply     = 16 << 20 // bytes per reply body
	hubMaxInFlight  = 8        // requests run at once, across redials
	hubQueued       = 64       // frames waiting for a runner; the reader blocks past it
	hubMaxBadFrames = 16       // bad frames that end a stream
	hubMaxIDLen     = 128      // InstanceID bytes
	hubMaxFrameID   = 64       // request id bytes
	hubDefaultBeat  = 5 * time.Second
	hubMinBeat      = time.Second
	hubMaxBeat      = 60 * time.Second
	hubFirstRetry   = time.Second
	hubMaxRetry     = 30 * time.Second
	hubByeTimeout   = 2 * time.Second
	hubReplyTimeout = 30 * time.Second
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
	minBeat      time.Duration
	firstRetry   time.Duration
	maxRetry     time.Duration
	replyTimeout time.Duration
	runners      *sync.WaitGroup // tracks the runners, which may outlive ConnectHub
}

// WithHubReadOnly refuses every request but GET with 403: the hub can read
// the instance, not act on it.
func WithHubReadOnly() HubOption { return func(c *hubConfig) { c.readOnly = true } }

// WithHubNoPayloads answers the job routes (GET /queues/{id}/jobs and
// /queues/{id}/jobs/{jobId}) with payload null and no result; result_size
// stays. Job error texts (errors[].error) and workflow task reasons are
// still sent: they can hold application data.
func WithHubNoPayloads() HubOption { return func(c *hubConfig) { c.noPayloads = true } }

// WithHubClient sets the HTTP client, e.g. for custom TLS or a proxy.
// ConnectHub uses a copy: its Timeout is ignored (the stream is long-lived)
// and redirects are never followed (the token stays with hubURL). Before
// each redial ConnectHub closes the idle connections of its transport, so
// a dead HTTP/2 connection is not reused.
func WithHubClient(c *http.Client) HubOption { return func(cfg *hubConfig) { cfg.client = c } }

// WithHubHeartbeat sets the heartbeat used when the hub's hello gives none
// (default 5 s, clamped to 1–60 s).
func WithHubHeartbeat(d time.Duration) HubOption { return func(c *hubConfig) { c.heartbeat = d } }

// WithHubInsecure allows plain http:// to a host that is not loopback. The
// token then travels in clear text, and anyone on the path can act as the
// hub: read the instance and run every action the connection allows.
func WithHubInsecure() HubOption { return func(c *hubConfig) { c.insecure = true } }

// WithHubErrHandler receives connection errors: failed dials, hub statuses
// such as 401, dropped streams, bad frames, handler panics. Text from the
// hub is truncated. Calls are serialized and stop when ConnectHub
// returns. nil ignores them.
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
	errHubStale     = errors.New("varmq: hub stream: no line for 3 heartbeats")
	errHubBusy      = errors.New("varmq: hub stream: request queue full for 3 heartbeats (8 running, 64 waiting)")
	errHubBadFrames = errors.New("varmq: hub stream: 16 bad frames")
	errHubBadLine   = errors.New("varmq: hub frame: not a JSON object with a string type")
	errHubNoStream  = errors.New("varmq: hub beat: no open stream")
	errHubClosed    = errors.New("varmq: hub stream: closed by hub")
)

// ConnectHub connects this process to a hub (vartop) at hubURL and serves
// the REST API of [Handler] through it: the hub reads and acts on the
// instance with no inbound port. It dials out, keeps one stream open,
// redials with backoff when it drops, and returns ctx.Err() once ctx ends,
// after telling the hub (best effort). It returns at once on a bad config:
// hubURL not http(s), or with userinfo, a query or a fragment; empty
// token; [InstanceID] empty or over 128 bytes; or plain http:// to a host
// that is not loopback without [WithHubInsecure].
//
// The token proves this instance to the hub; only TLS and hubURL prove the
// hub. Whoever answers at hubURL gets everything Handler allows: it reads
// job payloads and results, stops and resizes workers, pauses queues and
// cancels jobs and workflows. On shared storage a queue pause or a job
// cancel reaches every consumer process. See [WithHubReadOnly] and
// [WithHubNoPayloads]. Set InstanceID first when several processes share
// a host; the hub refuses a duplicate.
//
//	err := varmq.ConnectHub(ctx, "https://vartop.internal", token,
//		varmq.WithHubErrHandler(func(err error) { log.Print(err) }))
//
// The hub connection holds no job-path lock. It runs the same handlers as
// a mounted Handler, at most 8 at once, which turns on the same job
// tracking as [NewServerMux]. A handler that ignores its context (a
// Restart waiting for a long job, say) does not delay a redial or the
// return: its goroutine may outlive the call, and its reply is dropped.
// Protocol: spec/hub.md.
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
	frames  chan hubJob // shared by every stream: the runners outlive them

	errMu  sync.Mutex
	closed bool // ConnectHub returned: no more callbacks
}

// hubJob is a request frame and the context of the stream it came on.
type hubJob struct {
	ctx context.Context
	f   hubFrame
}

// newHubConfig applies opts over the protocol defaults.
func newHubConfig(opts []HubOption) hubConfig {
	cfg := hubConfig{
		heartbeat:    hubDefaultBeat,
		minBeat:      hubMinBeat,
		firstRetry:   hubFirstRetry,
		maxRetry:     hubMaxRetry,
		replyTimeout: hubReplyTimeout,
		runners:      new(sync.WaitGroup),
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.heartbeat <= 0 {
		cfg.heartbeat = hubDefaultBeat
	}
	return cfg
}

// newHub checks the config. Its errors never repeat hubURL: it may hold
// credentials.
func newHub(hubURL, token string, cfg hubConfig) (*hub, error) {
	u, err := url.Parse(hubURL)
	switch {
	case err != nil:
		return nil, errors.New("varmq: hub url: not a URL")
	case u.User != nil:
		return nil, errors.New("varmq: hub url: userinfo not allowed; the token authenticates")
	case (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return nil, errors.New("varmq: hub url: want http(s)://host[/path], no query or fragment")
	case u.Scheme == "http" && !cfg.insecure && !loopback(u.Hostname()):
		return nil, fmt.Errorf("varmq: hub url: plain http to %s, not loopback, sends the token in clear text (WithHubInsecure allows it)", u.Host)
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
		frames:  make(chan hubJob, hubQueued),
	}

	if cfg.client != nil {
		c := *cfg.client
		h.client = &c
	} else {
		h.client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: time.Second,
			MaxIdleConnsPerHost:   hubMaxInFlight + 2, // replies and beats reuse connections
		}}
	}
	h.client.Timeout = 0 // the stream lives for hours; contexts bound every call
	// the token must not follow a redirect (maybe to plain http)
	h.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

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
	if !h.closed {
		h.cfg.onErr(err)
	}
}

// run dials until ctx ends, then says bye if a stream was ever open.
func (h *hub) run(ctx context.Context) {
	defer func() {
		h.errMu.Lock()
		h.closed = true
		h.errMu.Unlock()
	}()
	for range hubMaxInFlight {
		h.cfg.runners.Go(func() { h.runner(ctx) })
	}

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
		// a dead HTTP/2 connection (no FIN) would carry the redial: dial fresh
		h.client.CloseIdleConnections()
	}

	if ever {
		bctx, cancel := context.WithTimeout(context.Background(), hubByeTimeout)
		defer cancel()
		h.post(bctx, h.bye, nil, nil)
	}
}

// runner serves queued frames until ctx ends. The runners live as long as
// ConnectHub, not one stream: a handler stuck past its stream holds its
// runner, so at most 8 requests run at once across redials.
func (h *hub) runner(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-h.frames:
			if j.ctx.Err() == nil { // dead stream: the hub fails what waits
				h.serve(j.ctx, j.f)
			}
		}
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
// default when absent or 0, else clamped to [minBeat, 60 s].
func (h *hub) clampBeat(ms int64) time.Duration {
	d := h.cfg.heartbeat
	if ms != 0 {
		d = time.Duration(min(max(ms, 0), int64(hubMaxBeat/time.Millisecond))) * time.Millisecond
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

// parseFrame decodes one stream line. Only hello and request are decoded
// in full: any other type is ignored whole, whatever its fields hold.
func parseFrame(line []byte) (hubFrame, error) {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &head) != nil {
		return hubFrame{}, errHubBadLine
	}
	if head.Type != "hello" && head.Type != "request" {
		return hubFrame{Type: head.Type}, nil
	}
	var f hubFrame
	if json.Unmarshal(line, &f) != nil {
		return hubFrame{}, fmt.Errorf("varmq: hub frame: bad %s fields", head.Type)
	}
	if f.Type == "request" && !validFrameID(f.ID) {
		return hubFrame{}, fmt.Errorf("varmq: hub frame: bad request id %.80q", f.ID)
	}
	return f, nil
}

// session runs one stream until it drops or ctx ends. hello reports the
// hub's hello arrived. It returns without waiting for the runners: a
// request of this stream still running ends with its context.
func (h *hub) session(ctx context.Context) (hello bool, err error) {
	sctx, cancel := context.WithCancelCause(ctx)
	var beats sync.WaitGroup
	defer func() {
		cancel(nil)
		beats.Wait()
	}()

	var full atomic.Bool // the reader waits for room in the queue
	beat := h.clampBeat(0)
	// the watchdog runs from the dial: a hub that never answers is dead too
	stale := time.AfterFunc(3*beat, func() {
		if full.Load() {
			cancel(errHubBusy)
		} else {
			cancel(errHubStale)
		}
	})
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

	bad := 0
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4<<10), hubMaxLine+1)
	for sc.Scan() {
		stale.Reset(3 * beat)
		f, err := parseFrame(sc.Bytes())
		if err != nil {
			h.report(err)
			if bad++; bad == hubMaxBadFrames { // the redial backoff bounds the rate
				return hello, errHubBadFrames
			}
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
			beats.Go(func() { h.beatLoop(sctx, cancel, every) })
		case "request":
			// ponytail: the reader blocks once hubQueued frames wait; a
			// hub that floods slow requests past 3 heartbeats gets a redial.
			j := hubJob{sctx, f}
			select {
			case h.frames <- j:
				continue
			default:
			}
			full.Store(true)
			select {
			case h.frames <- j:
				full.Store(false)
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

// serve runs one request frame on the handler and posts the reply, once,
// within the reply timeout. A reply for a stream that ended is dropped.
func (h *hub) serve(ctx context.Context, f hubFrame) {
	rec := h.run1(ctx, f)
	if ctx.Err() != nil { // the hub failed the request with its stream
		return
	}

	hdr := http.Header{}
	hdr.Set("X-Hub-Status", strconv.Itoa(rec.code))
	if ct := rec.h.Get("Content-Type"); ct != "" {
		hdr.Set("Content-Type", ct)
	}
	hdr.Set(InstanceHeader, rec.h.Get(InstanceHeader))

	target := h.base.JoinPath("hub", "reply", f.ID).String() + "?" + url.Values{"instance": {h.id}}.Encode()
	pctx, cancel := context.WithTimeout(ctx, h.cfg.replyTimeout)
	defer cancel()
	code, err := h.post(pctx, target, hdr, rec.body.Bytes())
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
// only, 500 on a handler panic, 502 when the reply is over 16 MiB, else
// the handler's reply.
func (h *hub) run1(ctx context.Context, f hubFrame) *hubRecorder {
	u, err := url.ParseRequestURI(f.Path)
	switch {
	case f.Method != http.MethodGet && f.Method != http.MethodPatch:
		return hubFail(http.StatusBadRequest, "hub frame: method must be GET or PATCH")
	case !strings.HasPrefix(f.Path, "/") || err != nil:
		return hubFail(http.StatusBadRequest, "hub frame: path must start with /")
	case h.cfg.readOnly && f.Method != http.MethodGet:
		return hubFail(http.StatusForbidden, "read-only hub connection")
	}

	if h.cfg.noPayloads { // the job handlers read it after routing
		ctx = context.WithValue(ctx, noPayloadsKey{}, true)
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

	rec := &hubRecorder{h: http.Header{}, code: http.StatusOK}
	if p := h.call(rec, req); p != nil {
		h.report(fmt.Errorf("varmq: hub: handler panicked on %s %.80q: %.80v", f.Method, f.Path, p))
		return hubFail(http.StatusInternalServerError, "handler panicked")
	}
	if rec.over {
		return hubFail(http.StatusBadGateway, "reply over 16 MiB")
	}
	return rec
}

// call runs the handler and returns what it panicked with, nil if nothing.
func (h *hub) call(w http.ResponseWriter, r *http.Request) (p any) {
	defer func() { p = recover() }()
	h.handler.ServeHTTP(w, r)
	return nil
}

// hubFail is a JSON error reply.
func hubFail(code int, msg string) *hubRecorder {
	rec := &hubRecorder{h: http.Header{}}
	writeError(rec, code, msg)
	return rec
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

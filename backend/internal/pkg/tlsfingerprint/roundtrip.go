package tlsfingerprint

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DialTLSFunc establishes a TLS connection to addr ("host:port") and returns
// the ready-to-use connection. It matches the signature of the Dialer /
// HTTPProxyDialer / SOCKS5ProxyDialer DialTLSContext methods, so the caller
// can plug in whichever proxy-aware dialer applies.
type DialTLSFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// RoundTripperConfig configures an order-preserving HTTP/1.1 RoundTripper.
type RoundTripperConfig struct {
	// DialTLS creates the underlying (already TLS-handshaked) connection.
	DialTLS DialTLSFunc
	// HeaderOrder lists request header keys in the exact order a genuine
	// Claude CLI sends them. Headers present on the request but absent from
	// this list are emitted afterwards in stable alphabetical order. Empty
	// disables ordering (headers are written alphabetically, like net/http).
	HeaderOrder []string
	// MaxIdleConnsPerHost caps idle keep-alive connections retained per host.
	MaxIdleConnsPerHost int
	// IdleConnTimeout closes idle connections after this duration. 0 disables.
	IdleConnTimeout time.Duration
	// ResponseHeaderTimeout bounds the wait for response headers. 0 disables.
	ResponseHeaderTimeout time.Duration
}

// RoundTripper is an HTTP/1.1 http.RoundTripper that writes request headers in
// a caller-specified order (and original casing) instead of net/http's
// alphabetical order. It exists so the TLS-fingerprint forwarding path matches
// the real Claude CLI on the wire at the HTTP layer too, not just the TLS
// ClientHello. Responses are parsed with the standard library, so chunked /
// content-length framing and streaming (SSE) bodies behave exactly as with
// http.Transport. Connections are pooled and reused (keep-alive) so no
// "Connection: close" tell is emitted.
type RoundTripper struct {
	cfg  RoundTripperConfig
	pool *connPool
}

// NewRoundTripper builds a RoundTripper from cfg.
func NewRoundTripper(cfg RoundTripperConfig) *RoundTripper {
	return &RoundTripper{
		cfg: cfg,
		pool: &connPool{
			idle:           make(map[string][]*pooledConn),
			maxIdlePerHost: cfg.MaxIdleConnsPerHost,
			idleTimeout:    cfg.IdleConnTimeout,
		},
	}
}

// excludedRequestHeaders are managed by the RoundTripper itself (framing /
// connection lifecycle) and must never be copied verbatim from the request.
var excludedRequestHeaders = map[string]struct{}{
	"host":              {},
	"content-length":    {},
	"connection":        {},
	"proxy-connection":  {},
	"keep-alive":        {},
	"transfer-encoding": {},
	"upgrade":           {},
}

// RoundTrip implements http.RoundTripper.
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.cfg.DialTLS == nil {
		return nil, fmt.Errorf("tlsfingerprint: RoundTripper has no DialTLS")
	}
	if req.URL == nil {
		return nil, fmt.Errorf("tlsfingerprint: nil request URL")
	}

	// Guarantee a known Content-Length (the gateway always sends fixed-length
	// bodies, but buffer defensively so we never need chunked request framing
	// and so a request can be replayed on a stale pooled connection).
	if err := ensureKnownLength(req); err != nil {
		return nil, err
	}

	ctx := req.Context()
	addr := canonicalAddr(req.URL)

	// Up to two attempts: a pooled connection may have been closed by the peer
	// since it went idle. Retry once on a fresh connection when the request is
	// replayable and nothing was read yet.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		pc, reused, err := rt.getConn(ctx, addr)
		if err != nil {
			// RoundTripper contract: always close the request body, even on error.
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, err
		}

		var stopOnce sync.Once
		stop := make(chan struct{})
		stopWatch := func() { stopOnce.Do(func() { close(stop) }) }
		go func() {
			select {
			case <-ctx.Done():
				_ = pc.conn.Close() // unblock any in-flight read/write
			case <-stop:
			}
		}()

		writeErr := rt.writeRequest(pc, req)
		// The request body is fully consumed by writeRequest; close it now.
		if req.Body != nil {
			_ = req.Body.Close()
		}
		if writeErr != nil {
			stopWatch()
			_ = pc.conn.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = writeErr
			if reused && attempt == 0 && rewindBody(req) == nil {
				continue
			}
			return nil, writeErr
		}

		if rt.cfg.ResponseHeaderTimeout > 0 {
			_ = pc.conn.SetReadDeadline(time.Now().Add(rt.cfg.ResponseHeaderTimeout))
		}
		resp, err := http.ReadResponse(pc.br, req)
		if err != nil {
			stopWatch()
			_ = pc.conn.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			if reused && attempt == 0 && rewindBody(req) == nil {
				continue
			}
			return nil, err
		}
		// Clear the header deadline; streaming bodies (SSE) may idle between events.
		_ = pc.conn.SetReadDeadline(time.Time{})

		reusable := !resp.Close && !req.Close
		resp.Body = &pooledBody{
			rt:        rt,
			pc:        pc,
			inner:     resp.Body,
			ctx:       ctx,
			reusable:  reusable,
			stopWatch: stopWatch,
		}
		return resp, nil
	}
	return nil, lastErr
}

// writeRequest serializes the request in origin form with Host first, the
// configured header order next, and Content-Length at its ordered position.
func (rt *RoundTripper) writeRequest(pc *pooledConn, req *http.Request) error {
	bw := bufio.NewWriter(pc.conn)

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	reqURI := req.URL.RequestURI()
	if _, err := fmt.Fprintf(bw, "%s %s HTTP/1.1\r\n", method, reqURI); err != nil {
		return err
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if _, err := fmt.Fprintf(bw, "Host: %s\r\n", host); err != nil {
		return err
	}

	// Assemble the headers we will actually emit (excluding self-managed ones),
	// preserving the exact casing already set on the request.
	emit := make(http.Header, len(req.Header)+2)
	hasAcceptEncoding := false
	for k, vv := range req.Header {
		if _, bad := excludedRequestHeaders[strings.ToLower(k)]; bad {
			continue
		}
		if strings.EqualFold(k, "accept-encoding") {
			hasAcceptEncoding = true
		}
		emit[k] = vv
	}
	// Mimic the real client advertising compression when the caller didn't set
	// it. Limited to codecs the response path can actually decode (gzip/deflate/
	// br) — never zstd — so responses are always decompressable.
	if !hasAcceptEncoding {
		emit["accept-encoding"] = []string{"gzip, deflate, br"}
	}
	// Content-Length is tracked on req.ContentLength, not in the header map.
	if req.ContentLength > 0 || (req.Body != nil && req.ContentLength == 0) {
		emit["content-length"] = []string{strconv.FormatInt(req.ContentLength, 10)}
	}

	for _, key := range orderedHeaderKeys(emit, rt.cfg.HeaderOrder) {
		for _, v := range emit[key] {
			if _, err := bw.WriteString(key); err != nil {
				return err
			}
			if _, err := bw.WriteString(": "); err != nil {
				return err
			}
			if _, err := bw.WriteString(v); err != nil {
				return err
			}
			if _, err := bw.WriteString("\r\n"); err != nil {
				return err
			}
		}
	}
	if _, err := bw.WriteString("\r\n"); err != nil {
		return err
	}

	if req.Body != nil {
		if _, err := io.Copy(bw, req.Body); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// orderedHeaderKeys returns the keys of h ordered by wireOrder first (matched
// case-insensitively, original casing preserved), then any remaining keys in
// stable alphabetical order.
func orderedHeaderKeys(h http.Header, wireOrder []string) []string {
	actual := make(map[string]string, len(h))
	for k := range h {
		actual[strings.ToLower(k)] = k
	}

	result := make([]string, 0, len(h))
	seen := make(map[string]struct{}, len(h))
	for _, wk := range wireOrder {
		lk := strings.ToLower(wk)
		if key, ok := actual[lk]; ok {
			if _, dup := seen[lk]; !dup {
				result = append(result, key)
				seen[lk] = struct{}{}
			}
		}
	}
	extras := make([]string, 0)
	for k := range h {
		if _, ok := seen[strings.ToLower(k)]; !ok {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	return append(result, extras...)
}

func (rt *RoundTripper) getConn(ctx context.Context, addr string) (*pooledConn, bool, error) {
	if pc := rt.pool.get(addr); pc != nil {
		return pc, true, nil
	}
	conn, err := rt.cfg.DialTLS(ctx, "tcp", addr)
	if err != nil {
		return nil, false, err
	}
	return &pooledConn{conn: conn, br: bufio.NewReader(conn), addr: addr}, false, nil
}

// CloseIdleConnections closes all pooled idle connections. http.Client calls
// this via its own CloseIdleConnections.
func (rt *RoundTripper) CloseIdleConnections() {
	rt.pool.closeAll()
}

// ResponseHeaderTimeout reports the configured response-header timeout
// (0 means no timeout). Exposed for tests and diagnostics.
func (rt *RoundTripper) ResponseHeaderTimeout() time.Duration {
	return rt.cfg.ResponseHeaderTimeout
}

// ensureKnownLength buffers a body of unknown length so Content-Length is
// always known and the request stays replayable. No-op for nil/known bodies.
func ensureKnownLength(req *http.Request) error {
	if req.Body == nil || req.ContentLength >= 0 {
		return nil
	}
	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	req.Body = io.NopCloser(strings.NewReader(string(data)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(data))), nil
	}
	return nil
}

// rewindBody resets req.Body for a retry. Returns an error if the body cannot
// be replayed (no GetBody), signalling the caller not to retry.
func rewindBody(req *http.Request) error {
	if req.Body == nil {
		return nil
	}
	if req.GetBody == nil {
		return fmt.Errorf("tlsfingerprint: request body not replayable")
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	return nil
}

// canonicalAddr returns host:port for a URL, defaulting the port by scheme.
func canonicalAddr(u *url.URL) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if strings.EqualFold(u.Scheme, "http") {
			port = "80"
		} else {
			port = "443"
		}
	}
	return net.JoinHostPort(host, port)
}

// --- connection pool ---

type pooledConn struct {
	conn      net.Conn
	br        *bufio.Reader
	addr      string
	idleSince time.Time
}

type connPool struct {
	mu             sync.Mutex
	idle           map[string][]*pooledConn
	maxIdlePerHost int
	idleTimeout    time.Duration
}

func (p *connPool) get(addr string) *pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	conns := p.idle[addr]
	for len(conns) > 0 {
		pc := conns[len(conns)-1]
		conns = conns[:len(conns)-1]
		if p.idleTimeout > 0 && time.Since(pc.idleSince) > p.idleTimeout {
			_ = pc.conn.Close()
			continue
		}
		p.idle[addr] = conns
		return pc
	}
	delete(p.idle, addr)
	return nil
}

func (p *connPool) put(pc *pooledConn) {
	// Never pool a connection with leftover buffered bytes — it would corrupt
	// the next response.
	if pc.br.Buffered() > 0 {
		_ = pc.conn.Close()
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.maxIdlePerHost > 0 && len(p.idle[pc.addr]) >= p.maxIdlePerHost {
		_ = pc.conn.Close()
		return
	}
	pc.idleSince = time.Now()
	p.idle[pc.addr] = append(p.idle[pc.addr], pc)
}

func (p *connPool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, conns := range p.idle {
		for _, pc := range conns {
			_ = pc.conn.Close()
		}
		delete(p.idle, addr)
	}
}

// --- response body that returns its connection to the pool ---

type pooledBody struct {
	rt        *RoundTripper
	pc        *pooledConn
	inner     io.ReadCloser
	ctx       context.Context
	reusable  bool
	eof       bool
	closed    bool
	stopWatch func()
}

func (b *pooledBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if err == io.EOF {
		b.eof = true
	}
	return n, err
}

func (b *pooledBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	b.stopWatch()
	err := b.inner.Close()
	// Reuse only a fully-drained connection on a non-cancelled request.
	if b.reusable && b.eof && b.ctx.Err() == nil {
		b.rt.pool.put(b.pc)
	} else {
		_ = b.pc.conn.Close()
	}
	return err
}

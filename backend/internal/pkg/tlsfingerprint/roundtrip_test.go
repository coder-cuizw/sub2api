package tlsfingerprint

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureServer is a bare TCP server that records the raw request header block
// of every request it receives (so tests can assert on-wire header order and
// casing) and replies via a per-request handler. It supports keep-alive: it
// keeps reading requests on the same connection.
type captureServer struct {
	ln       net.Listener
	mu       sync.Mutex
	requests []capturedRequest
	// handler writes the response for request n (0-based) on conn.
	handler func(n int, conn net.Conn)
}

type capturedRequest struct {
	requestLine string
	headerKeys  []string // in wire order
	headers     map[string]string
	body        string
	connID      int
}

func newCaptureServer(t *testing.T, handler func(n int, conn net.Conn)) *captureServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &captureServer{ln: ln, handler: handler}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *captureServer) serve() {
	connID := 0
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		id := connID
		connID++
		go s.handleConn(conn, id)
	}
}

func (s *captureServer) handleConn(conn net.Conn, connID int) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	for {
		var lines []string
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			return
		}
		cr := capturedRequest{requestLine: lines[0], headers: map[string]string{}, connID: connID}
		contentLength := 0
		for _, l := range lines[1:] {
			idx := strings.Index(l, ":")
			if idx < 0 {
				continue
			}
			k := strings.TrimSpace(l[:idx])
			v := strings.TrimSpace(l[idx+1:])
			cr.headerKeys = append(cr.headerKeys, k)
			cr.headers[k] = v
			if strings.EqualFold(k, "content-length") {
				contentLength, _ = strconv.Atoi(v)
			}
		}
		if contentLength > 0 {
			buf := make([]byte, contentLength)
			if _, err := io.ReadFull(br, buf); err != nil {
				return
			}
			cr.body = string(buf)
		}
		s.mu.Lock()
		n := len(s.requests)
		s.requests = append(s.requests, cr)
		s.mu.Unlock()

		s.handler(n, conn)
	}
}

func (s *captureServer) get(n int) capturedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[n]
}

func (s *captureServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// dialTo returns a DialTLSFunc that always connects to the capture server
// (the addr argument is ignored — no real TLS is needed to exercise the HTTP
// writer).
func dialTo(s *captureServer) DialTLSFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", s.ln.Addr().String())
	}
}

func okResponse(_ int, conn net.Conn) {
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
}

var testWireOrder = []string{
	"Accept",
	"X-Stainless-Lang",
	"anthropic-version",
	"authorization",
	"User-Agent",
	"content-type",
	"content-length",
	"accept-encoding",
}

func newTestClient(s *captureServer) *http.Client {
	rt := NewRoundTripper(RoundTripperConfig{
		DialTLS:             dialTo(s),
		HeaderOrder:         testWireOrder,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	})
	return &http.Client{Transport: rt}
}

func TestRoundTripperHeaderOrderAndCasing(t *testing.T) {
	s := newCaptureServer(t, okResponse)
	client := newTestClient(s)

	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	// Set headers via direct map assignment to preserve wire casing, in an
	// intentionally scrambled order — the RoundTripper must reorder them.
	set := func(k, v string) { req.Header[k] = []string{v} }
	set("content-type", "application/json")
	set("authorization", "Bearer sk-xxx")
	set("anthropic-version", "2023-06-01")
	set("User-Agent", "claude-cli/2.1.161 (external, cli)")
	set("X-Stainless-Lang", "js")
	set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	cr := s.get(0)
	if cr.requestLine != "POST /v1/messages HTTP/1.1" {
		t.Errorf("request line = %q", cr.requestLine)
	}
	want := []string{
		"Host",
		"Accept",
		"X-Stainless-Lang",
		"anthropic-version",
		"authorization",
		"User-Agent",
		"content-type",
		"content-length",
		"accept-encoding",
	}
	if strings.Join(cr.headerKeys, ",") != strings.Join(want, ",") {
		t.Errorf("header order mismatch:\n got:  %v\n want: %v", cr.headerKeys, want)
	}
	if cr.headers["Host"] != "api.anthropic.com" {
		t.Errorf("Host = %q", cr.headers["Host"])
	}
	if cr.headers["content-length"] != "7" {
		t.Errorf("content-length = %q, want 7", cr.headers["content-length"])
	}
	if cr.headers["accept-encoding"] != "gzip, deflate, br" {
		t.Errorf("accept-encoding = %q", cr.headers["accept-encoding"])
	}
	if cr.body != `{"a":1}` {
		t.Errorf("body = %q", cr.body)
	}
}

func TestRoundTripperGETNoBody(t *testing.T) {
	s := newCaptureServer(t, okResponse)
	client := newTestClient(s)

	req, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	req.Header["Accept"] = []string{"application/json"}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	cr := s.get(0)
	if _, ok := cr.headers["content-length"]; ok {
		t.Errorf("GET with no body must not send Content-Length, got %q", cr.headers["content-length"])
	}
	if cr.requestLine != "GET /v1/models HTTP/1.1" {
		t.Errorf("request line = %q", cr.requestLine)
	}
}

func TestRoundTripperExplicitAcceptEncodingPreserved(t *testing.T) {
	s := newCaptureServer(t, okResponse)
	client := newTestClient(s)

	req, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	req.Header["accept-encoding"] = []string{"identity"}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	cr := s.get(0)
	count := 0
	for _, k := range cr.headerKeys {
		if strings.EqualFold(k, "accept-encoding") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one accept-encoding header, got %d (%v)", count, cr.headerKeys)
	}
	if cr.headers["accept-encoding"] != "identity" {
		t.Errorf("accept-encoding = %q, want identity (caller value preserved)", cr.headers["accept-encoding"])
	}
}

func TestRoundTripperKeepAliveReuse(t *testing.T) {
	s := newCaptureServer(t, okResponse)
	client := newTestClient(s)

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{}`))
		req.Header["content-type"] = []string{"application/json"}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do #%d: %v", i, err)
		}
		// Fully drain + close so the connection can be pooled.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	if s.count() != 2 {
		t.Fatalf("expected 2 requests, got %d", s.count())
	}
	if s.get(0).connID != s.get(1).connID {
		t.Errorf("keep-alive failed: requests landed on different connections (%d, %d)",
			s.get(0).connID, s.get(1).connID)
	}
}

func TestRoundTripperNoReuseOnEarlyClose(t *testing.T) {
	// Response has a body the client will NOT fully read before closing, so the
	// connection must not be reused.
	handler := func(_ int, conn net.Conn) {
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n"))
		_, _ = conn.Write(make([]byte, 100))
	}
	s := newCaptureServer(t, handler)
	client := newTestClient(s)

	req1, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("Do #0: %v", err)
	}
	// Close without reading the body to EOF.
	resp1.Body.Close()

	req2, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("Do #1: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()

	if s.count() != 2 {
		t.Fatalf("expected 2 requests, got %d", s.count())
	}
	if s.get(0).connID == s.get(1).connID {
		t.Errorf("connection was reused despite early body close")
	}
}

func TestRoundTripperStreamingChunked(t *testing.T) {
	// Send a chunked response in two parts with a gap; assert the client
	// receives the full payload in order.
	handler := func(_ int, conn net.Conn) {
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"))
		_, _ = conn.Write([]byte("5\r\nhello\r\n"))
		time.Sleep(30 * time.Millisecond)
		_, _ = conn.Write([]byte("5\r\nworld\r\n"))
		_, _ = conn.Write([]byte("0\r\n\r\n"))
	}
	s := newCaptureServer(t, handler)
	client := newTestClient(s)

	req, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/messages", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != "helloworld" {
		t.Errorf("streamed body = %q, want helloworld", string(data))
	}
}

func TestRoundTripperContextCancel(t *testing.T) {
	// Server reads the request but never responds; the client must unblock when
	// the context is cancelled.
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	handler := func(_ int, conn net.Conn) {
		<-block // hold the connection open without writing a response
	}
	s := newCaptureServer(t, handler)
	client := newTestClient(s)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	start := time.Now()
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("context cancel took too long: %v", elapsed)
	}
}

func TestRoundTripperRetriesOnStalePooledConn(t *testing.T) {
	// After serving the first request, the server closes the connection. The
	// client will have pooled it; the next request must transparently retry on
	// a fresh connection instead of failing.
	handler := func(n int, conn net.Conn) {
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
		if n == 0 {
			_ = conn.Close()
		}
	}
	s := newCaptureServer(t, handler)
	client := newTestClient(s)

	req1, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{}`))
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("Do #0: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	// Let the server-side close propagate so the pooled conn is genuinely dead.
	time.Sleep(50 * time.Millisecond)

	req2, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{}`))
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("Do #1 (should retry on fresh conn): %v", err)
	}
	body, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(body) != "ok" {
		t.Errorf("retry response body = %q, want ok", string(body))
	}
	if s.count() < 2 {
		t.Errorf("server should have seen at least 2 requests, got %d", s.count())
	}
}

func TestRoundTripperResponseHeaderTimeout(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	handler := func(_ int, conn net.Conn) {
		<-block
	}
	s := newCaptureServer(t, handler)
	rt := NewRoundTripper(RoundTripperConfig{
		DialTLS:               dialTo(s),
		HeaderOrder:           testWireOrder,
		MaxIdleConnsPerHost:   8,
		ResponseHeaderTimeout: 100 * time.Millisecond,
	})
	client := &http.Client{Transport: rt}

	req, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	start := time.Now()
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("response header timeout took too long: %v", elapsed)
	}
}

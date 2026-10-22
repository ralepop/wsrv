package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	startTimeout = 5 * time.Second
	ioTimeout    = 3 * time.Second
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wsrv-it-")
	if err != nil {
		os.Exit(1)
	}

	binPath = filepath.Join(dir, "wsrv-it")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// ---------- server ----------

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeAddr: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func launch(t *testing.T, root string) string {
	t.Helper()

	for attempt := 1; attempt <= 5; attempt++ {
		addr := freeAddr(t)
		var stderr bytes.Buffer

		cmd := exec.Command(binPath, "-addr", addr, "-root", root)
		cmd.Stderr = &stderr

		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}

		if err := waitReady(addr); err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			if attempt == 5 {
				t.Fatalf("server nije spreman: %v\n%s", err, stderr.String())
			}
			continue
		}

		t.Cleanup(func() {
			cmd.Process.Kill()
			cmd.Wait()
		})
		return addr
	}
	panic("unreachable")
}

func waitReady(addr string) error {
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timeout posle %s", startTimeout)
}

func startServer(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "index.html"), "<h1>home</h1>")
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "hello")
	mustWrite(t, filepath.Join(root, "big.bin"), strings.Repeat("x", 100000))

	return launch(t, root)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ---------- client ----------

type response struct {
	status  string
	headers map[string]string
	body    string
}

func (r *response) String() string {
	return fmt.Sprintf("status=%q headers=%v body=%q", r.status, r.headers, r.body)
}

type client struct {
	conn net.Conn
	br   *bufio.Reader
}

func dial(t *testing.T, addr string) *client {
	t.Helper()

	c, err := net.DialTimeout("tcp", addr, ioTimeout)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	c.SetDeadline(time.Now().Add(ioTimeout))
	t.Cleanup(func() { c.Close() })

	return &client{conn: c, br: bufio.NewReader(c)}
}

func (c *client) send(t *testing.T, raw string) {
	t.Helper()
	if _, err := io.WriteString(c.conn, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func (c *client) readHead(t *testing.T) *response {
	t.Helper()

	line, err := c.br.ReadString('\n')
	if err != nil {
		t.Fatalf("status line: %v", err)
	}

	r := &response{
		status:  strings.TrimRight(line, "\r\n"),
		headers: make(map[string]string),
	}

	for {
		h, err := c.br.ReadString('\n')
		if err != nil {
			t.Fatalf("header: %v", err)
		}
		if h == "\r\n" || h == "\n" {
			break
		}
		i := strings.Index(h, ":")
		if i < 0 {
			t.Fatalf("malformed header: %q", h)
		}
		r.headers[strings.ToLower(strings.TrimSpace(h[:i]))] = strings.TrimSpace(h[i+1:])
	}

	return r
}

func (c *client) readBody(t *testing.T, r *response) {
	t.Helper()

	cl, ok := r.headers["content-length"]
	if !ok {
		b, err := io.ReadAll(c.br)
		if err != nil {
			t.Fatalf("body (close-delimited): %v", err)
		}
		r.body = string(b)
		return
	}

	var n int
	if _, err := fmt.Sscanf(cl, "%d", &n); err != nil {
		t.Fatalf("Content-Length %q: %v", cl, err)
	}

	b := make([]byte, n)
	if _, err := io.ReadFull(c.br, b); err != nil {
		t.Fatalf("body (%d bajtova): %v", n, err)
	}
	r.body = string(b)
}

func (c *client) do(t *testing.T, raw string) *response {
	t.Helper()
	c.send(t, raw)
	r := c.readHead(t)
	c.readBody(t, r)
	return r
}

func (c *client) expectEOF(t *testing.T) {
	t.Helper()
	if _, err := c.br.ReadByte(); err != io.EOF {
		t.Fatalf("ocekivan EOF, dobijeno: %v", err)
	}
}

// ---------- assertions ----------

func expectStatus(t *testing.T, r *response, want string) {
	t.Helper()
	if r.status != want {
		t.Fatalf("status = %q, hteo %q (%s)", r.status, want, r)
	}
}

func expectHeader(t *testing.T, r *response, name, want string) {
	t.Helper()
	if got := r.headers[strings.ToLower(name)]; got != want {
		t.Fatalf("%s = %q, hteo %q (%s)", name, got, want, r)
	}
}

func expectBody(t *testing.T, r *response, want string) {
	t.Helper()
	if r.body != want {
		t.Fatalf("body = %q, hteo %q (%s)", r.body, want, r)
	}
}

func expectContentLength(t *testing.T, r *response, want int) {
	t.Helper()
	expectHeader(t, r, "Content-Length", fmt.Sprintf("%d", want))
	if len(r.body) != want {
		t.Fatalf("telo ima %d bajtova, Content-Length kaze %d", len(r.body), want)
	}
}

// ---------- tests ----------

func TestGETIndex(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectBody(t, r, "<h1>home</h1>")
	expectContentLength(t, r, len("<h1>home</h1>"))
	if ct := r.headers["content-type"]; !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
	expectHeader(t, r, "Connection", "keep-alive")

	r = c.do(t, "GET /sub/a.txt HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectBody(t, r, "hello")
}

func TestGETBinary(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "GET /big.bin HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectContentLength(t, r, 100000)
	expectHeader(t, r, "Content-Type", "application/octet-stream")
}

func TestHEAD(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	c.send(t, "HEAD /big.bin HTTP/1.1\r\nHost: localhost\r\n\r\n")
	r := c.readHead(t)

	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectHeader(t, r, "Content-Length", "100000")

	r = c.do(t, "GET /sub/a.txt HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectBody(t, r, "hello")
}

func TestNotFoundKeepsConnection(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "GET /missing HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 404 Not Found")
	expectBody(t, r, "Not Found")
	expectHeader(t, r, "Connection", "keep-alive")

	r = c.do(t, "GET /sub/a.txt HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
}

func TestMethodNotAllowed(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 0\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 405 Method Not Allowed")
	expectHeader(t, r, "Allow", "GET, HEAD")
}

func TestConnectionClose(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectHeader(t, r, "Connection", "close")
	c.expectEOF(t)
}

func TestHTTP10Closes(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "GET / HTTP/1.0\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectHeader(t, r, "Connection", "close")
	c.expectEOF(t)
}

func TestHTTP10KeepAlive(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	r := c.do(t, "GET / HTTP/1.0\r\nConnection: keep-alive\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectHeader(t, r, "Connection", "keep-alive")

	r = c.do(t, "GET /sub/a.txt HTTP/1.0\r\nConnection: keep-alive\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
}

func TestBadRequests(t *testing.T) {
	addr := startServer(t)

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"prazan zahtev", "\r\n\r\n", "400"},
		{"request line bez verzije", "GET /\r\nHost: x\r\n\r\n", "400"},
		{"nedostaje Host", "GET / HTTP/1.1\r\n\r\n", "400"},
		{"spejs pre dvotacke", "GET / HTTP/1.1\r\nHost : x\r\n\r\n", "400"},
		{"CL nije broj", "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: abc\r\n\r\n", "400"},
		{"TE i CL", "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\nContent-Length: 3\r\n\r\nabc", "400"},
		{"dupli Host", "GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n", "400"},
		{"chunked nije finalni", "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked, gzip\r\n\r\n", "400"},
		{"nepoznata verzija", "GET / HTTP/2.0\r\nHost: x\r\n\r\n", "505"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dial(t, addr)
			r := c.do(t, tc.raw)

			if !strings.Contains(r.status, tc.want) {
				t.Fatalf("status = %q, hteo %s (%s)", r.status, tc.want, r)
			}
			expectHeader(t, r, "Connection", "close")
		})
	}
}

func TestTraversalRejected(t *testing.T) {
	addr := startServer(t)

	for _, target := range []string{
		"..%2f..%2fetc%2fpasswd",
		"/%2e%2e/%2e%2e/etc/passwd",
		"/%00x",
	} {
		t.Run(target, func(t *testing.T) {
			c := dial(t, addr)
			r := c.do(t, "GET "+target+" HTTP/1.1\r\nHost: localhost\r\n\r\n")

			if strings.Contains(r.status, "200") {
				t.Fatalf("traversal je prosao: %s", r)
			}
			if strings.Contains(r.body, "root:") {
				t.Fatalf("procureo sadrzaj: %q", r.body)
			}
		})
	}
}

func TestSplitHeaderAcrossWrites(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	c.send(t, "GET / HTTP/1.1\r\nHost: local")
	time.Sleep(50 * time.Millisecond)
	c.send(t, "host\r\n\r\n")

	r := c.readHead(t)
	c.readBody(t, r)
	expectStatus(t, r, "HTTP/1.1 200 OK")
	expectBody(t, r, "<h1>home</h1>")
}

func TestIdleConnectionDoesNotBlock(t *testing.T) {
	addr := startServer(t)

	_ = dial(t, addr)

	c := dial(t, addr)
	r := c.do(t, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	expectStatus(t, r, "HTTP/1.1 200 OK")
}

func TestPipelining(t *testing.T) {
	addr := startServer(t)
	c := dial(t, addr)

	c.send(t, "GET /sub/a.txt HTTP/1.1\r\nHost: localhost\r\n\r\n"+
		"GET /index.html HTTP/1.1\r\nHost: localhost\r\n\r\n")

	r1 := c.readHead(t)
	c.readBody(t, r1)
	expectStatus(t, r1, "HTTP/1.1 200 OK")
	expectBody(t, r1, "hello")

	r2 := c.readHead(t)
	c.readBody(t, r2)
	expectStatus(t, r2, "HTTP/1.1 200 OK")
	expectBody(t, r2, "<h1>home</h1>")
}

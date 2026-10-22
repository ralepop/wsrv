package main

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr error
		check   func(t *testing.T, req *Request)
	}{
		// ---------- happy path ----------
		{
			name: "GET sa Host",
			raw:  "GET /index.html HTTP/1.1\r\nHost: example.com\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if req.Method != "GET" || req.Path != "/index.html" || req.Version != "HTTP/1.1" {
					t.Fatalf("neocekivani request line: %q %q %q", req.Method, req.Path, req.Version)
				}
			},
		},
		{
			name: "HTTP/1.0 bez Host",
			raw:  "GET / HTTP/1.0\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if req.Version != "HTTP/1.0" {
					t.Fatalf("verzija = %q", req.Version)
				}
			},
		},
		{
			name: "HTTP/1.9 se vraca na 1.1",
			raw:  "GET / HTTP/1.9\r\nHost: x\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if req.Version != "HTTP/1.1" {
					t.Fatalf("verzija = %q, ocekivano HTTP/1.1", req.Version)
				}
			},
		},
		{
			name: "imena zaglavlja na lower",
			raw:  "GET / HTTP/1.1\r\nHost: x\r\nX-CUSTOM: v\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if _, ok := req.Headers["x-custom"]; !ok {
					t.Fatalf("zaglavlje nije na lower: %v", req.Headers)
				}
			},
		},
		{
			name: "duplikati se spajaju sa zarezom",
			raw:  "GET / HTTP/1.1\r\nHost: x\r\nX-Dup: a\r\nX-Dup: b\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if got := req.Headers["x-dup"]; got != "a, b" {
					t.Fatalf("x-dup = %q, ocekivano %q", got, "a, b")
				}
			},
		},
		{
			name: "Connection duplikati se spajaju",
			raw:  "GET / HTTP/1.1\r\nHost: x\r\nConnection: keep-alive\r\nConnection: close\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if got := req.Headers["connection"]; got != "keep-alive, close" {
					t.Fatalf("connection = %q", got)
				}
			},
		},
		{
			name: "LF-only line endings su dozvoljeni",
			raw:  "GET / HTTP/1.1\nHost: x\n\n",
		},
		{
			name: "telo se cita tacno Content-Length bajtova",
			raw:  "POST /x HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello",
			check: func(t *testing.T, req *Request) {
				if string(req.Body) != "hello" {
					t.Fatalf("telo = %q", req.Body)
				}
			},
		},
		{
			name: "Content-Length: 0 daje prazno telo",
			raw:  "POST /x HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if len(req.Body) != 0 {
					t.Fatalf("telo = %q, ocekivano prazno", req.Body)
				}
			},
		},
		{
			name: "metod sme da sadrzi tchar (-)",
			raw:  "M-SEARCH / HTTP/1.1\r\nHost: x\r\n\r\n",
			check: func(t *testing.T, req *Request) {
				if req.Method != "M-SEARCH" {
					t.Fatalf("metod = %q", req.Method)
				}
			},
		},
		{
			name: "lowercase metod je validan token",
			raw:  "get / HTTP/1.1\r\nHost: x\r\n\r\n",
		},

		// ---------- request line ----------
		{name: "prazan zahtev", raw: "", wantErr: ErrMalformedHeader},
		{name: "samo CRLF", raw: "\r\n\r\n", wantErr: ErrInvalidMethod},
		{name: "jedan token", raw: "GARBAGE\r\n\r\n", wantErr: ErrInvalidMethod},
		{name: "cetiri dela", raw: "GET / HTTP/1.1 extra\r\nHost: x\r\n\r\n", wantErr: ErrInvalidMethod},
		{name: "dupli razmak u request line-u", raw: "GET  / HTTP/1.1\r\nHost: x\r\n\r\n", wantErr: ErrInvalidMethod},
		{name: "metod sa nedozvoljenim tchar (@)", raw: "GE@T / HTTP/1.1\r\nHost: x\r\n\r\n", wantErr: ErrBadRequest},

		// ---------- version ----------
		{name: "major 2.0 -> 505", raw: "GET / HTTP/2.0\r\nHost: x\r\n\r\n", wantErr: ErrHTTPVersionNotSupported},
		{name: "major 0.9 -> 505", raw: "GET / HTTP/0.9\r\nHost: x\r\n\r\n", wantErr: ErrHTTPVersionNotSupported},
		{name: "verzija bez minor", raw: "GET / HTTP/1\r\nHost: x\r\n\r\n", wantErr: ErrBadRequest},
		{name: "verzija nije broj", raw: "GET / HTTP/abc\r\nHost: x\r\n\r\n", wantErr: ErrBadRequest},
		{name: "tri broja u verziji", raw: "GET / HTTP/1.1.1\r\nHost: x\r\n\r\n", wantErr: ErrBadRequest},

		// ---------- Host ----------
		{name: "1.1 bez Host", raw: "GET / HTTP/1.1\r\n\r\n", wantErr: ErrBadRequest},
		{name: "prazan Host", raw: "GET / HTTP/1.1\r\nHost: \r\n\r\n", wantErr: ErrBadRequest},
		{name: "Host samo sapce", raw: "GET / HTTP/1.1\r\nHost:    \r\n\r\n", wantErr: ErrBadRequest},
		{name: "dupli Host", raw: "GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n", wantErr: ErrBadRequest},
		{name: "Host sa space u vrednosti", raw: "GET / HTTP/1.1\r\nHost: foo bar\r\n\r\n", wantErr: ErrBadRequest},
		{name: "Host sa userinfo", raw: "GET / HTTP/1.1\r\nHost: user@evil\r\n\r\n", wantErr: ErrBadRequest},
		{
			name: "Host sa portom je validan",
			raw:  "GET / HTTP/1.1\r\nHost: example.com:8080\r\n\r\n",
		},

		// ---------- field lines ----------
		{name: "space pre dvotacke", raw: "GET / HTTP/1.1\r\nHost : x\r\n\r\n", wantErr: ErrBadRequest},
		{name: "zaglavlje bez dvotacke", raw: "GET / HTTP/1.1\r\nHost: x\r\nX-Broken\r\n\r\n", wantErr: ErrBadRequest},
		{name: "obs-fold", raw: "GET / HTTP/1.1\r\nHost: x\r\n X: y\r\n\r\n", wantErr: ErrBadRequest},
		{name: "prazno ime zaglavlja", raw: "GET / HTTP/1.1\r\nHost: x\r\n: v\r\n\r\n", wantErr: ErrBadRequest},

		// ---------- framing ----------
		{name: "TE chunked -> 501", raw: "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n", wantErr: ErrNotImplemented},
		{name: "TE chunked nije finalni -> 400", raw: "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked, gzip\r\n\r\n0\r\n\r\n", wantErr: ErrBadRequest},
		{name: "TE gzip -> 400", raw: "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: gzip\r\n\r\n0\r\n\r\n", wantErr: ErrBadRequest},
		{name: "TE i CL zajedno", raw: "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\nTransfer-Encoding: chunked\r\n\r\nhello", wantErr: ErrBadRequest},
		{name: "dupli Content-Length", raw: "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\nContent-Length: 5\r\n\r\nhello", wantErr: ErrBadRequest},
		{name: "CL nije broj", raw: "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 5a\r\n\r\nhello", wantErr: ErrBadRequest},
		{name: "CL negativan", raw: "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: -1\r\n\r\n", wantErr: ErrBadRequest},
		{name: "CL preko maxBody", raw: "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 2000000\r\n\r\n", wantErr: ErrPayloadTooLarge},
		{name: "POST bez CL i TE", raw: "POST / HTTP/1.1\r\nHost: x\r\n\r\n", wantErr: ErrLengthRequired},
		{name: "PUT bez CL i TE", raw: "PUT / HTTP/1.1\r\nHost: x\r\n\r\n", wantErr: ErrLengthRequired},
		{name: "telo krace od CL", raw: "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhel", wantErr: ErrIncompleteBody},
		{name: "zahtev bez prazne linije", raw: "GET / HTTP/1.1\r\nHost: x\r\n", wantErr: ErrMalformedHeader},
		{
			name:    "header preko limita",
			raw:     "GET / HTTP/1.1\r\nHost: x\r\nX-Pad: " + strings.Repeat("A", headerLimit) + "\r\n\r\n",
			wantErr: ErrRequestTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := parseRequest(bufio.NewReader(strings.NewReader(tt.raw)))

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("ocekivana greska %v, dobijen zahtev %+v", tt.wantErr, req)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("greska = %v, ocekivano %v", err, tt.wantErr)
				}
				if req != nil {
					t.Fatalf("uz gresku se vraca i zahtev: %+v", req)
				}
				return
			}

			if err != nil {
				t.Fatalf("neocekivana greska: %v", err)
			}
			if tt.check != nil {
				tt.check(t, req)
			}
		})
	}
}

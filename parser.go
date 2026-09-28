package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const maxHeaderBytes = 64 * 1024 // 64 KB

var (
	ErrRequestTooLarge = errors.New("request header exceeds limit")
	ErrMalformedHeader = errors.New("malformed HTTP request header")
	ErrInvalidMethod   = errors.New("invalid HTTP request line")
	ErrIncompleteBody  = errors.New("body size does not match Content-Length")
)

type Request struct {
	Method 		string
	Path 		string
	Version 	string
	Headers 	map[string]string
	Body		[]byte
}

func ParseRequest(raw []byte) (*Request, error) {
	if len(raw) > maxHeaderBytes {
		return nil, ErrRequestTooLarge
	}

	// razdvajamo zaglavlja od tela - "\r\n\r\n"
	headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	var headerBytes []byte
	var bodyBytes []byte

	if headerEnd != -1 {
		headerBytes = raw[:headerEnd]
		bodyBytes = raw[headerEnd+4:] // preskacemo "\r\n\r\n"
	} else {
		// ukoliko nema body
		headerBytes = raw
	}

	// delimo header
	lines := bytes.Split(headerBytes, []byte("\r\n"))
	if len(lines) == 0 || len(lines[0]) == 0 {
		return nil, ErrInvalidMethod
	}

	// parsiramo prvu liniju (npr. "GET /index.html HTTP/1.1")
	requestLine := string(lines[0])
	parts := strings.Split(requestLine, " ")
	if len(parts) != 3 {
		return nil, ErrInvalidMethod
	}

	req := &Request{
		Method: 	parts[0],
		Path: 		parts[1],
		Version: 	parts[2],
		Headers: 	make(map[string]string),
	}

	// validacija protokola
	if !strings.HasPrefix(req.Version, "HTTP/") {
		return nil, ErrInvalidMethod
	}

	// parsiramo ostala zaglavlja
	for i := 1; i < len(lines); i++ {
		line := string(lines[i])
		if line == "" {
			continue
		}

		c := strings.Index(line, ":")
		if c == -1 {
			return nil, ErrMalformedHeader
		}

		key := strings.ToLower(strings.TrimSpace(line[:c]))
		val := strings.TrimSpace(line[c+1:])
		req.Headers[key] = val
	}

	if clStr, exists := req.Headers["content-length"]; exists {
		expectedLen, err := strconv.Atoi(clStr)
		if err != nil || expectedLen < 0 {
			return nil, fmt.Errorf("invalid Content-Length value: %w", ErrMalformedHeader)
		}

		// provera da li smo dobili dovoljno bajtova tela
		if len(bodyBytes) < expectedLen {
			return nil, ErrIncompleteBody
		}

		req.Body = bodyBytes[:expectedLen]
	} else {
		req.Body = bodyBytes
	}

	return req, nil
}

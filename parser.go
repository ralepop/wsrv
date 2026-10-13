package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// HTTP 413: Payload Too Large, odnosi se na body
// HTTP 431: Request Header Fields Too Large, odnosi se na header

const readBufSize = 4 * 1024 // koliko bufio.Reader vuce iz soketa u jednom koraku, 4KB
const headerLimit = 8 * 1024 // koliko bajtova headera prihvatamo pre 431
const maxBody = 1024 * 1024  // gornja granica tela pre 413

var (
	ErrBadRequest      = errors.New("400 Bad Request")
	ErrNotImplemented  = errors.New("501 Not Implemented")
	ErrPayloadTooLarge = errors.New("413 Payload Too Large")
	ErrLengthRequired  = errors.New("411 Length Required")
	ErrTimeout         = errors.New("408 Request Timeout")

	ErrRequestTooLarge = errors.New("request header exceeds limit")
	ErrMalformedHeader = errors.New("malformed HTTP request header")
	ErrInvalidMethod   = errors.New("invalid HTTP request line")
	ErrIncompleteBody  = errors.New("body size does not match Content-Length")
)

type Request struct {
	Method  string
	Path    string
	Version string
	Headers map[string]string
	Body    []byte
}

func readLine(r *bufio.Reader) ([]byte, error) {

	// ReadBytes('\n') vraca kopiju []byte, alokacija po liniji
	lineBytes, err := r.ReadBytes('\n')

	if err != nil && err != io.EOF {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, ErrTimeout
		}
	}

	return lineBytes, err
}

func parseRequest(r *bufio.Reader) (*Request, error) {

	// HEADER LOOP

	total := 0
	var lines []string

	for {
		line, err := readLine(r)

		if err != nil {
			if err == io.EOF {
				return nil, ErrMalformedHeader
			}
			return nil, err
		}

		total += len(line)

		if total > headerLimit {
			return nil, ErrRequestTooLarge
		}

		// skidamo \r i \n sa kraja
		line = bytes.TrimRight(line, "\r\n")

		// ako je linija prazna
		if len(line) == 0 {
			break
		}

		lines = append(lines, string(line))
	}

	if len(lines) == 0 || len(lines[0]) == 0 {
		return nil, ErrInvalidMethod
	}

	// parsiramo prvu liniju (npr. "GET /index.html HTTP/1.1")
	requestLine := lines[0]
	parts := strings.Split(requestLine, " ")
	if len(parts) != 3 {
		return nil, ErrInvalidMethod
	}

	req := &Request{
		Method:  parts[0],
		Path:    parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}

	// validacija protokola
	if !strings.HasPrefix(req.Version, "HTTP/") {
		return nil, ErrInvalidMethod
	}

	// parsiramo ostala zaglavlja
	for i := 1; i < len(lines); i++ {
		line := lines[i]
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

	teStr, teExists := req.Headers["transfer-encoding"]
	clStr, clExists := req.Headers["content-length"]

	if teExists {
		// ako ima i TE i CL -> 400 (anti-smuggling)
		if clExists {
			return nil, fmt.Errorf("both TE and CL present: %w", ErrBadRequest)
		}

		// delimo enkodovanja ako ih ima vise (npr. "gzip, chunked")
		teParts := strings.Split(teStr, ",")
		lastEncoding := strings.TrimSpace(strings.ToLower(teParts[len(teParts)-1]))

		if lastEncoding == "chunked" {
			// ne implementiramo chunked -> 501
			return nil, fmt.Errorf("chunked TE not supported: %w", ErrNotImplemented)
		} else {
			// chunked nije na kraju TE -> 501
			return nil, fmt.Errorf("TE without final chunked: %w", ErrNotImplemented)
		}
	}

	if clExists {

		if len(clStr) == 0 {
			return nil, fmt.Errorf("CL is empty: %w", ErrBadRequest)
		}

		// moze biti samo cifra, bez minusa, slova, razmaka
		for _, ch := range clStr {
			if ch < '0' || ch > '9' {
				return nil, fmt.Errorf("CL is not 1*DIGIT: %w", ErrBadRequest)
			}
		}

		expectedLen, err := strconv.Atoi(clStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CL value: %w", ErrBadRequest)
		}

		// N > maxBody -> 413 (pre alociranja)
		if expectedLen > maxBody {
			return nil, ErrPayloadTooLarge
		}

		// citamo tacno expectedLen bajtova
		req.Body = make([]byte, expectedLen)
		_, err = io.ReadFull(r, req.Body)

		if err != nil {
			switch err {
			case io.EOF, io.ErrUnexpectedEOF:
				return nil, ErrIncompleteBody
			default:
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					return nil, ErrTimeout
				}
				return nil, ErrBadRequest
			}
		}

		return req, nil
	}

	// nema ni TE ni CL
	switch req.Method {
	case "POST", "PUT", "PATCH":
		// ako je POST/PUT/PATCH, treba duzina tela
		return nil, ErrLengthRequired // 411

	default:
		// inace (GET, HEAD, DELETE, ...), duzina tela je 0
		req.Body = nil
		return req, nil
	}
}

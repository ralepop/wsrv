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
	ErrBadRequest              = errors.New("400 Bad Request")
	ErrNotImplemented          = errors.New("501 Not Implemented")
	ErrPayloadTooLarge         = errors.New("413 Payload Too Large")
	ErrLengthRequired          = errors.New("411 Length Required")
	ErrTimeout                 = errors.New("408 Request Timeout")
	ErrHTTPVersionNotSupported = errors.New("505 HTTP Version Not Supported")

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

		if len(lines) > 0 && !validateHeader(line) {
			return nil, ErrBadRequest
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

	if !validMethod(parts[0]) {
		return nil, ErrBadRequest
	}

	version := parts[2]

	status := checkHTTPVersion(version)
	if status == 400 {
		return nil, ErrBadRequest
	}
	if status == 505 {
		return nil, ErrHTTPVersionNotSupported
	}

	if version != "HTTP/1.0" {
		version = "HTTP/1.1"
	}

	req := &Request{
		Method:  parts[0],
		Path:    parts[1],
		Version: version,
		Headers: make(map[string]string),
	}

	numHostHeaders := 0
	numCLHeaders := 0

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

		key := strings.ToLower(line[:c])
		val := strings.TrimSpace(line[c+1:])

		if key == "host" {
			numHostHeaders++
			if numHostHeaders > 1 {
				return nil, ErrBadRequest
			}
		}

		if key == "content-length" {
			numCLHeaders++
			if numCLHeaders > 1 {
				return nil, ErrBadRequest
			}
		}

		if oldValue, exists := req.Headers[key]; exists {
			req.Headers[key] = oldValue + ", " + val
		} else {
			req.Headers[key] = val
		}

	}

	hostHeaderStr, hostHeaderExists := req.Headers["host"]

	// ako Host postoji, mora biti validan
	if hostHeaderExists && !validHostValue(hostHeaderStr) {
		return nil, ErrBadRequest
	}

	// Host je obavezan samo u HTTP/1.1
	if req.Version == "HTTP/1.1" && !hostHeaderExists {
		return nil, ErrBadRequest
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
		}

		return nil, fmt.Errorf("TE without final chunked: %w", ErrBadRequest)
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

func checkHTTPVersion(ver string) int {
	if len(ver) != 8 || ver[:5] != "HTTP/" ||
		ver[5] < '0' || ver[5] > '9' ||
		ver[6] != '.' ||
		ver[7] < '0' || ver[7] > '9' {
		return 400
	}

	// 505 samo za nepodrzan MAJOR
	if ver[5] != '1' {
		return 505
	}

	return 200
}

func validMethod(method string) bool {
	if len(method) == 0 {
		return false
	}

	for i := 0; i < len(method); i++ {
		if !isTokenChar(method[i]) {
			return false
		}
	}

	return true
}

func validateHeader(line []byte) bool {
	// mora postojati dvotacka
	colon := bytes.IndexByte(line, ':')
	if colon <= 0 {
		return false
	}

	// razmak ili tab pre dvotacke nije dozvoljen
	if line[colon-1] == ' ' || line[colon-1] == '\t' {
		return false
	}

	// provera key kod hedera
	key := line[:colon]
	for i := 0; i < len(key); i++ {
		c := line[i]
		if !isTokenChar(c) {
			return false
		}
	}

	// provera vrednosti hedera
	value := line[colon+1:]
	for i := 0; i < len(value); i++ {
		c := value[i]

		// dozvoljeni su tab i obicni tekstualni bajtovi, ali ne ostali ASCII kontrolni
		if c == 0 || (c < 32 && c != '\t') || c == 127 {
			return false
		}
	}

	return true
}

const hostIllegalChars = " /\\@#?"

func validHostValue(v string) bool {
	if v == "" {
		return false
	}

	return !strings.ContainsAny(v, hostIllegalChars)
}

const tcharSet = "!#$%&'*+-.^_`|~"

func isTokenChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		strings.IndexByte(tcharSet, c) >= 0
}

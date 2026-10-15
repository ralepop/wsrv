package main

import (
	"errors"
	"fmt"
	"net"
	"time"
)

const httpDateFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

type ResponseWriter struct {
	conn        net.Conn
	wroteHeader bool
	err         error
	keepAlive   bool
}

func newResponseWriter(conn net.Conn, keepAlive bool) *ResponseWriter {
	return &ResponseWriter{conn: conn, keepAlive: keepAlive}
}

// writeHeader upisuje status liniju i zaglavlja.
// cl < 0 znaci "duzina tela nije poznata": Content-Length se izostavlja.
func (w *ResponseWriter) writeHeader(code int, cl int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	response := fmt.Sprintf("HTTP/1.1 %d %s\r\nDate: %s\r\n",
		code, statusText(code), time.Now().UTC().Format(httpDateFormat))

	if cl >= 0 {
		response += fmt.Sprintf("Content-Length: %d\r\n", cl)
	}

	connection := "close"
	if w.keepAlive {
		connection = "keep-alive"
	}
	response += "Connection: " + connection + "\r\n\r\n"

	if _, err := w.conn.Write([]byte(response)); err != nil {
		w.err = err
	}
}

func (w *ResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.writeHeader(200, len(p))
	}

	if w.err != nil {
		return 0, w.err
	}

	n, err := w.conn.Write(p)
	if err != nil {
		w.err = err
	}

	return n, err
}

func writeError(w *ResponseWriter, err error) {
	code := statusForError(err)
	body := statusText(code)

	w.writeHeader(code, len(body))
	w.Write([]byte(body))
}

// statusForError prevodi gresku iz parsera u code.
// za tekst je zaduzen iskljucivo statusText.
func statusForError(err error) int {
	switch {
	case errors.Is(err, ErrPayloadTooLarge):
		return 413
	case errors.Is(err, ErrNotImplemented):
		return 501
	case errors.Is(err, ErrLengthRequired):
		return 411
	case errors.Is(err, ErrRequestTooLarge):
		return 431
	case errors.Is(err, ErrTimeout):
		return 408
	default:
		return 400
	}
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 408:
		return "Request Timeout"
	case 411:
		return "Length Required"
	case 413:
		return "Payload Too Large"
	case 431:
		return "Request Header Fields Too Large"
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 505:
		return "HTTP Version Not Supported"
	default:
		return "Unknown"
	}
}

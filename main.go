package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"runtime/debug"
	"time"
)

const readTimeout = 5 * time.Second  // cekanje da klijent posalje zahtev
const writeTimeout = 5 * time.Second // cekanje da klijent primi odgovor

func statusForError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrPayloadTooLarge):
		return 413, "Payload Too Large"
	case errors.Is(err, ErrNotImplemented):
		return 501, "Not Implemented"
	case errors.Is(err, ErrLengthRequired):
		return 411, "Length Required"
	case errors.Is(err, ErrRequestTooLarge):
		return 431, "Request Header Fields Too Large"
	case errors.Is(err, ErrTimeout):
		return 408, "Request Timeout"
	default:
		// Za sve ostalo (ErrMalformedHeader, ErrInvalidMethod, ErrBadRequest, ...)
		return 400, "Bad Request"
	}
}

/*
Opsluzuje jednu vec prihvacenu konekciju (main prihvata).
*/
func handleConn(conn net.Conn) {
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic: %v\n%s", r, debug.Stack())
		}
	}()

	conn.SetReadDeadline(time.Now().Add(readTimeout))
	reader := bufio.NewReaderSize(conn, readBufSize)

	// parsiramo primljeni zahtev
	req, err := parseRequest(reader)

	conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	if err != nil {
		log.Printf("Parse error: %v", err)

		code, text := statusForError(err)
		body := text

		response := fmt.Sprintf(
			"HTTP/1.1 %d %s\r\nContent-Length: %d\r\n\r\n%s",
			code, text, len(body), body,
		)

		conn.Write([]byte(response))
		return
	}

	log.Printf("Handled request: %s %s %s", req.Method, req.Path, req.Version)

	// privremeni odgovor
	conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 12\r\n\r\nHello World!"))
}

func main() {

	// definisemo CLI flegove
	addr := flag.String("addr", ":8080", "TCP address to listen on")
	root := flag.String("root", "./static", "Path to static files directory")
	flag.Parse()

	log.Printf("Starting server on %s, serving root %s\n", *addr, *root)

	// otvaramo TCP listener
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}
	defer listener.Close()

	// petlja koja prihvata konekcije
	for {
		conn, err := listener.Accept() // sinhrono
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go handleConn(conn)
	}
}

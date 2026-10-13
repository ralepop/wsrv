package main

import (
	"bufio"
	"flag"
	"log"
	"net"
	"runtime/debug"
	"time"
)

const readTimeout = 5 * time.Second  // cekanje da klijent posalje zahtev
const writeTimeout = 5 * time.Second // cekanje da klijent primi odgovor

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

	w := newResponseWriter(conn)

	conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	if err != nil {
		log.Printf("Parse error: %v", err)
		writeError(w, err)
	} else {
		log.Printf("Handled request: %s %s %s", req.Method, req.Path, req.Version)

		body := "Hello World!"
		w.writeHeader(200, len(body))
		w.Write([]byte(body))
	}

	// klijent je mozda otisao pre nego sto je odgovor upisan
	if w.err != nil {
		log.Printf("Write failed: %v", w.err)
	}
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

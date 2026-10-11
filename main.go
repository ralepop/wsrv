package main

import (
	"flag"
	"log"
	"net"
)

/*
Opsluzuje jednu vec prihvacenu konekciju (main prihvata).
*/
func handleConn(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, bufSize)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Read error: %v", err)
		return
	}

	// parsiramo primljeni zahtev
	req, err := parseRequest(buf[:n])
	if err != nil {
		log.Printf("Parse error: %v", err)
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 11\r\n\r\nBad Request"))
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

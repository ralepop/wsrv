package main

import (
	"flag"
	"log"
	"net"
)

func main() {

	// definisemo CLI flegove
	addr := flag.String("addr", ":8080", "TCP adresa za slusanje")
	root := flag.String("root", "./static", "Putanja do statickih fajlova")
	flag.Parse()

	log.Printf("Server se pokrece na %s, iz %s\n", *addr, *root)

	// otvaramo TCP listener
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Greska pri otvaranju porta: %v", err)
	}
	defer listener.Close()
	
	// petlja koja prihvata konekcije
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("Greska pri prihvatanju konekcije: %v", err)
			continue
		}

		// odgovaramo sa "Hello"
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 12\r\n\r\nHello World!"))
		conn.Close()
	}

}
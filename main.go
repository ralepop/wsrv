package main

import (
	"flag"
	"log"
	"time"
)

func main() {

	// definisemo CLI flegove
	addr := flag.String("addr", ":8080", "TCP address to listen on")
	root := flag.String("root", "./static", "Path to static files directory")
	flag.Parse()

	log.Printf("Starting server on %s, serving root %s\n", *addr, *root)

	server := &Server{
		Addr:         *addr,
		Root:         *root,
		readTimeout:  5 * time.Second,
		writeTimeout: 5 * time.Second,
		idleTimeout:  5 * time.Second,
	}

	if err := server.listenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

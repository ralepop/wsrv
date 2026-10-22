package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {

	// definisemo CLI flegove
	addr := flag.String("addr", ":8080", "TCP address to listen on")
	root := flag.String("root", "./static", "Path to static files directory")
	flag.Parse()

	log.Printf("Starting server on %s, serving root %s\n", *addr, *root)

	// Ctrl-C / SIGTERM otkazuju kontekst -> shutdown krece
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &Server{
		Addr:            *addr,
		Root:            *root,
		readTimeout:     5 * time.Second,
		writeTimeout:    5 * time.Second,
		idleTimeout:     5 * time.Second,
		shutdownTimeout: 3 * time.Second,
		router:          newRouter(*root, []string{"GET", "HEAD"}),
	}

	if err := server.listenAndServe(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	log.Printf("Bye")
}

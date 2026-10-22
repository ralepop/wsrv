package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Addr, Root                                              string
	readTimeout, writeTimeout, idleTimeout, shutdownTimeout time.Duration
	router                                                  *Router
	wg                                                      sync.WaitGroup
}

func (s *Server) listenAndServe(ctx context.Context) error {

	// otvaramo TCP listener
	listener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.Addr, err)
	}
	defer listener.Close()

	// gasenje: zatvaramo listener da bi Accept izasao iz blokade.
	go func() {
		<-ctx.Done()
		log.Printf("Shutting down: no longer accepting connections")
		listener.Close()
	}()

	// petlja koja prihvata konekcije
	for {
		conn, err := listener.Accept() // sinhrono
		if err != nil {
			if ctx.Err() != nil {
				break
			}

			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		s.wg.Add(1)
		go s.handleConn(conn)
	}

	// cekamo da aktivne konekcije zavrse, ali ne duze od shutdownTimeout.
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("Shutdown complete: all connections closed")
	case <-time.After(s.shutdownTimeout):
		log.Printf("Shutdown timeout exceeded, forcing exit")
	}

	return nil
}

/*
Opsluzuje jednu vec prihvacenu konekciju.
*/
func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic: %v\n%s", r, debug.Stack())
		}
	}()

	reader := bufio.NewReaderSize(conn, readBufSize)
	s.serve(conn, reader)
}

func (s *Server) serve(conn net.Conn, reader *bufio.Reader) {
	for {

		conn.SetReadDeadline(time.Now().Add(s.idleTimeout))

		if _, err := reader.Peek(1); err != nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(s.readTimeout))
		req, err := parseRequest(reader)

		if err != nil {
			w := newResponseWriter(conn, false)
			writeError(w, err)
			return
		}

		keepAlive := decide(req)
		w := newResponseWriter(conn, keepAlive)
		conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))

		log.Printf("Handled request: %s %s %s", req.Method, req.Path, req.Version)

		// ruter odlucuje sta je odgovor; greska se pise na jednom mestu
		if err := s.router.serve(req, w); err != nil {
			log.Printf("Handler error: %v", err)

			// ako je telo vec pocelo da se pise, odgovor je nepopravljiv:
			// drugi status bi pokvario tok bajtova na konekciji
			if !w.wroteHeader {
				writeError(w, err)
			}
		}

		// klijent je mozda otisao pre nego sto je odgovor upisan
		if w.err != nil {
			log.Printf("Write failed: %v", w.err)
		}

		// konekcija je gotova ako klijent ne zeli keep-alive ili je upis pukao
		if !keepAlive || w.err != nil {
			return
		}
	}
}

func decide(req *Request) bool {
	connStr, connExists := req.Headers["connection"]

	hasKeepAlive := false
	hasClose := false

	if connExists {
		connectionList := strings.Split(connStr, ",")

		for _, item := range connectionList {
			cleaned := strings.TrimSpace(item)

			if strings.EqualFold(cleaned, "keep-alive") {
				hasKeepAlive = true
			} else if strings.EqualFold(cleaned, "close") {
				hasClose = true
			}
		}
	}

	switch req.Version {
	case "HTTP/1.1":
		// close ima prednost
		return !hasClose
	case "HTTP/1.0":
		// zatvara po defaultu, osim ako klijent izricito trazi keep-alive
		return hasKeepAlive
	default:
		// nepoznata verzija
		return false
	}
}

# wsrv

HTTP/1.1 server napisan od nule u Go-u, na sirovim TCP socketima. Bez `net/http` - parsiranje, framing, keep-alive i serviranje fajlova su ručno napisani.

[Srpski](#srpski) · [English](#english)

---

## Srpski

### O projektu

`wsrv` je HTTP/1.1 server koji ne koristi `net/http`. Jedina osnova je `net` paket: `Listen`, `Accept`, `Read`, `Write`. Cilj je bio da se kroz kod vidi šta protokol stvarno traži - gde se zahtev završava, ko odlučuje o dužini tela, i šta se dešava kada klijent pošalje pola zahteva.

### Pokretanje

```bash
mkdir -p static          # root mora da postoji
go build -o wsrv .
./wsrv -addr :8080 -root ./static
```

### Šta radi

- Zahtev se čita inkrementalno kroz `bufio.Reader` - može da stigne u više TCP segmenata
- Request line i verzija po gramatici: `HTTP/2.0` → 505, `HTTP/abc` → 400, `HTTP/1.9` → tretira se kao 1.1
- `Host` obavezan u 1.1 i validiran; dupli `Host` i dupli `Content-Length` → 400
- Telo po `Content-Length`, tačno N bajtova; `chunked` se prepoznaje i vraća 501
- Keep-alive (1.1 podrazumevano, 1.0 na zahtev) i pipelining
- Deadline za idle/read/write: prekinut zahtev → 408, tiha konekcija se zatvara bez odgovora
- Statički fajlovi: `index.html`, MIME, `HEAD`, 404/403/405 sa `Allow`
- Zaštita od path traversal-a: decode → clean → containment check
- Graceful shutdown, sa ograničenim čekanjem na aktivne konekcije

### Šta namerno ne radi

- `chunked` dekodiranje (vraća 501), TLS, HTTP/2
- Range i conditional requests, kompresija, directory listing
- Nema ograničenja broja istovremenih konekcija

### Struktura

| Fajl | Šta radi |
|---|---|
| `server.go` | accept petlja, goroutine po konekciji, keep-alive, shutdown |
| `parser.go` | bajtovi → `Request` |
| `response.go` | status linija, zaglavlja, telo |
| `router.go` | putanja → fajl, MIME, traversal |
| `main.go` | flagovi i wiring |
| `*_test.go` | 46 parser slučajeva i 13 integracionih testova |

### Testovi

```bash
go test ./...
go test -race ./...
```

### RFC

RFC 9110 (semantika), RFC 9112 (framing), RFC 6585 (431).

---

## English

### About project

`wsrv` is an HTTP/1.1 server that does not use `net/http`. The only foundation is the `net` package: `Listen`, `Accept`, `Read`, `Write`. The point was to make the protocol visible in the code - where a request ends, who decides the body length, and what happens when a client sends half of one.

### Running

```bash
mkdir -p static          # the root must exist
go build -o wsrv .
./wsrv -addr :8080 -root ./static
```

### What it does

- Reads requests incrementally through `bufio.Reader` - a request may arrive across several TCP segments
- Request line and version checked against the grammar: `HTTP/2.0` → 505, `HTTP/abc` → 400, `HTTP/1.9` → treated as 1.1
- `Host` required in 1.1 and validated; duplicate `Host` and duplicate `Content-Length` → 400
- Body read by `Content-Length`, exactly N bytes; `chunked` is detected and answered with 501
- Keep-alive (default in 1.1, opt-in in 1.0) and pipelining
- Idle/read/write deadlines: a half-sent request → 408, a silent connection is closed without a response
- Static files: `index.html`, MIME, `HEAD`, 404/403/405 with `Allow`
- Path traversal guard: decode → clean → containment check
- Graceful shutdown, with a bounded wait for active connections

### What it deliberately does not do

- `chunked` decoding (answers 501), TLS, HTTP/2
- Range and conditional requests, compression, directory listing
- No limit on concurrent connections

### Layout

| File | Purpose |
|---|---|
| `server.go` | accept loop, goroutine per connection, keep-alive, shutdown |
| `parser.go` | bytes → `Request` |
| `response.go` | status line, headers, body |
| `router.go` | path → file, MIME, traversal |
| `main.go` | flags and wiring |
| `*_test.go` | 46 parser cases and 13 integration tests |

### Tests

```bash
go test ./...
go test -race ./...
```

### RFC

RFC 9110 (semantics), RFC 9112 (framing), RFC 6585 (431).

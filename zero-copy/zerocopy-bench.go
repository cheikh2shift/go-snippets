// zerocopy-bench.go
// Benchmark: zero-copy vs wrapped ResponseWriter in Go net/http
// Run: go run zerocopy-bench.go
// Then benchmark with wrk (see bench.sh)

package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const bodySize = 4 << 20 // 4 MB
const listenAddr = ":8080"

// loggingResponseWriter wraps http.ResponseWriter but does NOT forward ReadFrom.
// This is the "bug": it silently disables zero-copy.
type loggingResponseWriter struct {
	http.ResponseWriter
	bytes int64
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// FIX: uncomment to restore zero-copy for the wrapped path.
// func (w *loggingResponseWriter) ReadFrom(src io.Reader) (int64, error) {
// 	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
// 		return rf.ReadFrom(src)
// 	}
// 	return io.Copy(w.ResponseWriter, src)
// }

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lrw := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(lrw, r)
		log.Printf("bytes=%d", lrw.bytes)
	})
}

func main() {
	dir, err := os.MkdirTemp("", "zerocopy")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "body.bin")
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	buf := make([]byte, 64<<10)
	for written := 0; written < bodySize; written += len(buf) {
		if _, err := f.Write(buf); err != nil {
			log.Fatal(err)
		}
	}
	f.Close()

	// /raw: direct io.Copy to the real *http.response (zero-copy path)
	http.HandleFunc("/raw", func(w http.ResponseWriter, r *http.Request) {
		src, _ := os.Open(path)
		defer src.Close()
		io.Copy(w, src)
	})

	// /wrapped: same io.Copy but through the middleware (copy path)
	http.Handle("/wrapped", Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src, _ := os.Open(path)
		defer src.Close()
		io.Copy(w, src)
	})))

	fmt.Printf("serving %d MB body on %s\n", bodySize>>20, listenAddr)
	fmt.Println("bench: wrk -t4 -c200 -d30s http://localhost:8080/raw")
	fmt.Println("bench: wrk -t4 -c200 -d30s http://localhost:8080/wrapped")
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

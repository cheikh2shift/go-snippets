// download fetches the HuggingFaceTB/SmolLM2-135M-Instruct files we need to
// convert the model into llama2.c format. It streams each file in small
// chunks (so RAM stays flat regardless of file size), resumes from a .part
// file on restart via HTTP Range requests, and prints live progress.
//
// Build/run:   go build -o dl.exe ./download && .\dl.exe
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	baseURL  = "https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct/resolve/main/"
	chunkLen = 4 * 1024 * 1024 // 4 MiB read chunks
)

type file struct {
	name string
	url  string
}

func main() {
	files := []file{
		{"config.json", baseURL + "config.json"},
		{"tokenizer_config.json", baseURL + "tokenizer_config.json"},
		{"special_tokens_map.json", baseURL + "special_tokens_map.json"},
		{"tokenizer.json", baseURL + "tokenizer.json"},
		{"model.safetensors", baseURL + "model.safetensors"},
	}
	os.MkdirAll("model", 0755)
	for _, f := range files {
		start := time.Now()
		if err := download(f.name, f.url); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", f.name, err)
			os.Exit(1)
		}
		fmt.Printf("done: %-18s %s\n", f.name, time.Since(start).Round(time.Millisecond))
	}
	fmt.Println("all downloads complete")
}

// download fetches url into name (as name+".part" first, then renames), using
// a Range header to resume any partially-written file.
func download(name, url string) error {
	part := "model/" + name + ".part"

	var start int64
	if fi, err := os.Stat(part); err == nil {
		start = fi.Size()
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (chunked-llama2c-converter)")
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	// Total expected size: Content-Range "bytes start-end/total" when resuming,
	// otherwise Content-Length.
	total := start + resp.ContentLength
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if _, after, ok := strings.Cut(cr, "/"); ok {
			var t int64
			fmt.Sscanf(after, "%d", &t)
			if t > 0 {
				total = t
			}
		}
	}
	if resp.StatusCode == 200 && start > 0 {
		// Server ignored our range request; restart from scratch.
		start = 0
		total = resp.ContentLength
		os.Remove(part)
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return err
		}
	}

	written := start
	buf := make([]byte, chunkLen)
	t0 := time.Now()
	lastPrint := time.Now()
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if time.Since(lastPrint) > time.Second {
			printProgress(name, written, total, t0)
			lastPrint = time.Now()
		}
	}
	printProgress(name, written, total, t0)
	fmt.Println()
	if err := f.Close(); err != nil {
		return err
	}

	if total > 0 && written != total {
		return fmt.Errorf("size mismatch: got %d bytes, expected %d (restart to resume)", written, total)
	}
	return os.Rename(part, "model/"+name)
}

// printProgress renders a single-line progress report with throughput.
func printProgress(name string, written, total int64, t0 time.Time) {
	pct := 0.0
	if total > 0 {
		pct = float64(written) / float64(total) * 100
	}
	rate := float64(written) / time.Since(t0).Seconds() / (1024 * 1024)
	fmt.Printf("\r%s: %6.2f%%  %8.1f / %8.1f MB  %6.1f MB/s  ", name, pct,
		float64(written)/(1024*1024), float64(total)/(1024*1024), rate)
}

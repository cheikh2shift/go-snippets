//go:build goexperiment.simd

// simd_inference.go runs a real llama2.c-format transformer checkpoint
// (e.g. the TinyStories "stories42M" model) using the Go 1.27 experimental
// "simd" package for hardware-accelerated float32 matmuls.
//
// Build/run with:   GOEXPERIMENT=simd go run ./simd_inference.go
//
// A llama2.c tokenizer.bin is required alongside model.bin. The vocab is the
// SentencePiece byte-fallback one: tokens 0-2 are <unk>/<s>/</s>, bytes live
// at ids 3-258, and the rest are BPE merges.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"simd"
)

const maxModelSize = 1024 * 1024 * 1024

// Config mirrors the llama2.c checkpoint header (7 little-endian int32s).
type Config struct {
	Dim       int
	HiddenDim int
	NLayers   int
	NHeads    int
	NKVHeads  int
	VocabSize int
	SeqLen    int
}

func (c Config) headDim() int { return c.Dim / c.NHeads }
func (c Config) kvDim() int   { return c.NKVHeads * (c.Dim / c.NHeads) }

// Model holds all weights in llama2.c "legacy" (v0) order:
//
//	header, tok_embeddings, [attention_norm, wq, wk, wv, wo] * L,
//	[ffn_norm, w1, w2, w3] * L, final norm, freqs_cos, freqs_sin
//
// Wq/Wk/Wv/Wo are row-major (out, in); W1/W3 are (hidden, dim);
// W2 is (dim, hidden); embeddings are tied with the output classifier.
type Model struct {
	Cfg Config

	TokenEmbedding []float32   // [vocabSize][dim]
	AttnNorm       [][]float32 // [layer][dim]
	Wq, Wk, Wv, Wo [][]float32 // [layer][outDim][inDim]
	FfnNorm        [][]float32 // [layer][dim]
	W1, W3         [][]float32 // [layer][hiddenDim][dim]
	W2             [][]float32 // [layer][dim][hiddenDim]
	FinalNorm      []float32   // [dim]

	FreqsCos []float32 // [seqLen][headDim/2]
	FreqsSin []float32 // [seqLen][headDim/2]
}

// Runtime owns per-token scratch buffers and the KV caches.
type Runtime struct {
	m         *Model
	nWorkers int

	x, xb, xb2 []float32   // [dim]
	q          []float32   // [dim]
	k, v       []float32   // [kvDim]
	hb, hb2    []float32   // [hiddenDim]
	att        [][]float32 // [nHeads][seqLen]
	logits     []float32   // [vocabSize]

	keyCache, valCache [][]float32 // [layer][seqLen][kvDim]
}

func LoadModel(path string) (*Model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxModelSize {
		return nil, fmt.Errorf("model size %d exceeds %d byte limit", info.Size(), maxModelSize)
	}

	var hdr [7]int32
	if err := binary.Read(f, binary.LittleEndian, &hdr); err != nil {
		return nil, fmt.Errorf("reading model header: %w", err)
	}
	m := &Model{Cfg: Config{
		Dim:       int(hdr[0]),
		HiddenDim: int(hdr[1]),
		NLayers:   int(hdr[2]),
		NHeads:    int(hdr[3]),
		NKVHeads:  int(hdr[4]),
		VocabSize: int(hdr[5]),
		SeqLen:    int(hdr[6]),
	}}
	cfg := m.Cfg
	if cfg.Dim <= 0 || cfg.HiddenDim <= 0 || cfg.NLayers <= 0 ||
		cfg.NHeads <= 0 || cfg.NKVHeads <= 0 || cfg.VocabSize <= 0 || cfg.SeqLen <= 0 {
		return nil, fmt.Errorf("invalid model config in header: %+v", cfg)
	}
	if cfg.Dim%cfg.NHeads != 0 || cfg.NHeads%cfg.NKVHeads != 0 {
		return nil, fmt.Errorf("invalid head configuration: dim=%d heads=%d kv_heads=%d", cfg.Dim, cfg.NHeads, cfg.NKVHeads)
	}

	readN := func(n int) ([]float32, error) {
		buf := make([]float32, n)
		if err := binary.Read(f, binary.LittleEndian, buf); err != nil {
			return nil, fmt.Errorf("reading %d weights: %w", n, err)
		}
		return buf, nil
	}

	dim, hidden, kvDim, hd := cfg.Dim, cfg.HiddenDim, cfg.kvDim(), cfg.headDim()
	if m.TokenEmbedding, err = readN(cfg.VocabSize * dim); err != nil {
		return nil, err
	}

	allocs := make([][][]float32, 5)
	for i := range allocs {
		allocs[i] = make([][]float32, cfg.NLayers)
	}
	m.AttnNorm, m.Wq, m.Wk, m.Wv, m.Wo = allocs[0], allocs[1], allocs[2], allocs[3], allocs[4]
	m.FfnNorm = make([][]float32, cfg.NLayers)
	m.W1 = make([][]float32, cfg.NLayers)
	m.W2 = make([][]float32, cfg.NLayers)
	m.W3 = make([][]float32, cfg.NLayers)

	for l := 0; l < cfg.NLayers; l++ {
		if m.AttnNorm[l], err = readN(dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.Wq[l], err = readN(dim * dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.Wk[l], err = readN(kvDim * dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.Wv[l], err = readN(kvDim * dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.Wo[l], err = readN(dim * dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.FfnNorm[l], err = readN(dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.W1[l], err = readN(hidden * dim); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.W2[l], err = readN(dim * hidden); err != nil {
			return nil, err
		}
	}
	for l := 0; l < cfg.NLayers; l++ {
		if m.W3[l], err = readN(hidden * dim); err != nil {
			return nil, err
		}
	}
	if m.FinalNorm, err = readN(dim); err != nil {
		return nil, err
	}
	if m.FreqsCos, err = readN(cfg.SeqLen * hd / 2); err != nil {
		return nil, err
	}
	if m.FreqsSin, err = readN(cfg.SeqLen * hd / 2); err != nil {
		return nil, err
	}
	return m, nil
}

// weightBytes returns the total bytes held by all weight tensors.
func (m *Model) weightBytes() int64 {
	var b int64
	b += int64(len(m.TokenEmbedding)) * 4
	b += int64(len(m.FinalNorm)) * 4
	b += int64(len(m.FreqsCos)) * 4
	b += int64(len(m.FreqsSin)) * 4
	for l := 0; l < m.Cfg.NLayers; l++ {
		b += int64(len(m.AttnNorm[l])+len(m.FfnNorm[l])) * 4
		b += int64(len(m.Wq[l])+len(m.Wk[l])+len(m.Wv[l])+len(m.Wo[l])) * 4
		b += int64(len(m.W1[l])+len(m.W2[l])+len(m.W3[l])) * 4
	}
	return b
}

// kvCacheBytes returns the bytes held by both KV caches across all layers.
func (m *Model) kvCacheBytes() int64 {
	kvDim := m.Cfg.kvDim()
	return 2 * int64(m.Cfg.NLayers) * int64(m.Cfg.SeqLen) * int64(kvDim) * 4
}

// scratchBytes returns the bytes held by the per-token scratch buffers.
func (m *Model) scratchBytes() int64 {
	cfg := m.Cfg
	b := 4 * int64(cfg.Dim) * 4                 // x, xb, xb2, q
	b += 2 * int64(cfg.kvDim()) * 4             // k, v
	b += 2 * int64(cfg.HiddenDim) * 4           // hb, hb2
	b += int64(cfg.NHeads) * int64(cfg.SeqLen) * 4 // att
	b += int64(cfg.VocabSize) * 4               // logits
	return b
}

func NewRuntime(m *Model) *Runtime {
	cfg := m.Cfg
	dim, hidden, kvDim := cfg.Dim, cfg.HiddenDim, cfg.kvDim()
	rt := &Runtime{
		m:        m,
		nWorkers: runtime.NumCPU(),
		x:        make([]float32, dim),
		xb:       make([]float32, dim),
		xb2:      make([]float32, dim),
		q:        make([]float32, dim),
		k:        make([]float32, kvDim),
		v:        make([]float32, kvDim),
		hb:       make([]float32, hidden),
		hb2:      make([]float32, hidden),
		att:      make([][]float32, cfg.NHeads),
		logits:   make([]float32, cfg.VocabSize),
	}
	for h := 0; h < cfg.NHeads; h++ {
		rt.att[h] = make([]float32, cfg.SeqLen)
	}
	rt.keyCache = make([][]float32, cfg.NLayers)
	rt.valCache = make([][]float32, cfg.NLayers)
	for l := 0; l < cfg.NLayers; l++ {
		rt.keyCache[l] = make([]float32, cfg.SeqLen*kvDim)
		rt.valCache[l] = make([]float32, cfg.SeqLen*kvDim)
	}
	return rt
}

// Forward runs one token through the transformer and returns the logits
// (next-token distribution over the vocabulary). Position pos is 0-based.
func (rt *Runtime) Forward(token, pos int) []float32 {
	m, cfg := rt.m, rt.m.Cfg
	dim, hidden, kvDim, headDim := cfg.Dim, cfg.HiddenDim, cfg.kvDim(), cfg.headDim()

	emb := m.TokenEmbedding[token*dim : (token+1)*dim]
	copy(rt.x, emb)

	for l := 0; l < cfg.NLayers; l++ {
		rmsNorm(rt.xb, rt.x, m.AttnNorm[l])

		// QKV projections (SIMD matmuls, rows split across workers).
		rt.matVecIntoP(rt.q, m.Wq[l], rt.xb, dim)
		rt.matVecIntoP(rt.k, m.Wk[l], rt.xb, dim)
		rt.matVecIntoP(rt.v, m.Wv[l], rt.xb, dim)

		// Rotary position embeddings.
		rt.applyRope(rt.q, pos, headDim)
		rt.applyRope(rt.k, pos, headDim)

		// Store the keys/values for this position in the KV cache.
		kOff := pos * kvDim
		copy(rt.keyCache[l][kOff:kOff+kvDim], rt.k)
		copy(rt.valCache[l][kOff:kOff+kvDim], rt.v)

		// Multi-head attention over cached positions 0..pos.
		kvMul := cfg.NHeads / cfg.NKVHeads
		rt.attend(rt.xb2, rt.q, rt.keyCache[l], rt.valCache[l], pos,
			cfg.NHeads, kvMul, headDim, kvDim)

		rt.matVecIntoP(rt.xb, m.Wo[l], rt.xb2, dim)
		addInPlace(rt.x, rt.xb)

		// SwiGLU feed-forward network.
		rmsNorm(rt.xb, rt.x, m.FfnNorm[l])
		rt.matVecIntoP(rt.hb, m.W1[l], rt.xb, dim)
		for i, v := range rt.hb {
			rt.hb[i] = swish(v)
		}
		rt.matVecIntoP(rt.hb2, m.W3[l], rt.xb, dim)
		for i := range rt.hb2 {
			rt.hb2[i] *= rt.hb[i]
		}
		rt.matVecIntoP(rt.xb, m.W2[l], rt.hb2, hidden)
		addInPlace(rt.x, rt.xb)
	}

	rmsNorm(rt.x, rt.x, m.FinalNorm)

	// Logits = x · embedding table (tied weights).
	rt.parFor(cfg.VocabSize, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			rt.logits[t] = dotProduct(rt.x, m.TokenEmbedding[t*dim:(t+1)*dim])
		}
	})
	return rt.logits
}

func (rt *Runtime) applyRope(x []float32, pos, headDim int) {
	hd := headDim / 2
	base := pos * hd
	for i := 0; i < len(x); i += headDim {
		for j := 0; j < hd; j++ {
			idx := i + 2*j
			c := rt.m.FreqsCos[base+j]
			s := rt.m.FreqsSin[base+j]
			x1, x2 := x[idx], x[idx+1]
			x[idx] = x1*c - x2*s
			x[idx+1] = x1*s + x2*c
		}
	}
}

// attend computes the attention output for all heads at the current position.
// kvMul = nHeads / nKVHeads groups query heads to shared KV heads (GQA).
// Heads are independent (disjoint q/out/att buffers, read-only KV cache), so
// the head loop is split across workers.
func (rt *Runtime) attend(out, q []float32, keyCache, valCache []float32, pos, nHeads, kvMul, headDim, kvDim int) {
	invSqrt := float32(1.0 / math.Sqrt(float64(headDim)))
	rt.parFor(nHeads, func(lo, hi int) {
		for h := lo; h < hi; h++ {
			att := rt.att[h]
			qh := q[h*headDim : (h+1)*headDim]
			kvh := (h / kvMul) * headDim
			for t := 0; t <= pos; t++ {
				kt := keyCache[t*kvDim+kvh : t*kvDim+kvh+headDim]
				att[t] = dotProduct(qh, kt) * invSqrt
			}
			softmax(att[:pos+1])

			oh := out[h*headDim : (h+1)*headDim]
			for i := range oh {
				oh[i] = 0
			}
			for t := 0; t <= pos; t++ {
				vt := valCache[t*kvDim+kvh : t*kvDim+kvh+headDim]
				a := att[t]
				for i, v := range vt {
					oh[i] += a * v
				}
			}
		}
	})
}

func rmsNorm(out, x, w []float32) {
	var ss float32
	for _, v := range x {
		ss += v * v
	}
	ss = ss/float32(len(x)) + 1e-5
	inv := float32(1.0 / math.Sqrt(float64(ss)))
	for i := range x {
		out[i] = w[i] * (x[i] * inv)
	}
}

func swish(v float32) float32 {
	return v / (1 + float32(math.Exp(float64(-v))))
}

func softmax(v []float32) {
	mx := v[0]
	for _, x := range v[1:] {
		if x > mx {
			mx = x
		}
	}
	var sum float32
	for i, x := range v {
		v[i] = float32(math.Exp(float64(x - mx)))
		sum += v[i]
	}
	for i := range v {
		v[i] /= sum
	}
}

func addInPlace(a, b []float32) {
	for i := range a {
		a[i] += b[i]
	}
}

// parFor splits rows across rt.nWorkers goroutines, running sequentially for
// tiny ranges where spawn overhead would dominate. fn(lo, hi) handles the
// inclusive-exclusive row range [lo, hi).
func (rt *Runtime) parFor(rows int, fn func(lo, hi int)) {
	n := rt.nWorkers
	if n < 2 || rows < 2*n {
		fn(0, rows)
		return
	}
	chunk := (rows + n - 1) / n
	var wg sync.WaitGroup
	for lo := 0; lo < rows; lo += chunk {
		hi := lo + chunk
		if hi > rows {
			hi = rows
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(lo, hi)
		}()
	}
	wg.Wait()
}

// matVecIntoP computes out[r] = sum_j w[r*cols+j] * x[j] for every row,
// splitting the rows across workers so DRAM bandwidth is saturated.
func (rt *Runtime) matVecIntoP(out, w, x []float32, cols int) {
	rows := len(w) / cols
	rt.parFor(rows, func(lo, hi int) {
		for r := lo; r < hi; r++ {
			off := r * cols
			out[r] = dotProduct(w[off:off+cols], x)
		}
	})
}

var (
	dotOnce   sync.Once
	dotVecLen int
	// dotScratchPool hands each concurrent dotProduct its own reduction
	// buffer (64 floats covers AVX-512 and beyond), so goroutines never
	// share mutable scratch.
	dotScratchPool = sync.Pool{
		New: func() any { return make([]float32, 64) },
	}
)

// dotProduct computes sum(a[i]*b[i]) in SIMD chunks using the Go 1.27
// simd package (MulAdd accumulates element-wise; a final scalar pass
// reduces the vector). Hardware vector length is probed lazily.
func dotProduct(a, b []float32) float32 {
	dotOnce.Do(func() {
		dotVecLen = simd.LoadFloat32s(make([]float32, 64)).Len()
	})

	n := len(a)
	var acc simd.Float32s
	i := 0
	for ; i+dotVecLen <= n; i += dotVecLen {
		acc = simd.LoadFloat32s(a[i:]).MulAdd(simd.LoadFloat32s(b[i:]), acc)
	}
	scratch := dotScratchPool.Get().([]float32)
	acc.Store(scratch)

	var sum float32
	for _, v := range scratch {
		sum += v
	}
	dotScratchPool.Put(scratch)
	for ; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}

// Tokenizer wraps a llama2.c tokenizer.bin. Two vocab styles are supported:
// sentencepiece byte-fallback (bytes at ids 3-258, tokens 0-2 are
// <unk>/<s>/</s>) and byte-level BPE (ids 0..N hold specials/bytes/merges).
// The style is detected from the vocab contents.
type Tokenizer struct {
	vocab    []string
	scores   []float32
	rev      map[string]int
	maxLen   int
	byteBase int     // id of byte 0 (3 for sentencepiece, 190 for SmolLM2)
	byteToID [256]int // byte -> token id, authoritative for BPE initialization
}

// LoadTokenizer reads a llama2.c-format tokenizer.bin.
func LoadTokenizer(path string, vocabSize int) (*Tokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(data)
	var maxLen int32
	if err := binary.Read(r, binary.LittleEndian, &maxLen); err != nil {
		return nil, fmt.Errorf("tokenizer header: %w", err)
	}
	t := &Tokenizer{maxLen: int(maxLen), rev: make(map[string]int, vocabSize)}
	t.vocab = make([]string, vocabSize)
	t.scores = make([]float32, vocabSize)
	for i := 0; i < vocabSize; i++ {
		if err := binary.Read(r, binary.LittleEndian, &t.scores[i]); err != nil {
			return nil, fmt.Errorf("tokenizer entry %d: %w", i, err)
		}
		var n int32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, fmt.Errorf("tokenizer entry %d: %w", i, err)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("tokenizer entry %d: %w", i, err)
		}
		t.vocab[i] = string(buf)
	}
	for id, s := range t.vocab {
		t.rev[s] = id
	}
	t.detectByteBase()
	t.buildByteTable()
	return t, nil
}

// detectByteBase decides the byte-token offset. Sentencepiece tokenizers have
// "<0xNN>" byte tokens (byteBase 3); byte-level BPE has literal single-byte
// tokens. The result is only a fallback: buildByteTable scans the vocab for
// the authoritative mapping.
func (t *Tokenizer) detectByteBase() {
	t.byteBase = -1
	for _, s := range t.vocab {
		if byteTokRE.MatchString(s) {
			t.byteBase = 3
			break
		}
	}
	if t.byteBase < 0 {
		if id, ok := t.rev[string([]byte{0})]; ok {
			t.byteBase = id
		}
	}
	if t.byteBase < 0 {
		t.byteBase = 3
	}
}

// buildByteTable maps each byte to the id of its single-byte token. Byte-level
// BPE vocabularies scatter byte tokens across ids (printable ASCII first, the
// escaped rest later), so byteBase+byte arithmetic is wrong; scanning for
// single-byte strings is authoritative. Missing bytes fall back to byteBase+b
// for sentencepiece-style vocabularies.
func (t *Tokenizer) buildByteTable() {
	for b := 0; b < 256; b++ {
		t.byteToID[b] = -1
	}
	for id, s := range t.vocab {
		if len(s) == 1 {
			t.byteToID[int(s[0])] = id
		}
	}
	for b := 0; b < 256; b++ {
		if t.byteToID[b] < 0 {
			t.byteToID[b] = t.byteBase + b
		}
	}
}

// specialScoreThreshold marks added (special) tokens: the converter scores
// them far above any BPE merge so they are never treated as merge candidates.
const specialScoreThreshold = float32(1e8)

func (t *Tokenizer) isSpecial(id int) bool {
	return id >= 0 && id < len(t.scores) && t.scores[id] >= specialScoreThreshold
}

// Encode tokenizes text with byte-level BPE, matching llama2.c run.c. Special
// tokens (e.g. <|im_start|>) never participated in merges, so they are split
// out of the text first and emitted directly; the plain runs between them are
// BPE-encoded starting from single bytes and repeatedly merging the
// best-scoring consecutive pair.
func (t *Tokenizer) Encode(s string) []int {
	var out []int
	rest := s
	for rest != "" {
		// Emit the longest special token matching at the current position.
		spID, spLen := -1, 0
		for id := 0; id < len(t.vocab); id++ {
			if t.isSpecial(id) && len(t.vocab[id]) > spLen && strings.HasPrefix(rest, t.vocab[id]) {
				spID, spLen = id, len(t.vocab[id])
			}
		}
		if spID >= 0 {
			out = append(out, spID)
			rest = rest[spLen:]
			continue
		}
		// BPE-encode the plain run up to the next special token.
		next := len(rest)
		for id := 0; id < len(t.vocab); id++ {
			if t.isSpecial(id) {
				if idx := strings.Index(rest, t.vocab[id]); idx >= 0 && idx < next {
					next = idx
				}
			}
		}
		out = append(out, t.encodeBPE(rest[:next])...)
		rest = rest[next:]
	}
	return out
}

// encodeBPE runs byte-level BPE merging over a plain (special-free) run.
func (t *Tokenizer) encodeBPE(s string) []int {
	b := []byte(s)
	ids := make([]int, len(b))
	for i, c := range b {
		ids[i] = t.byteToID[int(c)]
	}
	for {
		bestScore := float32(-1e10)
		bestID, bestIdx := -1, -1
		for i := 0; i < len(ids)-1; i++ {
			if t.isSpecial(ids[i]) || t.isSpecial(ids[i+1]) {
				continue
			}
			merged := t.vocab[ids[i]] + t.vocab[ids[i+1]]
			if id, ok := t.rev[merged]; ok && !t.isSpecial(id) && t.scores[id] > bestScore {
				bestScore = t.scores[id]
				bestID, bestIdx = id, i
			}
		}
		if bestIdx == -1 {
			break
		}
		ids[bestIdx] = bestID
		copy(ids[bestIdx+1:], ids[bestIdx+2:])
		ids = ids[:len(ids)-1]
	}
	return ids
}

var byteTokRE = regexp.MustCompile(`^<0x([0-9A-Fa-f]{2})>$`)

// Decode maps a token id to its display string. Token ids 1 (<s>) and 2 (</s>,
// or <|im_end|> for SmolLM2) mark turn boundaries and decode to nothing.
// Sentencepiece byte tokens (<0xNN>) are emitted as their raw byte; after a
// <s> token a leading space is stripped, mirroring llama2.c's decode().
func (t *Tokenizer) Decode(prev, id int) string {
	if id < 0 || id >= len(t.vocab) {
		return ""
	}
	if id == 1 || id == 2 {
		return ""
	}
	piece := t.vocab[id]
	if t.byteBase == 3 && prev == 1 && len(piece) > 0 && piece[0] == ' ' {
		piece = piece[1:]
	}
	if m := byteTokRE.FindStringSubmatch(piece); m != nil {
		var b int
		fmt.Sscanf(m[1], "%x", &b)
		return string([]byte{byte(b)})
	}
	return piece
}

// sample returns a token drawn from the softmax(logits/temperature).
func sample(logits []float32, temperature float32, rng *rand.Rand) int {
	if temperature <= 0 {
		best := 0
		for i, v := range logits {
			if v > logits[best] {
				best = i
			}
		}
		return best
	}
	var sum float64
	for i, v := range logits {
		p := math.Exp(float64(v) / float64(temperature))
		logits[i] = float32(p)
		sum += p
	}
	r := rng.Float64() * sum
	acc := 0.0
	for i, v := range logits {
		acc += float64(v)
		if r < acc {
			return i
		}
	}
	return len(logits) - 1
}

// topLogits returns the indices of the k highest logit values (for debugging).
func topLogits(logits []float32, k int) []int {
	idx := make([]int, len(logits))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return logits[idx[a]] > logits[idx[b]] })
	return idx[:k]
}

func main() {
	modelPath := flag.String("model", "model.bin", "path to a llama2.c-format checkpoint")
	tokenizerPath := flag.String("tokenizer", "tokenizer.bin", "path to a llama2.c tokenizer.bin")
	temperature := flag.Float64("temp", 0.8, "sampling temperature (0 = greedy)")
	maxTokens := flag.Int("n", 200, "maximum tokens to generate per prompt")
	debugDump := flag.Bool("debug", false, "dump per-step top-5 tokens and logits")
	chatMode := flag.Bool("chat", false, "ChatML conversation mode (e.g. SmolLM2-135M-Instruct)")
	systemPrompt := flag.String("system", "You are a helpful assistant.", "system prompt for chat mode")
	tokensCSV := flag.String("tokens", "", "debug: feed this raw token sequence and dump top-10 logits")
	flag.Parse()

	model, err := LoadModel(*modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load model: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: GOEXPERIMENT=simd go run ./simd_inference.go -model model.bin")
		os.Exit(1)
	}

	cfg := model.Cfg
	fmt.Println("Go 1.27 SIMD inference engine (llama2.c format)")
	fmt.Printf("dim=%d hidden=%d layers=%d heads=%d kv_heads=%d vocab=%d seq_len=%d workers=%d\n",
		cfg.Dim, cfg.HiddenDim, cfg.NLayers, cfg.NHeads, cfg.NKVHeads, cfg.VocabSize, cfg.SeqLen, runtime.NumCPU())
	fmt.Printf("memory: weights=%.1f MB kv-cache=%.1f MB scratch=%.1f MB\n",
		mb(float64(model.weightBytes())), mb(float64(model.kvCacheBytes())), mb(float64(model.scratchBytes())))

	tok, err := LoadTokenizer(*tokenizerPath, cfg.VocabSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load tokenizer: %v\n", err)
		os.Exit(1)
	}

	rt := NewRuntime(model)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	if *tokensCSV != "" {
		var toks []int
		for _, p := range strings.Split(*tokensCSV, ",") {
			var v int
			if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err != nil {
				fmt.Fprintln(os.Stderr, "bad token list:", err)
				os.Exit(1)
			}
			toks = append(toks, v)
		}
		for i, tk := range toks {
			logits := rt.Forward(tk, i)
			top := topLogits(logits, 5)
			fmt.Printf("pos=%d:", i)
			for _, id := range top {
				fmt.Printf(" [%d %.4f]", id, logits[id])
			}
			fmt.Println()
		}
		return
	}

	if *chatMode {
		runChat(tok, rt, rng, *systemPrompt, float32(*temperature), *maxTokens, *debugDump)
		return
	}

	fmt.Println("Type a prompt and press Enter. Type 'exit' to quit.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for {
		fmt.Print("\nPrompt > ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		if input == "exit" {
			break
		}
		if input == "" {
			continue
		}

		tokens := tok.Encode(input)
		if len(tokens) >= cfg.SeqLen {
			tokens = tokens[:cfg.SeqLen-1]
		}
		if *debugDump {
			fmt.Printf("DEBUG prompt tokens: %v\n", tokens)
		}

		// Process the whole prompt, caching keys/values, so the model can
		// attend to every prompt token during generation.
		start := time.Now()
		prev := tokens[0]
		for i, tok := range tokens {
			rt.Forward(tok, i)
			prev = tok
		}

		_, genCount := generate(tok, rt, rng, prev, len(tokens), *maxTokens,
			float32(*temperature), map[int]bool{1: true, 2: true}, *debugDump)
		fmt.Println()

		elapsed := time.Since(start)
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		total := len(tokens) + genCount
		rate := float64(total) / elapsed.Seconds()
		fmt.Printf("--- profile: prompt=%d generated=%d total=%d | %s (%.1f tok/s) | goroutines=%d | gc=%d ---\n",
			len(tokens), genCount, total, elapsed.Round(time.Millisecond), rate,
			runtime.NumGoroutine(), ms.NumGC)
		fmt.Printf("--- profile: heap=%s total-alloc=%s sys=%s | final-gc-pause=%s ---\n",
			humanBytes(ms.HeapAlloc), humanBytes(ms.TotalAlloc), humanBytes(ms.Sys),
			time.Duration(ms.PauseTotalNs).Round(time.Millisecond))
	}
}

// generate runs autoregressive generation starting after the prompt (prev = last
// prompt token, pos = next position). Sampled tokens are decoded to stdout as
// they are produced; generation stops when a stop-token id is sampled (it is
// not emitted). Returns the reply text and the number of generated tokens.
func generate(tok *Tokenizer, rt *Runtime, rng *rand.Rand, prev, pos, maxTokens int, temperature float32, stops map[int]bool, debug bool) (string, int) {
	var sb strings.Builder
	genCount := 0
	for gen := 0; gen < maxTokens && pos < rt.m.Cfg.SeqLen; gen++ {
		logits := rt.Forward(prev, pos)
		pos++
		next := sample(logits, temperature, rng)
		if debug {
			top5 := topLogits(logits, 5)
			fmt.Printf("DEBUG pos=%d prev=%d -> next=%d top=%v\n", pos-1, prev, next, top5)
		}
		if stops[next] {
			break
		}
		s := tok.Decode(prev, next)
		os.Stdout.WriteString(s)
		sb.WriteString(s)
		prev = next
		genCount++
	}
	return sb.String(), genCount
}

// runChat is a REPL that keeps a ChatML conversation and answers each message.
func runChat(tok *Tokenizer, rt *Runtime, rng *rand.Rand, system string, temperature float32, maxTokens int, debug bool) {
	fmt.Println("ChatML mode (Ctrl-C or 'exit' to quit).")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var turns []string // alternating user/assistant message bodies

	for {
		fmt.Print("\nYou > ")
		if !scanner.Scan() {
			break
		}
		user := scanner.Text()
		if user == "exit" {
			break
		}
		if user == "" {
			continue
		}

		prompt := buildChatPrompt(system, turns, user)
		tokens := tok.Encode(prompt)
		if len(tokens) >= rt.m.Cfg.SeqLen {
			tokens = tokens[:rt.m.Cfg.SeqLen-1]
		}
		if debug {
			fmt.Printf("DEBUG prompt tokens: %v\n", tokens)
		}

		start := time.Now()
		prev := tokens[0]
		for i, tk := range tokens {
			rt.Forward(tk, i)
			prev = tk
		}

		fmt.Print("Assistant > ")
		reply, genCount := generate(tok, rt, rng, prev, len(tokens), maxTokens, temperature,
			map[int]bool{0: true, 1: true, 2: true}, debug)
		fmt.Println()

		elapsed := time.Since(start)
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		total := len(tokens) + genCount
		rate := float64(total) / elapsed.Seconds()
		fmt.Printf("--- profile: prompt=%d generated=%d total=%d | %s (%.1f tok/s) | goroutines=%d | gc=%d ---\n",
			len(tokens), genCount, total, elapsed.Round(time.Millisecond), rate, runtime.NumGoroutine(), ms.NumGC)
		fmt.Printf("--- profile: heap=%s total-alloc=%s sys=%s | final-gc-pause=%s ---\n",
			humanBytes(ms.HeapAlloc), humanBytes(ms.TotalAlloc), humanBytes(ms.Sys),
			time.Duration(ms.PauseTotalNs).Round(time.Millisecond))

		turns = append(turns, user, reply)
	}
}

// buildChatPrompt renders a ChatML prompt with a system message, prior turns,
// and the new user message. The final assistant turn is left open so the model
// completes it.
func buildChatPrompt(system string, turns []string, user string) string {
	var sb strings.Builder
	sb.WriteString("<|im_start|>system\n")
	sb.WriteString(system)
	sb.WriteString("<|im_end|>\n")
	for i := 0; i+1 < len(turns); i += 2 {
		sb.WriteString("<|im_start|>user\n")
		sb.WriteString(turns[i])
		sb.WriteString("<|im_end|>\n")
		sb.WriteString("<|im_start|>assistant\n")
		sb.WriteString(turns[i+1])
		sb.WriteString("<|im_end|>\n")
	}
	sb.WriteString("<|im_start|>user\n")
	sb.WriteString(user)
	sb.WriteString("<|im_end|>\n")
	sb.WriteString("<|im_start|>assistant\n")
	return sb.String()
}

// mb converts bytes to mebibytes.
func mb(b float64) float64 { return b / (1024 * 1024) }

// humanBytes renders a byte count in a compact IEC unit.
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

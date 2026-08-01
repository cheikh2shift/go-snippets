// convert turns a HuggingFace SmolLM2 checkpoint (safetensors + tokenizer.json)
// into llama2.c "legacy" (v0) format: a flat little-endian float32 .bin plus a
// tokenizer.bin. It streams tensor data in chunks so RAM stays flat, and
// transposes every weight from HF row-major to llama2.c column-major.
//
// Layout specifics handled here:
//   - GQA: wk/wv stay (kv_dim, dim); kv_mul = n_heads/n_kv_heads.
//   - RoPE: HF half-split pairs (real first half, imag second half) are
//     permuted to llama2.c interleaved pairs within each head's rows/columns.
//   - freqs_cos/sin are generated for theta=100000, seq_len, head_dim.
//
// Build/run:   go build -o conv.exe ./convert && .\conv.exe
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	outModel     = "smol.bin"
	outTokenizer = "smol_tokenizer.bin"
	inConfig     = "model/config.json"
	inTokenizer  = "model/tokenizer.json"
	inWeights    = "model/model.safetensors"
)

type hfConfig struct {
	HiddenSize     int `json:"hidden_size"`
	Intermediate   int `json:"intermediate_size"`
	NumLayers      int `json:"num_hidden_layers"`
	NumHeads       int `json:"num_attention_heads"`
	NumKVHeads     int `json:"num_key_value_heads"`
	VocabSize      int `json:"vocab_size"`
	MaxSeqLen      int `json:"max_position_embeddings"`
	RopeTheta      float64 `json:"rope_theta"`
	RopeInterleaved bool `json:"rope_interleaved"`
	RMSNormEps     float64 `json:"rms_norm_eps"`
	TieWordEmbed   bool `json:"tie_word_embeddings"`
}

func main() {
	cfg := loadConfig()
	if cfg.RopeInterleaved {
		fmt.Fprintln(os.Stderr, "unexpected: rope_interleaved=true")
		os.Exit(1)
	}
	if !cfg.TieWordEmbed {
		fmt.Fprintln(os.Stderr, "unexpected: tie_word_embeddings=false")
		os.Exit(1)
	}
	dim, hidden := cfg.HiddenSize, cfg.Intermediate
	hd := cfg.HiddenSize / cfg.NumHeads
	seqLen := 2048 // chat context; well within max_position_embeddings

	fmt.Printf("config: dim=%d hidden=%d layers=%d heads=%d kv_heads=%d vocab=%d seq_len=%d theta=%g\n",
		dim, hidden, cfg.NumLayers, cfg.NumHeads, cfg.NumKVHeads, cfg.VocabSize, seqLen, cfg.RopeTheta)

	convertTokenizer(cfg)

	fw, err := os.Create(outModel)
	if err != nil {
		fatal(err)
	}
	defer fw.Close()

	// Header: dim, hidden, layers, heads, kv_heads, vocab, seq_len.
	hdr := []int32{
		int32(dim), int32(hidden), int32(cfg.NumLayers), int32(cfg.NumHeads),
		int32(cfg.NumKVHeads), int32(cfg.VocabSize), int32(seqLen),
	}
	if err := binary.Write(fw, binary.LittleEndian, hdr); err != nil {
		fatal(err)
	}

	// One big read of the safetensors header (only the JSON portion).
	h = readSafeHeader(inWeights)
	get := func(name string) tensorInfo {
		t, ok := h.tensors[name]
		if !ok {
			fatalf("missing tensor %q", name)
		}
		return t
	}

	// Embeddings: (vocab, dim) -> [vocab][dim], no transpose.
	writeTensor(fw, get("model.embed_tokens.weight"), 1, 0, nil)

	ropePerm := makeRopePerm(hd) // maps interleaved index -> half-split index

	// llama2.c stores weights grouped by type across all layers, NOT
	// interleaved per layer: [attn_norm x L] [wq x L] [wk x L] [wv x L]
	// [wo x L] [ffn_norm x L] [w1 x L] [w2 x L] [w3 x L].
	attn := make([]tensorInfo, cfg.NumLayers)
	ffn := make([]tensorInfo, cfg.NumLayers)
	for l := 0; l < cfg.NumLayers; l++ {
		pre := fmt.Sprintf("model.layers.%d.", l)
		attn[l] = get(pre + "input_layernorm.weight")
		ffn[l] = get(pre + "post_attention_layernorm.weight")
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, attn[l], 1, 0, nil)
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, get(fmt.Sprintf("model.layers.%d.self_attn.q_proj.weight", l)), cfg.NumHeads, hd, ropePerm)
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, get(fmt.Sprintf("model.layers.%d.self_attn.k_proj.weight", l)), cfg.NumKVHeads, hd, ropePerm)
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, get(fmt.Sprintf("model.layers.%d.self_attn.v_proj.weight", l)), cfg.NumKVHeads, hd, ropePerm)
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensorCols(fw, get(fmt.Sprintf("model.layers.%d.self_attn.o_proj.weight", l)), cfg.NumHeads, hd, ropePerm)
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, ffn[l], 1, 0, nil)
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, get(fmt.Sprintf("model.layers.%d.mlp.gate_proj.weight", l)), 1, 0, nil) // w1
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, get(fmt.Sprintf("model.layers.%d.mlp.down_proj.weight", l)), 1, 0, nil) // w2
	}
	for l := 0; l < cfg.NumLayers; l++ {
		writeTensor(fw, get(fmt.Sprintf("model.layers.%d.mlp.up_proj.weight", l)), 1, 0, nil) // w3
	}

	writeTensor(fw, get("model.norm.weight"), 1, 0, nil)

	// freqs_cos/sin: [seqLen][hd/2].
	freqs := makeFreqs(seqLen, hd, cfg.RopeTheta)
	if err := binary.Write(fw, binary.LittleEndian, freqs.cos); err != nil {
		fatal(err)
	}
	if err := binary.Write(fw, binary.LittleEndian, freqs.sin); err != nil {
		fatal(err)
	}
	if err := fw.Close(); err != nil {
		fatal(err)
	}

	fi, _ := os.Stat(outModel)
	fmt.Printf("wrote %s (%.1f MB) + %s\n", outModel, float64(fi.Size())/(1024*1024), outTokenizer)
}

// ---- safetensors reading ----

type tensorInfo struct {
	dtype     string
	shape     []int64
	offset    int64
	startByte int64 // absolute offset of the tensor's raw bytes in the file
}

type safeHeader struct {
	tensors map[string]tensorInfo
	file    *os.File
}

var h *safeHeader

// readSafeHeader parses the JSON header and remembers the data-file start.
func readSafeHeader(path string) *safeHeader {
	f, err := os.Open(path)
	if err != nil {
		fatal(err)
	}
	// The header length is the first 8 bytes (usize, little-endian).
	var hlen uint64
	if err := binary.Read(f, binary.LittleEndian, &hlen); err != nil {
		fatal(err)
	}
	if hlen > 64*1024*1024 {
		fatalf("safetensors header too large: %d", hlen)
	}
	buf := make([]byte, hlen)
	if _, err := io.ReadFull(f, buf); err != nil {
		fatal(err)
	}
	var raw map[string]struct {
		Dtype string   `json:"dtype"`
		Shape []int64  `json:"shape"`
		Data  []int64  `json:"data_offsets"`
	}
	if err := json.Unmarshal(buf, &raw); err != nil {
		fatal(err)
	}
	h := &safeHeader{tensors: make(map[string]tensorInfo), file: f}
	// The first tensor's data starts after the header itself.
	dataStart := int64(8 + hlen)
	for name, t := range raw {
		if len(t.Data) != 2 {
			continue // e.g. "__metadata__"
		}
		h.tensors[name] = tensorInfo{
			dtype:     t.Dtype,
			shape:     t.Shape,
			offset:    t.Data[0],
			startByte: dataStart + t.Data[0],
		}
	}
	return h
}

// readTensor streams one tensor from disk, converting bf16/f32 to f32.
func (h *safeHeader) readTensor(t tensorInfo) []float32 {
	n := int64(1)
	for _, d := range t.shape {
		n *= d
	}
	if n == 0 {
		return nil
	}
	out := make([]float32, n)
	var raw []byte
	switch t.dtype {
	case "BF16":
		raw = make([]byte, n*2)
	case "F32":
		raw = make([]byte, n*4)
	default:
		fatalf("unsupported dtype %q", t.dtype)
	}
	if _, err := h.file.ReadAt(raw, t.startByte); err != nil {
		fatal(err)
	}
	switch t.dtype {
	case "BF16":
		for i := range out {
			out[i] = math.Float32frombits(uint32(binary.LittleEndian.Uint16(raw[i*2:])) << 16)
		}
	case "F32":
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	}
	return out
}

func (h *safeHeader) close() { h.file.Close() }

// ---- weight writing ----

// writeTensor writes a row-major weight in llama2.c order. For q/k/v, rows are
// grouped as nHeads blocks of hd consecutive rows; within each block the rows
// are reordered by perm (the interleaved<->half-split rope mapping).
func writeTensor(fw *os.File, t tensorInfo, nHeads, hd int, perm []int) {
	if len(t.shape) == 1 {
		w := h.readTensor(t)
		if err := binary.Write(fw, binary.LittleEndian, w); err != nil {
			fatal(err)
		}
		return
	}
	if len(t.shape) != 2 {
		fatalf("unexpected shape %v", t.shape)
	}
	rows, cols := int(t.shape[0]), int(t.shape[1])
	w := h.readTensor(t)
	defer func() { w = nil }()

	if nHeads <= 1 {
		if err := binary.Write(fw, binary.LittleEndian, w); err != nil {
			fatal(err)
		}
		return
	}
	if rows != nHeads*hd || len(perm) != hd {
		fatalf("shape/perm mismatch: rows=%d nHeads=%d hd=%d len(perm)=%d", rows, nHeads, hd, len(perm))
	}
	tmp := make([]float32, hd*cols)
	for g := 0; g < nHeads; g++ {
		base := g * hd * cols
		for p := 0; p < hd; p++ {
			src := base + perm[p]*cols
			copy(tmp[p*cols:(p+1)*cols], w[src:src+cols])
		}
		if err := binary.Write(fw, binary.LittleEndian, tmp); err != nil {
			fatal(err)
		}
	}
}

// writeTensorCols permutes columns within each head (o_proj: one head block
// of hd columns per query head).
func writeTensorCols(fw *os.File, t tensorInfo, nHeads, hd int, perm []int) {
	rows, cols := int(t.shape[0]), int(t.shape[1])
	if cols != nHeads*hd || len(perm) != hd {
		fatalf("o_proj shape/perm mismatch: cols=%d nHeads=%d hd=%d len(perm)=%d", cols, nHeads, hd, len(perm))
	}
	w := h.readTensor(t)
	defer func() { w = nil }()

	if nHeads <= 1 {
		if err := binary.Write(fw, binary.LittleEndian, w); err != nil {
			fatal(err)
		}
		return
	}
	tmp := make([]float32, cols)
	for r := 0; r < rows; r++ {
		rowBase := r * cols
		for g := 0; g < nHeads; g++ {
			colBase := g * hd
			for p := 0; p < hd; p++ {
				tmp[colBase+p] = w[rowBase+colBase+perm[p]]
			}
		}
		copy(w[rowBase:rowBase+cols], tmp)
	}
	if err := binary.Write(fw, binary.LittleEndian, w); err != nil {
		fatal(err)
	}
}

// ---- rope ----

// makeRopePerm maps an interleaved index p to the half-split index g(p):
// g(2j)=j, g(2j+1)=j+hd/2. Row/column p of the converted weight equals row/
// column g(p) of the original HF weight.
func makeRopePerm(hd int) []int {
	perm := make([]int, hd)
	half := hd / 2
	for j := 0; j < half; j++ {
		perm[2*j] = j
		perm[2*j+1] = j + half
	}
	return perm
}

func makeFreqs(seqLen, headDim int, theta float64) (out struct{ cos, sin []float32 }) {
	half := headDim / 2
	out.cos = make([]float32, seqLen*half)
	out.sin = make([]float32, seqLen*half)
	for pos := 0; pos < seqLen; pos++ {
		for j := 0; j < half; j++ {
			inv := float64(pos) / math.Pow(theta, float64(2*j)/float64(headDim))
			out.cos[pos*half+j] = float32(math.Cos(inv))
			out.sin[pos*half+j] = float32(math.Sin(inv))
		}
	}
	return out
}

// ---- tokenizer ----

func convertTokenizer(cfg hfConfig) {
	data, err := os.ReadFile(inTokenizer)
	if err != nil {
		fatal(err)
	}
	var j struct {
		Model struct {
			Vocab     map[string]int `json:"vocab"`
			Merges    []string       `json:"merges"`
			ByteLevel struct {
				IgnoreMerges bool `json:"ignore_merges"`
			} `json:"byte_level"`
		} `json:"model"`
		AddedTokens []struct {
			ID      int    `json:"id"`
			Content string `json:"content"`
		} `json:"added_tokens"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		fatal(err)
	}

	vocab := make([]string, cfg.VocabSize)
	// Base vocab is stored in GPT-2 byte-level escaped form: each byte is a
	// single Unicode char (printable bytes map to themselves, others to
	// U+0100..). Unescape so tokenizer.bin holds the real byte sequences.
	for tok, id := range j.Model.Vocab {
		if id < len(vocab) {
			vocab[id] = unescapeBytes(tok)
		}
	}
	// Added (special) tokens override base entries by id and stay literal.
	for _, at := range j.AddedTokens {
		if at.ID < len(vocab) {
			vocab[at.ID] = at.Content
		}
	}
	// Sanity: every slot filled.
	missing := 0
	maxLen := 0
	for id, s := range vocab {
		if s == "" {
			missing++
			vocab[id] = fmt.Sprintf("<MISSING_%d>", id)
		}
		if len(s) > maxLen {
			maxLen = len(s)
		}
	}
	if missing > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d vocab slots missing, patched\n", missing)
	}

	// Token scores: byte-level BPE has no scores, so rank merges: earlier
	// entries in the merges list are higher priority (rank 0 = first), which
	// maps to a higher score for the "best merge wins" engine loop.
	score := make([]float32, len(vocab))
	numMerges := len(j.Model.Merges)
	for i, m := range j.Model.Merges {
		parts := bytes.Fields([]byte(m))
		if len(parts) == 2 {
			merged := string(parts[0]) + string(parts[1])
			if id, ok := j.Model.Vocab[merged]; ok {
				score[id] = float32(numMerges - i)
			}
		}
	}
	// Special (added) tokens are marked far above any merge score so the engine
	// can split them out of the text before BPE (they never participate in
	// merges). The engine's specialScoreThreshold is 1e8.
	for _, at := range j.AddedTokens {
		if at.ID < len(score) {
			score[at.ID] = 1e9 + float32(at.ID)
		}
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, int32(maxLen))
	for id := 0; id < len(vocab); id++ {
		binary.Write(&buf, binary.LittleEndian, score[id])
		b := []byte(vocab[id])
		binary.Write(&buf, binary.LittleEndian, int32(len(b)))
		buf.Write(b)
	}
	if err := os.WriteFile(outTokenizer, buf.Bytes(), 0644); err != nil {
		fatal(err)
	}

	// Print the special tokens found so the engine can hardcode chat stops.
	fmt.Printf("tokenizer: vocab=%d max_len=%d specials=", len(vocab), maxLen)
	for _, id := range []int{0, 1, 2, 3} {
		if id < len(vocab) {
			fmt.Printf("[%d]=%q ", id, vocab[id])
		}
	}
	fmt.Println()
}

func loadConfig() hfConfig {
	data, err := os.ReadFile(inConfig)
	if err != nil {
		fatal(err)
	}
	var cfg hfConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fatal(err)
	}
	if cfg.RopeTheta == 0 {
		cfg.RopeTheta = 100000
	}
	if cfg.RMSNormEps == 0 {
		cfg.RMSNormEps = 1e-5
	}
	return cfg
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fatal:", err)
	os.Exit(1)
}

// charToByte is the inverse of GPT-2's byte-to-unicode table used by byte-level
// BPE tokenizers: printable bytes (0x21-0x7E, 0xA1-0xAC, 0xAE-0xFF) map to
// themselves, all others map to U+0100.. in byte order.
var charToByte = func() map[rune]byte {
	m := make(map[rune]byte, 256)
	var n int
	for b := 0; b < 256; b++ {
		if (b >= 0x21 && b <= 0x7E) || (b >= 0xA1 && b <= 0xAC) || (b >= 0xAE && b <= 0xFF) {
			m[rune(b)] = byte(b)
		} else {
			m[rune(0x100+n)] = byte(b)
			n++
		}
	}
	return m
}()

// unescapeBytes converts a byte-level BPE vocab string to the real byte
// sequence it represents.
func unescapeBytes(s string) string {
	rs := []rune(s)
	out := make([]byte, 0, len(rs))
	for _, r := range rs {
		if b, ok := charToByte[r]; ok {
			out = append(out, b)
		} else {
			out = append(out, string(r)...)
		}
	}
	return string(out)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fatal: "+format+"\n", args...)
	os.Exit(1)
}

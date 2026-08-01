// ref implements the exact HuggingFace Llama forward pass (half-split RoPE,
// theta from config) directly from the SmolLM2-135M-Instruct safetensors and
// prints top-10 logits for a raw token sequence. This is the ground truth for
// verifying that convert/main.go produced a correct llama2.c .bin.
//
// Build/run:   go build -o ref.exe ./ref && .\ref.exe 1,100,200,300
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type hfConfig struct {
	HiddenSize     int     `json:"hidden_size"`
	Intermediate   int     `json:"intermediate_size"`
	NumLayers      int     `json:"num_hidden_layers"`
	NumHeads       int     `json:"num_attention_heads"`
	NumKVHeads     int     `json:"num_key_value_heads"`
	VocabSize      int     `json:"vocab_size"`
	MaxSeqLen      int     `json:"max_position_embeddings"`
	RopeTheta      float64 `json:"rope_theta"`
	RopeInterleaved bool   `json:"rope_interleaved"`
	RMSNormEps     float64 `json:"rms_norm_eps"`
	TieWordEmbed   bool    `json:"tie_word_embeddings"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ref.exe <token,csv>")
		os.Exit(1)
	}
	var toks []int
	for _, p := range strings.Split(os.Args[1], ",") {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			fatal(err)
		}
		toks = append(toks, v)
	}

	cfg := loadConfig()
	if cfg.RopeInterleaved {
		fatalf("ref only supports rope_interleaved=false")
	}

	w := loadTensors()
	dim, hidden, kvDim := cfg.HiddenSize, cfg.Intermediate, cfg.NumKVHeads*(cfg.HiddenSize/cfg.NumHeads)
	hd := dim / cfg.NumHeads
	kvMul := cfg.NumHeads / cfg.NumKVHeads

	cos, sin := freqs(cfg, 256)
	keyCache := make([][]float32, cfg.NumLayers)
	valCache := make([][]float32, cfg.NumLayers)
	for l := 0; l < cfg.NumLayers; l++ {
		keyCache[l] = make([]float32, 256*kvDim)
		valCache[l] = make([]float32, 256*kvDim)
	}

	x := make([]float32, dim)
	xb := make([]float32, dim)
	xb2 := make([]float32, dim)
	hb := make([]float32, hidden)
	hb2 := make([]float32, hidden)
	q := make([]float32, dim)
	k := make([]float32, kvDim)
	v := make([]float32, kvDim)
	att := make([]float32, 256)

	lay := func(l int, name string) []float32 { return w[fmt.Sprintf("model.layers.%d.%s", l, name)] }

	for pos, token := range toks {
		emb := w["model.embed_tokens.weight"]
		copy(x, emb[token*dim:(token+1)*dim])

		for l := 0; l < cfg.NumLayers; l++ {
			rmsNorm(xb, x, lay(l, "input_layernorm.weight"), cfg.RMSNormEps)

			matMul(q, lay(l, "self_attn.q_proj.weight"), xb)
			matMul(k, lay(l, "self_attn.k_proj.weight"), xb)
			matMul(v, lay(l, "self_attn.v_proj.weight"), xb)

			ropeHF(q, pos, hd, cos, sin)
			ropeHF(k, pos, hd, cos, sin)

			copy(keyCache[l][pos*kvDim:(pos+1)*kvDim], k)
			copy(valCache[l][pos*kvDim:(pos+1)*kvDim], v)

			// Attention per query head.
			for h := 0; h < cfg.NumHeads; h++ {
				qh := q[h*hd : (h+1)*hd]
				kvh := (h / kvMul) * hd
				for t := 0; t <= pos; t++ {
					kt := keyCache[l][t*kvDim+kvh : t*kvDim+kvh+hd]
					var s float32
					for i := 0; i < hd; i++ {
						s += qh[i] * kt[i]
					}
					att[t] = s / float32(math.Sqrt(float64(hd)))
				}
				softmax(att[:pos+1])
				oh := xb2[h*hd : (h+1)*hd]
				clear(oh)
				for t := 0; t <= pos; t++ {
					vt := valCache[l][t*kvDim+kvh : t*kvDim+kvh+hd]
					a := att[t]
					for i := 0; i < hd; i++ {
						oh[i] += a * vt[i]
					}
				}
			}

			matMul(xb, lay(l, "self_attn.o_proj.weight"), xb2)
			for i := 0; i < dim; i++ {
				x[i] += xb[i]
			}

			rmsNorm(xb, x, lay(l, "post_attention_layernorm.weight"), cfg.RMSNormEps)
			matMul(hb, lay(l, "mlp.gate_proj.weight"), xb)
			matMul(hb2, lay(l, "mlp.up_proj.weight"), xb)
			for i := 0; i < hidden; i++ {
				hb[i] = swish(hb[i]) * hb2[i]
			}
			matMul(xb, lay(l, "mlp.down_proj.weight"), hb)
			for i := 0; i < dim; i++ {
				x[i] += xb[i]
			}
		}

		rmsNorm(x, x, w["model.norm.weight"], cfg.RMSNormEps)

		// Logits: x . embed (tied embeddings).
		emb = w["model.embed_tokens.weight"]
		logits := make([]float32, cfg.VocabSize)
		for t := 0; t < cfg.VocabSize; t++ {
			var s float32
			base := emb[t*dim : (t+1)*dim]
			for i := 0; i < dim; i++ {
				s += x[i] * base[i]
			}
			logits[t] = s
		}
		top := topIndices(logits, 5)
		fmt.Printf("pos=%d:", pos)
		for _, id := range top {
			fmt.Printf(" [%d %.4f]", id, logits[id])
		}
		fmt.Println()
	}
}

// ropeHF applies HuggingFace's half-split rotary embedding.
func ropeHF(vec []float32, pos, headDim int, cos, sin []float32) {
	half := headDim / 2
	for h := 0; h < len(vec)/headDim; h++ {
		base := h * headDim
		for j := 0; j < half; j++ {
			c := cos[pos*half+j]
			s := sin[pos*half+j]
			r := vec[base+j]
			im := vec[base+half+j]
			vec[base+j] = r*c - im*s
			vec[base+half+j] = r*s + im*c
		}
	}
}

func freqs(cfg hfConfig, seqLen int) (cos, sin []float32) {
	hd := cfg.HiddenSize / cfg.NumHeads
	half := hd / 2
	theta := cfg.RopeTheta
	if theta == 0 {
		theta = 100000
	}
	cos = make([]float32, seqLen*half)
	sin = make([]float32, seqLen*half)
	for pos := 0; pos < seqLen; pos++ {
		for j := 0; j < half; j++ {
			inv := float64(pos) / math.Pow(theta, float64(2*j)/float64(hd))
			cos[pos*half+j] = float32(math.Cos(inv))
			sin[pos*half+j] = float32(math.Sin(inv))
		}
	}
	return cos, sin
}

func matMul(out, w, x []float32) {
	cols := len(x)
	rows := len(w) / cols
	for r := 0; r < rows; r++ {
		var s float32
		base := w[r*cols : (r+1)*cols]
		for j := 0; j < cols; j++ {
			s += base[j] * x[j]
		}
		out[r] = s
	}
}

func rmsNorm(out, x, w []float32, eps float64) {
	var ss float32
	for _, v := range x {
		ss += v * v
	}
	inv := float32(1.0 / math.Sqrt(float64(ss)/float64(len(x))+eps))
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

func topIndices(v []float32, k int) []int {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[idx[j]] > v[idx[j-1]]; j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	return idx[:k]
}

// ---- safetensors loading (everything into memory as float32) ----

func loadTensors() map[string][]float32 {
	f, err := os.Open("model/model.safetensors")
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	var hlen uint64
	if err := binary.Read(f, binary.LittleEndian, &hlen); err != nil {
		fatal(err)
	}
	hdr := make([]byte, hlen)
	if _, err := f.Read(hdr); err != nil {
		fatal(err)
	}
	var raw map[string]struct {
		Dtype string  `json:"dtype"`
		Shape []int64 `json:"shape"`
		Data  []int64 `json:"data_offsets"`
	}
	if err := json.Unmarshal(hdr, &raw); err != nil {
		fatal(err)
	}
	all, err := os.ReadFile("model/model.safetensors")
	if err != nil {
		fatal(err)
	}
	data := all[8+hlen:]
	w := make(map[string][]float32)
	for name, t := range raw {
		if len(t.Data) != 2 {
			continue
		}
		n := int64(1)
		for _, d := range t.Shape {
			n *= d
		}
		out := make([]float32, n)
		chunk := data[t.Data[0]:t.Data[1]]
		if t.Dtype == "BF16" {
			for i := range out {
				out[i] = math.Float32frombits(uint32(binary.LittleEndian.Uint16(chunk[i*2:])) << 16)
			}
		} else if t.Dtype == "F32" {
			for i := range out {
				out[i] = math.Float32frombits(binary.LittleEndian.Uint32(chunk[i*4:]))
			}
		}
		w[name] = out
	}
	return w
}

func loadConfig() hfConfig {
	data, err := os.ReadFile("model/config.json")
	if err != nil {
		fatal(err)
	}
	var cfg hfConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fatal(err)
	}
	return cfg
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fatal:", err)
	os.Exit(1)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fatal: "+format+"\n", args...)
	os.Exit(1)
}

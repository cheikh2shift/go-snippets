# inference-engine

A SIMD-accelerated llama2.c-format inference engine written in Go (see
`simd_inference.go`), plus tooling to download and convert HuggingFace models
into the llama2.c checkpoint format.

## Models

### SmolLM2-135M-Instruct (chat)

Source: https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct

Direct download links (HuggingFace `resolve/main`):

- https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct/resolve/main/config.json
- https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct/resolve/main/tokenizer.json
- https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct/resolve/main/tokenizer_config.json
- https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct/resolve/main/special_tokens_map.json
- https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct/resolve/main/model.safetensors

Easiest: use the included resumable downloader, which fetches all of the above
into `model/`:

    GOEXPERIMENT=simd go build -o dl.exe ./download && .\dl.exe

Convert the downloaded checkpoint into llama2.c format:

    GOEXPERIMENT=simd go build -o conv.exe ./convert && .\conv.exe

Run a chat session (from `smol.bin` / `smol_tokenizer.bin`):

    GOEXPERIMENT=simd go build -o simd_inf.exe ./simd_inference.go
    .\simd_inf.exe -model smol.bin -tokenizer smol_tokenizer.bin -chat

### TinyStories 42M (story generation)

Source: https://huggingface.co/karpathy/tinyllamas

Direct download links:

- https://huggingface.co/karpathy/tinyllamas/resolve/main/stories42M.bin
- https://huggingface.co/karpathy/tinyllamas/resolve/main/tokenizer.model

Run it (`model.bin` / `tokenizer.bin`):

    .\simd_inf.exe -model model.bin -tokenizer tokenizer.bin

# go-docs-rag

A tiny **retrieval-augmented generation (RAG)** demo over the official Go docs, in about 900 lines of Go (plus tests), standard library only. It runs entirely on your machine with [Ollama](https://ollama.com): no API keys, no vector database, no cost.

It's the companion repo for **"Do You Still Need RAG? (Context Windows Got Huge)"** in the *System Design in 2026* series on YouTube: [FILL IN: video link].

```
$ go run . ask "how do I generate a UUID in Go?"

question: how do I generate a UUID in Go?
mode: hybrid · top 3

[1] Go 1.27 Release Notes › Standard library › New uuid package   (score 0.033)
    The new `uuid` package generates and parses UUIDs.
    https://go.dev/doc/go1.27#uuid
...
answer (llama3.2):
Use the new standard library `uuid` package, added in Go 1.27 [1].
```
(Illustrative output: scores and the exact answer depend on your models.)

## Quick start

You need **Go 1.22+** and **Ollama** running.

```sh
ollama pull nomic-embed-text   # turns text into vectors (~270 MB)
ollama pull llama3.2           # writes the answer (~2 GB)

git clone https://github.com/marianina8/go-docs-rag
cd go-docs-rag
go run . index                 # download ~30 doc pages, chunk, embed, save (a few minutes)
go run . ask "how do I generate a UUID in Go?"
go run . eval                  # test retrieval on its own
```

Use other models with `RAG_EMBED_MODEL` and `RAG_CHAT_MODEL` (re-run `index` if you change the embedding model). `OLLAMA_HOST` works as usual.

## How it works

The two halves of RAG, one file each:

| Step | What happens | Code |
|---|---|---|
| **Index** (once, ahead of time) | | |
| 0 · fetch | Download ~30 pages of go.dev docs (release notes 1.21–1.27, Effective Go, the FAQ, the GC guide, modules, tutorials) from the [golang/website](https://github.com/golang/website) repo | `docs.go` |
| 1 · chunk | Split each page at its headings, so each chunk is about one thing. Long sections are cut at paragraph breaks (max 1,200 chars). Each chunk keeps its breadcrumb and link | `chunk.go` |
| 2 · embed | Turn each chunk into a vector with `nomic-embed-text`. The breadcrumb is embedded with the text, so "generates and parses UUIDs" still knows it's from Go 1.27 | `ollama.go` |
| 3 · store | Save every chunk and its vector to `data/index.json`. That's the whole "vector index" | `index.go` |
| **Ask** (every question) | | |
| 4 · retrieve | Embed the question, compare it with every chunk (cosine similarity), keep the **top K** (default 3). A plain loop is fast enough for ~1,000 chunks | `search.go` |
| 5 · window | Build the prompt: **instructions first, then the chunks, then the question last** | `main.go` (`buildPrompt`) |
| 6 · answer | The model answers from those chunks only, citing `[1]`, `[2]`, streamed as it's generated | `ollama.go` (`Chat`) |

## Three search modes

```sh
go run . ask --mode vector  "..."   # meaning: embeddings + cosine
go run . ask --mode keyword "..."   # exact words: BM25, the classic search-engine formula
go run . ask --mode hybrid  "..."   # both, merged with Reciprocal Rank Fusion (default)
```

Add `--no-answer` to see only what retrieval found, and `--k 5` to change top K.

## Things to try (where RAG breaks)

1. **Exact words.** `asynctimerchan` is a real GODEBUG setting. Compare:
   ```sh
   go run . ask --mode vector  --no-answer "what does asynctimerchan do?"
   go run . ask --mode keyword --no-answer "what does asynctimerchan do?"
   ```
   Embeddings are good at meaning and weaker at exact strings; keyword search finds the string. Hybrid gets both.
2. **The lookalike (an older answer).** The Go FAQ has an entry, *"Why does Go not permit methods to declare type parameters?"*, but Go 1.27 added generic methods. Ask:
   ```sh
   go run . ask --no-answer "Can a method declare its own type parameters?"
   ```
   and check whether the FAQ chunk or the Go 1.27 release notes come back first. Both are "close on the map." Only one is current.
3. **With and without RAG.** Ask the same model directly, with no retrieval:
   ```sh
   ollama run llama3.2 "How do I generate a UUID in Go? Answer in two sentences."
   ```
   A model trained before Go 1.27 (August 2026) may not know the standard library has a `uuid` package now.

## Test retrieval on its own

`evals.json` holds 12 real questions, each with the chunk that answers it. `go run . eval` runs only the search (no model answers) in all three modes and reports whether the right chunk came back in the top K:

```
question                                    keyword  vector   hybrid
How do I generate a UUID in Go?             ✓ #1     ...      ...
...
right chunk in top 3:  keyword 10/12  vector ?/12  hybrid ?/12
```

If retrieval misses, no prompt will save the answer: the model can't use what it never saw. Add your own questions to `evals.json` (the `expect` field is a chunk ID or its start, like `go1.27#uuid`; `ask` doesn't print IDs, but `data/index.json` has them).

## Not in this demo (on purpose)

A vector database, re-ranking with a cross-encoder model, incremental re-indexing, and chunk overlap. They matter at scale; at ~1,000 chunks they'd hide how simple the core idea is.

## Credits

- Go documentation © The Go Authors, licensed [CC BY 4.0](https://go.dev/copyright). This repo downloads it at `index` time and doesn't redistribute it.
- Prior art worth reading: Eli Bendersky, [Retrieval Augmented Generation in Go](https://eli.thegreenplace.net/2023/retrieval-augmented-generation-in-go/) (2023).
- Hybrid search idea: Anthropic, [Contextual Retrieval](https://www.anthropic.com/engineering/contextual-retrieval) (2024).

Code is MIT licensed.

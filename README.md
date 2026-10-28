# go-docs-rag

A tiny **retrieval-augmented generation (RAG)** demo over the official Go docs, in about 900 lines of Go (plus tests), standard library only. It runs entirely on your machine with [Ollama](https://ollama.com): no API keys, no vector database, no cost.

It's the companion repo for **"Do You Still Need RAG? (Context Windows Got Huge)"** in the *System Design in 2026* series on YouTube: [FILL IN: video link].

A real run (nomic-embed-text + llama3.2, October 2026):

```
$ go run . ask "how do I generate a UUID in Go?"

question: how do I generate a UUID in Go?
mode: hybrid · top 3

[1] Go 1.27 Release Notes › Standard library › New uuid package   (score 0.033)
    The new `uuid` package generates and parses UUIDs.
    https://go.dev/doc/go1.27#uuid

[2] Go 1.24 Release Notes › Linker   (score 0.032)
    The linker now generates a GNU build ID (the ELF `NT_GNU_BUILD_ID` note) on ELF platforms and a UUID (the Mach…
    https://go.dev/doc/go1.24#linker

[3] Go 1.27 Release Notes › Ports › PowerPC   (score 0.029)
    On the big-endian 64-bit PowerPC port on Linux (`GOOS=linux` `GOARCH=ppc64`), the Go toolchain now generates b…
    https://go.dev/doc/go1.27#ppc64

answer (llama3.2):
According to [1], you can generate a UUID in Go using the `uuid.New()` function.
```

Retrieval found the right page. Now check the citation: source [1] never mentions `uuid.New()`. The function does exist ([pkg.go.dev/uuid](https://pkg.go.dev/uuid)), so the answer happens to be right, but the model filled that part in from training, not from the source. That's why RAG answers cite: so you can check them.

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

1. **The lookalike: an outdated answer that's "close on the map."** The Go FAQ still has *"Why does Go not support methods with type parameters?"*, but Go 1.27 added generic methods. Compare vector-only search with hybrid:
   ```sh
   go run . ask --mode vector "Can a method declare its own type parameters?"
   go run . ask --mode hybrid "Can a method declare its own type parameters?"
   ```
   In our run, vector-only search missed the Go 1.27 release notes in its top 3; hybrid ranked them #1 and the FAQ #2. Read which answer each one gives, and which source it cites.
2. **Exact words.** Embeddings are good at meaning and can blur exact strings like error codes and setting names; keyword search matches them literally. Try a real GODEBUG setting:
   ```sh
   go run . ask --mode vector  --no-answer "what does asynctimerchan do?"
   go run . ask --mode keyword --no-answer "what does asynctimerchan do?"
   ```
   Honest result: in our run, vector search found it fine (#1), because the word appears in several chunks and their breadcrumbs. Try rarer strings from the release notes and see where it breaks.
3. **With and without RAG.** Ask the same model directly, with no retrieval:
   ```sh
   ollama run llama3.2 "How do I generate a UUID in Go? Answer in two sentences."
   ```
   A model trained before Go 1.27 (August 2026) may not know the standard library has a `uuid` package now.

## Test retrieval on its own

`evals.json` holds 12 real questions, each with the chunk (or chunks) that answer it. `go run . eval` runs only the search (no model answers) in all three modes and reports whether the right chunk came back in the top K. A real run:

```
question                                             keyword  vector   hybrid
How do I generate a UUID in Go?                      ✓ #1     ✓ #1     ✓ #1
What does asynctimerchan do?                         ✓ #3     ✓ #1     ✓ #1
Can a method declare its own type parameters?        ✓ #1     ✗        ✓ #1
What is the GOTOOLCHAIN setting?                     ✓ #1     ✓ #1     ✓ #1
How do I add a dependency to my module?              ✓ #2     ✓ #1     ✓ #1
How does GOGC change how often the garbage collect…  ✓ #2     ✓ #1     ✓ #1
How do I set a memory limit for the Go runtime?      ✓ #1     ✓ #2     ✓ #1
Should getter methods start with Get?                ✓ #1     ✓ #1     ✓ #1
Can I range over an integer?                         ✓ #1     ✗        ✓ #2
How do I write a fuzz test?                          ✗        ✗        ✗
How do I set up a workspace with multiple modules?   ✗        ✗        ✗
What is profile-guided optimization?                 ✓ #3     ✓ #2     ✓ #2

right chunk in top 3:  keyword 10/12  vector 8/12  hybrid 10/12
```

Two lessons from that table:
- **Hybrid wins** where vector alone misses (generic methods, range over int).
- **The test can be wrong too.** The fuzz miss was a bad label: the chunk that really answers it is the tutorial's "Add a fuzz test › Write the code," not the one we'd picked. `expect` now accepts a list for questions with more than one right chunk, so your run will show that row fixed. The workspaces miss is real: the tutorial's setup steps sit under generic headings.

If retrieval misses, no prompt will save the answer: the model can't use what it never saw. Add your own questions to `evals.json`. The `expect` field is a chunk ID or its start, like `go1.27#uuid`, or a list of them. `ask` doesn't print IDs, but `data/index.json` has them.

## Not in this demo (on purpose)

A vector database, re-ranking with a cross-encoder model, incremental re-indexing, and chunk overlap. They matter at scale; at ~1,000 chunks they'd hide how simple the core idea is.

## Credits

- Go documentation © The Go Authors, licensed [CC BY 4.0](https://go.dev/copyright). This repo downloads it at `index` time and doesn't redistribute it.
- Prior art worth reading: Eli Bendersky, [Retrieval Augmented Generation in Go](https://eli.thegreenplace.net/2023/retrieval-augmented-generation-in-go/) (2023).
- Hybrid search idea: Anthropic, [Contextual Retrieval](https://www.anthropic.com/engineering/contextual-retrieval) (2024).

Code is MIT licensed.

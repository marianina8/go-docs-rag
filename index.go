package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Index is the whole "vector index": every chunk with its embedding, in one JSON file.
type Index struct {
	EmbedModel string  `json:"embed_model"`
	Chunks     []Chunk `json:"chunks"`

	bm25 *BM25 // built on load, not saved
}

// buildIndex is part one of RAG, done ahead of time: chunk, embed, store.
func buildIndex(o *Ollama, docsDir, out string) error {
	docs, err := loadDocs(docsDir)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no .md or .html files in %s", docsDir)
	}
	names := make([]string, 0, len(docs))
	for name := range docs {
		names = append(names, name)
	}
	sort.Strings(names)

	// 1. chunk
	ix := &Index{EmbedModel: o.EmbedModel}
	for _, name := range names {
		ix.Chunks = append(ix.Chunks, chunkDoc(name, docs[name])...)
	}
	fmt.Printf("1 chunk:  %d docs -> %d chunks\n", len(docs), len(ix.Chunks))

	// 2. embed, a batch at a time
	const batch = 16
	for i := 0; i < len(ix.Chunks); i += batch {
		end := min(i+batch, len(ix.Chunks))
		texts := make([]string, 0, end-i)
		for _, c := range ix.Chunks[i:end] {
			texts = append(texts, c.EmbedText())
		}
		vecs, err := o.Embed(texts, false)
		if err != nil {
			return err
		}
		for j, v := range vecs {
			ix.Chunks[i+j].Vector = v
		}
		fmt.Printf("\r2 embed:  %d/%d chunks with %s", end, len(ix.Chunks), o.EmbedModel)
	}
	fmt.Println()

	// 3. store
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(ix); err != nil {
		return err
	}
	dims := 0
	if len(ix.Chunks) > 0 {
		dims = len(ix.Chunks[0].Vector)
	}
	fmt.Printf("3 store:  %s (%d chunks x %d numbers)\n", out, len(ix.Chunks), dims)
	return nil
}

func loadIndex(path string) (*Index, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w\n\nBuild the index first:  go run . index", err)
	}
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, err
	}
	ix.bm25 = newBM25(&ix)
	return &ix, nil
}

// retrieve is part two, on every question: find the k best chunks.
// mode is "vector" (meaning), "keyword" (exact words) or "hybrid" (both).
func retrieve(o *Ollama, ix *Index, question, mode string, k int) ([]Hit, error) {
	const pool = 20 // candidates from each search before fusing
	switch mode {
	case "keyword":
		return ix.bm25.search(question, k), nil
	case "vector", "hybrid":
		if o.EmbedModel != ix.EmbedModel {
			return nil, fmt.Errorf("index was built with %s but RAG_EMBED_MODEL is %s; rebuild the index", ix.EmbedModel, o.EmbedModel)
		}
		vecs, err := o.Embed([]string{question}, true)
		if err != nil {
			return nil, err
		}
		if mode == "vector" {
			return vectorSearch(ix, vecs[0], k), nil
		}
		return fuse([][]Hit{vectorSearch(ix, vecs[0], pool), ix.bm25.search(question, pool)}, k), nil
	default:
		return nil, fmt.Errorf("unknown mode %q (use vector, keyword or hybrid)", mode)
	}
}

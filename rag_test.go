package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleDoc = `---
title: Go 1.27 Release Notes
---

## Standard library {#library}

### New uuid package {#uuid}

The new ` + "`uuid`" + ` package generates and parses UUIDs.

### Minor changes {#minor}

#### time

The asynctimerchan GODEBUG setting has been removed permanently. Channels created by package time are now always unbuffered.

## Runtime {#runtime}

` + "```" + `
## not a heading, this is inside a code block
` + "```" + `
Goroutine leak profiles are now generally available in runtime/pprof for everyone.
`

func TestChunkDoc(t *testing.T) {
	cs := chunkDoc("go1.27", sampleDoc)
	got := map[string]Chunk{}
	for _, c := range cs {
		got[c.ID] = c
	}
	uuid, ok := got["go1.27#uuid"]
	if !ok {
		t.Fatalf("no go1.27#uuid chunk; got %v", keys(got))
	}
	if uuid.Title() != "Go 1.27 Release Notes › Standard library › New uuid package" {
		t.Errorf("title = %q", uuid.Title())
	}
	if uuid.URL != "https://go.dev/doc/go1.27#uuid" {
		t.Errorf("url = %q", uuid.URL)
	}
	timeChunk, ok := got["go1.27#time"]
	if !ok {
		t.Fatalf("no time chunk; got %v", keys(got))
	}
	if timeChunk.URL != "https://go.dev/doc/go1.27#minor" {
		t.Errorf("time chunk should link to nearest anchor, got %q", timeChunk.URL)
	}
	rt := got["go1.27#runtime"]
	if !strings.Contains(rt.Text, "not a heading") {
		t.Errorf("heading inside a code fence was treated as a heading: %q", rt.Text)
	}
}

func TestHTMLHeadings(t *testing.T) {
	doc := `<!--{"Title": "Effective Go"}--><h2 id="names">Names</h2><p>Names are as important in Go as in any other language, and they have semantic effect.</p>`
	cs := chunkDoc("effective_go", doc)
	if len(cs) != 1 || cs[0].ID != "effective_go#names" || cs[0].URL != "https://go.dev/doc/effective_go#names" {
		t.Fatalf("got %+v", cs)
	}
	if strings.Contains(cs[0].Text, "<p>") {
		t.Errorf("tags not stripped: %q", cs[0].Text)
	}
}

func TestSplitLong(t *testing.T) {
	long := strings.Repeat("word ", 700) // one 3500-char line, no paragraph breaks
	parts := splitLong(long, 1200)
	if len(parts) < 3 {
		t.Fatalf("expected at least 3 parts, got %d", len(parts))
	}
	for _, p := range parts {
		if len(p) > 1200 {
			t.Errorf("part too long: %d", len(p))
		}
	}
}

func TestTokensKeepIdentifiers(t *testing.T) {
	got := strings.Join(tokens("Use context.WithTimeout and asynctimerchan=1"), " ")
	for _, want := range []string{"context.withtimeout", "withtimeout", "asynctimerchan"} {
		if !strings.Contains(got, want) {
			t.Errorf("tokens %q missing %q", got, want)
		}
	}
}

func TestFuseRewardsBothLists(t *testing.T) {
	a := []Hit{{1, 0.9}, {2, 0.8}, {3, 0.7}}
	b := []Hit{{3, 12}, {4, 9}}
	got := fuse([][]Hit{a, b}, 2)
	if got[0].Chunk != 3 {
		t.Errorf("chunk in both lists should win, got %+v", got)
	}
}

// fakeOllama embeds text as a hashed bag of words (so shared words = closer
// vectors) and "answers" by echoing which sources it was given.
func fakeOllama(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			var req struct{ Input []string }
			json.NewDecoder(r.Body).Decode(&req)
			var out struct {
				Embeddings [][]float32 `json:"embeddings"`
			}
			for _, s := range req.Input {
				out.Embeddings = append(out.Embeddings, hashVec(s))
			}
			json.NewEncoder(w).Encode(out)
		case "/api/chat":
			var req struct {
				Messages []struct{ Role, Content string }
			}
			json.NewDecoder(r.Body).Decode(&req)
			user := req.Messages[1].Content
			first := strings.SplitN(strings.SplitN(user, "[1] ", 2)[1], " (", 2)[0]
			for _, piece := range []string{"Use ", "the source: ", first, " [1]"} {
				fmt.Fprintf(w, `{"message":{"content":%q},"done":false}`+"\n", piece)
			}
			fmt.Fprintln(w, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func hashVec(s string) []float32 {
	v := make([]float32, 256)
	for _, tok := range tokens(strings.TrimPrefix(strings.TrimPrefix(s, "search_query: "), "search_document: ")) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		v[h.Sum32()%256]++
	}
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(n) + 1e-9)
	}
	return v
}

func TestEndToEnd(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)

	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "go1.27.md"), []byte(sampleDoc), 0o644)
	os.WriteFile(filepath.Join(docs, "effective_go.html"), []byte(`<h2 id="names">Names</h2><p>Names are as important in Go as in any other language, and they have semantic effect.</p>`), 0o644)
	idx := filepath.Join(dir, "index.json")

	o := newOllama()
	if err := buildIndex(o, docs, idx); err != nil {
		t.Fatal(err)
	}
	ix, err := loadIndex(idx)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"keyword", "vector", "hybrid"} {
		hits, err := retrieve(o, ix, "how do I generate a UUID in Go?", mode, 3)
		if err != nil {
			t.Fatal(err)
		}
		// keyword search must put it first; the fake embedder is crude, so
		// for vector and hybrid we only require it in the top 3.
		if rank := rankOf(ix, hits, "go1.27#uuid"); rank == 0 || (mode == "keyword" && rank != 1) {
			t.Errorf("%s: go1.27#uuid at rank %d", mode, rank)
		}
	}
	hits, _ := retrieve(o, ix, "what does asynctimerchan do?", "keyword", 3)
	if len(hits) == 0 || ix.Chunks[hits[0].Chunk].ID != "go1.27#time" {
		t.Errorf("keyword search should find the exact word")
	}

	system, user := buildPrompt(ix, hits, "what does asynctimerchan do?")
	if !strings.HasSuffix(user, "Question: what does asynctimerchan do?") {
		t.Errorf("question should be last in the window")
	}
	if !strings.Contains(system, "ONLY the numbered sources") {
		t.Errorf("instructions missing")
	}
	var out bytes.Buffer
	if err := o.Chat(system, user, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[1]") {
		t.Errorf("streamed answer = %q", out.String())
	}

	// eval: the retrieval test passes for the expected chunk
	cases := `[{"question":"how do I generate a UUID in Go?","expect":"go1.27#uuid"}]`
	os.WriteFile(filepath.Join(dir, "evals.json"), []byte(cases), 0o644)
	if err := cmdEval([]string{"--file", filepath.Join(dir, "evals.json"), "--index", idx}); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]Chunk) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

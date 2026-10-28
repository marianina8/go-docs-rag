package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// A Hit is one search result: which chunk, and how well it matched.
type Hit struct {
	Chunk int     // index into Index.Chunks
	Score float64 // higher is better; scale depends on the mode
}

// --- meaning: vector search ---

// cosine is how close two embeddings point: 1 = same direction, 0 = unrelated.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// vectorSearch compares the question's vector with every chunk's vector.
// For a few thousand chunks, a plain loop is fast enough: no vector database needed.
func vectorSearch(ix *Index, q []float32, n int) []Hit {
	hits := make([]Hit, len(ix.Chunks))
	for i, c := range ix.Chunks {
		hits[i] = Hit{i, cosine(q, c.Vector)}
	}
	return top(hits, n)
}

// --- exact words: keyword search (BM25) ---

var reToken = regexp.MustCompile(`[a-z0-9_]+(?:[./][a-z0-9_]+)*`)

// stop words carry no meaning for keyword search.
var stop = map[string]bool{"a": true, "an": true, "the": true, "is": true, "are": true, "in": true,
	"of": true, "to": true, "do": true, "does": true, "i": true, "how": true, "what": true, "and": true,
	"or": true, "for": true, "on": true, "with": true, "it": true, "be": true, "as": true, "can": true}

// tokens lowercases text and keeps identifiers like "context.withtimeout" whole,
// plus their parts ("context", "withtimeout"), so both spellings match.
// It also folds simple plurals, so "UUIDs" matches "UUID".
func tokens(s string) []string {
	var out []string
	add := func(t string) {
		if stop[t] {
			return
		}
		if len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") {
			t = t[:len(t)-1]
		}
		out = append(out, t)
	}
	for _, t := range reToken.FindAllString(strings.ToLower(s), -1) {
		add(t)
		if strings.ContainsAny(t, "./") {
			for _, part := range strings.FieldsFunc(t, func(r rune) bool { return r == '.' || r == '/' }) {
				add(part)
			}
		}
	}
	return out
}

// BM25 is the classic keyword-ranking formula behind most search engines.
type BM25 struct {
	docs  []map[string]int // term counts per chunk
	lens  []int
	avg   float64
	df    map[string]int // how many chunks contain each term
	k1, b float64
}

func newBM25(ix *Index) *BM25 {
	bm := &BM25{df: map[string]int{}, k1: 1.2, b: 0.75}
	total := 0
	for _, c := range ix.Chunks {
		tf := map[string]int{}
		toks := tokens(c.Title() + " " + c.Text)
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			bm.df[t]++
		}
		bm.docs = append(bm.docs, tf)
		bm.lens = append(bm.lens, len(toks))
		total += len(toks)
	}
	if len(bm.docs) > 0 {
		bm.avg = float64(total) / float64(len(bm.docs))
	}
	return bm
}

func (bm *BM25) search(query string, n int) []Hit {
	N := float64(len(bm.docs))
	var hits []Hit
	q := tokens(query)
	for i, tf := range bm.docs {
		score := 0.0
		for _, t := range q {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (N-float64(bm.df[t])+0.5)/(float64(bm.df[t])+0.5))
			norm := bm.k1 * (1 - bm.b + bm.b*float64(bm.lens[i])/bm.avg)
			score += idf * f * (bm.k1 + 1) / (f + norm)
		}
		if score > 0 {
			hits = append(hits, Hit{i, score})
		}
	}
	return top(hits, n)
}

// --- both: hybrid search ---

// fuse merges ranked lists with Reciprocal Rank Fusion: a chunk scores
// 1/(60+rank) in each list it appears in. Ranks, not raw scores, so a
// cosine of 0.8 and a BM25 of 12.3 can be combined fairly.
func fuse(lists [][]Hit, n int) []Hit {
	const k = 60.0
	score := map[int]float64{}
	for _, list := range lists {
		for rank, h := range list {
			score[h.Chunk] += 1 / (k + float64(rank+1))
		}
	}
	hits := make([]Hit, 0, len(score))
	for c, s := range score {
		hits = append(hits, Hit{c, s})
	}
	return top(hits, n)
}

// top sorts hits best-first and keeps n.
func top(hits []Hit, n int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Chunk < hits[j].Chunk
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

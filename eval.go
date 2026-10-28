package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// An EvalCase is one real question plus the chunk that answers it.
type EvalCase struct {
	Question string `json:"question"`
	Expect   string `json:"expect"` // a chunk ID, or the start of one (e.g. "go1.27#uuid")
}

// cmdEval tests retrieval on its own: for each question, did the expected
// chunk come back in the top K? No model answers are involved.
func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	file := fs.String("file", "evals.json", "questions and the chunk that answers each")
	k := fs.Int("k", 3, "top K")
	idx := fs.String("index", "data/index.json", "index file")
	parseAnywhere(fs, args)

	b, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var cases []EvalCase
	if err := json.Unmarshal(b, &cases); err != nil {
		return err
	}
	o := newOllama()
	ix, err := loadIndex(*idx)
	if err != nil {
		return err
	}

	modes := []string{"keyword", "vector", "hybrid"}
	found := map[string]int{}
	fmt.Printf("%-52s %-8s %-8s %-8s\n", "question", modes[0], modes[1], modes[2])
	for _, c := range cases {
		row := fmt.Sprintf("%-52s", preview(c.Question, 50))
		for _, m := range modes {
			hits, err := retrieve(o, ix, c.Question, m, *k)
			if err != nil {
				return err
			}
			rank := rankOf(ix, hits, c.Expect)
			if rank > 0 {
				found[m]++
				row += fmt.Sprintf(" %-8s", fmt.Sprintf("✓ #%d", rank))
			} else {
				row += fmt.Sprintf(" %-8s", "✗")
			}
		}
		fmt.Println(row)
	}
	fmt.Printf("\nright chunk in top %d:", *k)
	for _, m := range modes {
		fmt.Printf("  %s %d/%d", m, found[m], len(cases))
	}
	fmt.Println()
	return nil
}

// rankOf returns the 1-based rank of the expected chunk, or 0 if it's missing.
func rankOf(ix *Index, hits []Hit, expect string) int {
	for i, h := range hits {
		if strings.HasPrefix(ix.Chunks[h.Chunk].ID, expect) {
			return i + 1
		}
	}
	return 0
}

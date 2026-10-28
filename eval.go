package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// An EvalCase is one real question plus the chunk (or chunks) that answer it.
type EvalCase struct {
	Question string  `json:"question"`
	Expect   Expects `json:"expect"` // chunk IDs or their starts, e.g. "go1.27#uuid"
}

// Expects accepts one ID ("go1.27#uuid") or a list (["a#x", "b#y"]):
// some questions are answered equally well by more than one chunk.
type Expects []string

func (e *Expects) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*e = Expects{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("expect must be a string or a list of strings: %w", err)
	}
	*e = many
	return nil
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
			rank := rankOf(ix, hits, c.Expect...)
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

// rankOf returns the 1-based rank of the first expected chunk, or 0 if none came back.
func rankOf(ix *Index, hits []Hit, expect ...string) int {
	for i, h := range hits {
		for _, e := range expect {
			if strings.HasPrefix(ix.Chunks[h.Chunk].ID, e) {
				return i + 1
			}
		}
	}
	return 0
}

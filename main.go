// go-docs-rag: a tiny retrieval-augmented generation (RAG) demo over the official Go docs.
//
//	go run . index                      # part one: chunk, embed, store
//	go run . ask "how do I generate a UUID in Go?"
//	go run . ask --mode vector "what does asynctimerchan do?"
//	go run . eval                       # test retrieval on its own
//
// Everything runs locally with Ollama. Standard library only.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const usage = `go-docs-rag: RAG over the official Go docs, running locally with Ollama.

Commands:
  index   download the docs, chunk them, embed them, save data/index.json
  ask     answer a question from the docs, with sources
  eval    check retrieval alone: does the right chunk come back?

Examples:
  go run . index
  go run . ask "how do I generate a UUID in Go?"
  go run . ask --mode vector --no-answer "what does asynctimerchan do?"
  go run . eval

Models (change with env vars): RAG_EMBED_MODEL=nomic-embed-text, RAG_CHAT_MODEL=llama3.2
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex(os.Args[2:])
	case "ask":
		err = cmdAsk(os.Args[2:])
	case "eval":
		err = cmdEval(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	docs := fs.String("docs", "data/docs", "folder of .md/.html docs (downloaded here if it's the default)")
	out := fs.String("out", "data/index.json", "where to save the index")
	parseAnywhere(fs, args)
	if *docs == "data/docs" {
		fmt.Println("0 fetch:  official Go docs (CC BY 4.0, The Go Authors)")
		if err := download(*docs); err != nil {
			return err
		}
	}
	return buildIndex(newOllama(), *docs, *out)
}

func cmdAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	mode := fs.String("mode", "hybrid", "vector, keyword or hybrid")
	k := fs.Int("k", 3, "how many chunks to put in the window (top K)")
	noAnswer := fs.Bool("no-answer", false, "only show what retrieval found")
	file := fs.String("index", "data/index.json", "index file")
	rest := parseAnywhere(fs, args)
	question := strings.TrimSpace(strings.Join(rest, " "))
	if question == "" {
		return fmt.Errorf(`ask needs a question, e.g.  go run . ask "how do I generate a UUID in Go?"`)
	}

	o := newOllama()
	ix, err := loadIndex(*file)
	if err != nil {
		return err
	}
	hits, err := retrieve(o, ix, question, *mode, *k)
	if err != nil {
		return err
	}

	fmt.Printf("\nquestion: %s\nmode: %s · top %d\n\n", question, *mode, *k)
	for i, h := range hits {
		c := ix.Chunks[h.Chunk]
		fmt.Printf("[%d] %s   (score %.3f)\n    %s\n    %s\n\n", i+1, c.Title(), h.Score, preview(c.Text, 110), c.URL)
	}
	if *noAnswer || len(hits) == 0 {
		return nil
	}
	fmt.Printf("answer (%s):\n", o.ChatModel)
	system, user := buildPrompt(ix, hits, question)
	return o.Chat(system, user, os.Stdout)
}

// buildPrompt puts the window together in order: instructions first,
// then the retrieved chunks, then the question last.
func buildPrompt(ix *Index, hits []Hit, question string) (system, user string) {
	system = "You answer questions about Go using ONLY the numbered sources provided. " +
		"Cite sources inline like [1] or [2]. " +
		"If the sources don't contain the answer, say \"The sources don't say.\" Don't guess. Be brief."
	var b strings.Builder
	b.WriteString("Sources:\n\n")
	for i, h := range hits {
		c := ix.Chunks[h.Chunk]
		fmt.Fprintf(&b, "[%d] %s (%s)\n%s\n\n", i+1, c.Title(), c.URL, c.Text)
	}
	fmt.Fprintf(&b, "Question: %s", question)
	return system, b.String()
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// parseAnywhere lets flags come before or after the question.
func parseAnywhere(fs *flag.FlagSet, args []string) []string {
	var rest []string
	for {
		_ = fs.Parse(args)
		args = fs.Args()
		if len(args) == 0 {
			return rest
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

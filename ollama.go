package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Ollama talks to a local Ollama server: one model for embeddings, one for answers.
type Ollama struct {
	Host       string // e.g. http://localhost:11434
	EmbedModel string // e.g. nomic-embed-text
	ChatModel  string // e.g. llama3.2
	http       *http.Client
}

func newOllama() *Ollama {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	if !strings.HasPrefix(host, "http") {
		host = "http://" + host
	}
	return &Ollama{
		Host:       strings.TrimRight(host, "/"),
		EmbedModel: envOr("RAG_EMBED_MODEL", "nomic-embed-text"),
		ChatModel:  envOr("RAG_CHAT_MODEL", "llama3.2"),
		http:       &http.Client{Timeout: 5 * time.Minute},
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Embed turns texts into vectors. isQuery matters for models like
// nomic-embed-text, which expect a different prefix for questions vs documents.
func (o *Ollama) Embed(texts []string, isQuery bool) ([][]float32, error) {
	in := make([]string, len(texts))
	for i, t := range texts {
		in[i] = o.prefix(isQuery) + t
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	err := o.post("/api/embed", map[string]any{"model": o.EmbedModel, "input": in}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: asked for %d vectors, got %d", len(texts), len(out.Embeddings))
	}
	return out.Embeddings, nil
}

func (o *Ollama) prefix(isQuery bool) string {
	if !strings.Contains(o.EmbedModel, "nomic") {
		return ""
	}
	if isQuery {
		return "search_query: "
	}
	return "search_document: "
}

// Chat sends one system + user message and streams the answer to w as it's generated.
func (o *Ollama) Chat(system, user string, w io.Writer) error {
	body, _ := json.Marshal(map[string]any{
		"model":  o.ChatModel,
		"stream": true,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"options": map[string]any{"temperature": 0},
	})
	resp, err := o.http.Post(o.Host+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return o.hint(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	// Ollama streams one JSON object per line, each with the next bit of text.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var part struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done  bool   `json:"done"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &part); err != nil {
			return fmt.Errorf("chat: bad stream line: %w", err)
		}
		if part.Error != "" {
			return fmt.Errorf("chat: %s", part.Error)
		}
		fmt.Fprint(w, part.Message.Content)
		if part.Done {
			break
		}
	}
	fmt.Fprintln(w)
	return sc.Err()
}

func (o *Ollama) post(path string, req, out any) error {
	body, _ := json.Marshal(req)
	resp, err := o.http.Post(o.Host+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return o.hint(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (o *Ollama) hint(err error) error {
	return fmt.Errorf("%w\n\nIs Ollama running? Start the app (or `ollama serve`), then:\n  ollama pull %s\n  ollama pull %s", err, o.EmbedModel, o.ChatModel)
}

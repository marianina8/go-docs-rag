package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// rawBase is where the official go.dev pages live as source files
// (the golang/website repository). Go docs are CC BY 4.0, (c) The Go Authors.
const rawBase = "https://raw.githubusercontent.com/golang/website/master/_content/doc/"

// sources is the small, hand-picked corpus this demo indexes.
// Release notes (what changed, when), the big guides, modules and tutorials.
var sources = []string{
	"go1.21.md", "go1.22.md", "go1.23.md", "go1.24.md",
	"go1.25.md", "go1.26.md", "go1.27.md",
	"faq.md", "effective_go.html", "toolchain.md",
	"gc-guide.html", "diagnostics.html", "comment.md", "pgo.md",
	"jsonv2-migration.md",
	"modules/gomod-ref.md", "modules/managing-dependencies.md",
	"modules/layout.md", "modules/version-numbers.md",
	"modules/major-version.md", "modules/publishing.md",
	"tutorial/generics.md", "tutorial/fuzz.md", "tutorial/database-access.md",
	"tutorial/workspaces.md", "tutorial/web-service-gin.md", "tutorial/govulncheck.md",
}

// download fetches every source file into dir, skipping files already there.
func download(dir string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	for _, name := range sources {
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if _, err := os.Stat(dst); err == nil {
			continue // cached
		}
		fmt.Printf("  downloading %s\n", name)
		resp, err := client.Get(rawBase + name)
		if err != nil {
			return fmt.Errorf("download %s: %w", name, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download %s: %s", name, resp.Status)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// loadDocs reads every .md and .html file under dir.
// It returns doc name (like "go1.27" or "modules/layout") -> raw text.
func loadDocs(dir string) (map[string]string, error) {
	docs := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(path)
		if ext != ".md" && ext != ".html" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		name := strings.TrimSuffix(filepath.ToSlash(rel), ext)
		docs[name] = string(b)
		return nil
	})
	return docs, err
}

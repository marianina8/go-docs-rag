package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// A Chunk is one small piece of one doc: the unit we embed, search and cite.
type Chunk struct {
	ID     string    `json:"id"`     // e.g. "go1.27#uuid"
	Doc    string    `json:"doc"`    // e.g. "go1.27"
	Path   []string  `json:"path"`   // e.g. ["Go 1.27 Release Notes", "Standard library", "New uuid package"]
	URL    string    `json:"url"`    // where a reader can check it
	Text   string    `json:"text"`   // the section's text
	Vector []float32 `json:"vector"` // its embedding (filled in by index)
}

// Title is the breadcrumb we show and cite: "Go 1.27 Release Notes › New uuid package".
func (c Chunk) Title() string { return strings.Join(c.Path, " › ") }

// EmbedText is what we actually embed: the breadcrumb plus the text, so a
// chunk that only says "The new package generates and parses UUIDs" still
// carries which release and section it came from.
func (c Chunk) EmbedText() string { return "Source: " + c.Title() + "\n\n" + c.Text }

const maxChunkChars = 1200

var (
	reHeading   = regexp.MustCompile(`^(#{1,4})\s+(.+?)(?:\s*\{#([^}]+)\})?\s*$`)
	reLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reComment   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reStyle     = regexp.MustCompile(`(?is)<(style|script)[^>]*>.*?</(style|script)>`)
	reHTMLHead  = regexp.MustCompile(`(?is)<h([1-4])([^>]*)>(.*?)</h[1-4]>`)
	reID        = regexp.MustCompile(`id="([^"]*)"`)
	reTag       = regexp.MustCompile(`<[^>]+>`)
	reFrontKV   = regexp.MustCompile(`(?m)^title:\s*(.+)$`)
	reHTMLTitle = regexp.MustCompile(`"Title":\s*"([^"]+)"`)
	reBlank     = regexp.MustCompile(`\n{3,}`)
)

// chunkDoc turns one doc into chunks, split at its headings.
func chunkDoc(name, raw string) []Chunk {
	title, body := normalize(name, raw)

	var chunks []Chunk
	path := []string{title} // breadcrumb: title, then h2, h3, h4
	levels := []int{1}      // heading level of each path entry
	anchor := ""            // nearest explicit anchor, for the URL
	anchors := []string{""} // anchor of each path entry
	var buf []string        // lines of the current section
	inFence := false
	seen := map[string]int{} // chunk IDs used so far, to keep them unique

	flush := func() {
		text := strings.TrimSpace(reBlank.ReplaceAllString(strings.Join(buf, "\n"), "\n\n"))
		buf = buf[:0]
		if len(strings.Fields(text)) < 8 { // headings with almost no text
			return
		}
		for i, part := range splitLong(text, maxChunkChars) {
			c := Chunk{
				Doc:  name,
				Path: append([]string(nil), path...),
				URL:  docURL(name, anchor),
				Text: part,
			}
			c.ID = chunkID(name, path, anchors[len(anchors)-1], i)
			if seen[c.ID]++; seen[c.ID] > 1 {
				c.ID = fmt.Sprintf("%s~%d", c.ID, seen[c.ID])
			}
			chunks = append(chunks, c)
		}
	}

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		m := reHeading.FindStringSubmatch(line)
		if m == nil || inFence {
			buf = append(buf, line)
			continue
		}
		level := len(m[1])
		if level == 1 { // the page title; we already have it
			continue
		}
		flush()
		for len(levels) > 1 && levels[len(levels)-1] >= level {
			path, levels, anchors = path[:len(path)-1], levels[:len(levels)-1], anchors[:len(anchors)-1]
		}
		path = append(path, cleanInline(m[2]))
		levels = append(levels, level)
		anchors = append(anchors, m[3])
		anchor = ""
		for _, a := range anchors { // deepest explicit anchor wins
			if a != "" {
				anchor = a
			}
		}
	}
	flush()
	return chunks
}

// normalize strips front matter, comments, styles and HTML tags, and turns
// HTML headings into markdown headings so one splitter handles both.
func normalize(name, raw string) (title, body string) {
	title = name
	if m := reFrontKV.FindStringSubmatch(raw); m != nil {
		title = strings.Trim(strings.TrimSpace(m[1]), `"`)
	} else if m := reHTMLTitle.FindStringSubmatch(raw); m != nil {
		title = m[1]
	}
	if strings.HasPrefix(raw, "---") { // markdown front matter
		if end := strings.Index(raw[3:], "\n---"); end >= 0 {
			raw = raw[3+end+4:]
		}
	}
	raw = reComment.ReplaceAllString(raw, "")
	raw = reStyle.ReplaceAllString(raw, "")
	raw = reHTMLHead.ReplaceAllStringFunc(raw, func(h string) string {
		m := reHTMLHead.FindStringSubmatch(h)
		heading := "\n" + strings.Repeat("#", int(m[1][0]-'0')) + " " + cleanInline(reTag.ReplaceAllString(m[3], ""))
		if id := reID.FindStringSubmatch(m[2]); id != nil {
			heading += " {#" + id[1] + "}"
		}
		return heading + "\n"
	})
	raw = reTag.ReplaceAllString(raw, "")
	raw = html.UnescapeString(raw)
	raw = reLink.ReplaceAllString(raw, "$1")
	return title, raw
}

// cleanInline removes markdown links and backticks from a heading.
func cleanInline(s string) string {
	s = reLink.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(html.UnescapeString(s))
}

// splitLong cuts a long section into pieces of at most max chars,
// preferring paragraph breaks, then line breaks, then a hard cut.
func splitLong(text string, max int) []string {
	if len(text) <= max {
		return []string{text}
	}
	var pieces []string
	for _, para := range strings.Split(text, "\n\n") {
		for _, line := range strings.Split(para, "\n") {
			for len(line) > max {
				cut := strings.LastIndex(line[:max], " ")
				if cut <= 0 {
					cut = max
				}
				pieces = append(pieces, line[:cut])
				line = line[cut:]
			}
			pieces = append(pieces, line)
		}
		pieces = append(pieces, "") // remember the paragraph break
	}
	var parts []string
	var cur strings.Builder
	for _, p := range pieces {
		if cur.Len() > 0 && cur.Len()+len(p)+1 > max {
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		cur.WriteString(p)
		cur.WriteString("\n")
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

func docURL(name, anchor string) string {
	u := "https://go.dev/doc/" + name
	if anchor != "" {
		u += "#" + anchor
	}
	return u
}

var reSlug = regexp.MustCompile(`[^a-z0-9]+`)

// chunkID is "doc#section", using the heading's own anchor when it has one.
func chunkID(name string, path []string, ownAnchor string, part int) string {
	key := ownAnchor
	if key == "" {
		key = strings.Trim(reSlug.ReplaceAllString(strings.ToLower(path[len(path)-1]), "-"), "-")
	}
	if part > 0 {
		key += fmt.Sprintf("-%d", part+1)
	}
	return name + "#" + key
}

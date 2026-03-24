package aisdk

import (
	"context"
	"fmt"
	"html"
	"io"
	"iter"
	"regexp"
	"slices"
	"strings"
)

// Chunk is a piece of text produced by a chunker.
type Chunk struct {
	Content  string
	Metadata ChunkMetadata
}

// ChunkMetadata holds information about a chunk's origin and strategy.
type ChunkMetadata struct {
	Strategy string // "characters", "separators", or "context"
	Index    int
	Chars    int    // rune count
	Section  string // heading section (separators/context only)
	Source   string // source identifier (separators/context only)
	Context  string // LLM-generated context (context only)
}

// defaultSeparators is the hierarchy used by separator-based chunkers, from coarse to fine.
var defaultSeparators = []string{"\n## ", "\n### ", "\n\n", "\n", ". ", " "} //nolint:gochecknoglobals

// ChunkByChars splits text into fixed-size chunks by rune count with overlap.
// Panics if size <= 0 or overlap >= size.
func ChunkByChars(r io.Reader, size, overlap int) iter.Seq2[Chunk, error] {
	assertChunkParams(size, overlap)
	return func(yield func(Chunk, error) bool) {
		text, err := readAll(r)
		if err != nil {
			yield(Chunk{}, err)
			return
		}

		runes := []rune(text)
		idx := 0
		for start := 0; start < len(runes); start += size - overlap {
			end := min(start+size, len(runes))
			content := string(runes[start:end])
			if !yield(Chunk{
				Content: content,
				Metadata: ChunkMetadata{
					Strategy: "characters",
					Index:    idx,
					Chars:    end - start,
				},
			}, nil) {
				return
			}
			idx++
		}
	}
}

// ChunkBySeparators splits text using a hierarchy of separators with heading-based
// section detection.
// Panics if size <= 0 or overlap >= size.
func ChunkBySeparators(r io.Reader, size, overlap int, source string) iter.Seq2[Chunk, error] {
	assertChunkParams(size, overlap)
	return func(yield func(Chunk, error) bool) {
		text, err := readAll(r)
		if err != nil {
			yield(Chunk{}, err)
			return
		}

		raw := splitBySeparators(text, size, overlap, defaultSeparators)
		headings := buildHeadingIndex(text)

		for i, content := range raw {
			if !yield(Chunk{
				Content: content,
				Metadata: ChunkMetadata{
					Strategy: "separators",
					Index:    i,
					Chars:    len([]rune(content)),
					Section:  findSection(text, content, headings),
					Source:   source,
				},
			}, nil) {
				return
			}
		}
	}
}

// ChunkByContext splits text using separators and enriches each chunk with
// LLM-generated context via the given ResponsesClient.
// The chunk content is XML-escaped before sending to the LLM to mitigate prompt injection.
// Panics if size <= 0 or overlap >= size.
func ChunkByContext(
	ctx context.Context,
	r io.Reader,
	size, overlap int,
	source string,
	client *ResponsesClient,
) iter.Seq2[Chunk, error] {
	assertChunkParams(size, overlap)
	return func(yield func(Chunk, error) bool) {
		text, err := readAll(r)
		if err != nil {
			yield(Chunk{}, err)
			return
		}

		raw := splitBySeparators(text, size, overlap, defaultSeparators)
		headings := buildHeadingIndex(text)

		for i, content := range raw {
			chunkCtx, err := enrichChunk(ctx, client, content)
			if err != nil {
				yield(Chunk{}, fmt.Errorf("enrich chunk %d: %w", i, err))
				return
			}
			if !yield(Chunk{
				Content: content,
				Metadata: ChunkMetadata{
					Strategy: "context",
					Index:    i,
					Chars:    len([]rune(content)),
					Section:  findSection(text, content, headings),
					Source:   source,
					Context:  chunkCtx,
				},
			}, nil) {
				return
			}
		}
	}
}

func assertChunkParams(size, overlap int) {
	if size <= 0 {
		panic("chunk: size must be > 0")
	}
	if overlap >= size {
		panic("chunk: overlap must be < size")
	}
}

func readAll(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return string(b), nil
}

func enrichChunk(ctx context.Context, client *ResponsesClient, content string) (string, error) {
	input := []M{{
		"role": "user",
		"content": fmt.Sprintf(
			"<chunk>%s</chunk>\nGenerate a very short (1-2 sentence) context that situates this chunk within the overall document. Return ONLY the context, nothing else.",
			html.EscapeString(content),
		),
	}}

	res, err := client.Do(ctx, input, nil)
	if err != nil {
		return "", err
	}

	text := res.text()
	if text == "" {
		return "", fmt.Errorf("no text in context response")
	}
	return text, nil
}

// Separator splitting internals.

func splitBySeparators(text string, size, overlap int, separators []string) []string {
	if len(text) <= size {
		return []string{text}
	}

	sep := ""
	for _, s := range separators {
		if strings.Contains(text, s) {
			sep = s
			break
		}
	}
	if sep == "" {
		return []string{text}
	}

	parts := strings.Split(text, sep)
	var chunks []string
	current := ""

	for _, part := range parts {
		candidate := current
		if candidate != "" {
			candidate += sep + part
		} else {
			candidate = part
		}

		if len(candidate) > size && current != "" {
			chunks = append(chunks, current)
			overlapText := pickOverlap(current, overlap, sep)
			if overlapText != "" {
				current = overlapText + sep + part
			} else {
				current = part
			}
		} else {
			current = candidate
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}

	// Recursively split chunks that are still too large.
	remaining := separators[slices.Index(separators, sep)+1:]
	if len(remaining) == 0 {
		return chunks
	}

	var result []string
	for _, chunk := range chunks {
		if len(chunk) > size {
			result = append(result, splitBySeparators(chunk, size, overlap, remaining)...)
		} else {
			result = append(result, chunk)
		}
	}
	return result
}

func pickOverlap(text string, overlap int, sep string) string {
	if overlap <= 0 {
		return ""
	}

	start := max(len(text)-overlap, 0)
	tail := text[start:]

	idx := strings.IndexByte(tail, '\n')
	if idx == -1 {
		idx = strings.IndexFunc(tail, func(r rune) bool {
			return r == ' ' || r == '\t'
		})
	}
	if idx == -1 {
		return ""
	}

	result := text[start+idx+1:]
	if sep != "" && strings.HasPrefix(result, sep) {
		result = result[len(sep):]
	}
	return result
}

// Heading index internals.

type heading struct {
	position int
	level    int
	title    string
}

var (
	mdHeadingRegex    = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	plainHeadingRegex = regexp.MustCompile(`(?m)(?:^|\n\n)([^\n]{1,80})\n[A-Za-z"'\[(]`)
)

func buildHeadingIndex(text string) []heading {
	var headings []heading

	// Markdown # headings.
	for _, m := range mdHeadingRegex.FindAllStringSubmatchIndex(text, -1) {
		headings = append(headings, heading{
			position: m[0],
			level:    m[3] - m[2], // length of # group
			title:    strings.TrimSpace(text[m[4]:m[5]]),
		})
	}

	// Plain-text headings: short line after blank line, followed by content.
	mdTitles := make(map[string]bool, len(headings))
	for _, h := range headings {
		mdTitles[h.title] = true
	}

	for _, m := range plainHeadingRegex.FindAllStringSubmatchIndex(text, -1) {
		title := strings.TrimSpace(text[m[2]:m[3]])
		if title == "" || title == "Conclusion:" || mdTitles[title] {
			continue
		}
		pos := m[0]
		if text[pos] == '\n' {
			pos += 2
		}
		headings = append(headings, heading{
			position: pos,
			level:    1,
			title:    title,
		})
	}

	slices.SortFunc(headings, func(a, b heading) int {
		return a.position - b.position
	})
	return headings
}

func findSection(text, chunkContent string, headings []heading) string {
	if len(headings) == 0 {
		return ""
	}

	// Sample from mid-chunk to avoid overlap-related false matches.
	mid := len(chunkContent) * 4 / 10
	end := min(mid+100, len(chunkContent))
	sample := chunkContent[mid:end]

	pos := strings.Index(text, sample)
	if pos == -1 {
		return ""
	}

	var current *heading
	for i := range headings {
		if headings[i].position <= pos {
			current = &headings[i]
		} else {
			break
		}
	}

	if current == nil {
		return ""
	}
	return strings.Repeat("#", current.level) + " " + current.title
}

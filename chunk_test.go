package aisdk

import (
	"strings"
	"testing"
)

func collect(seq func(func(Chunk, error) bool)) ([]Chunk, error) {
	var chunks []Chunk
	var retErr error
	seq(func(c Chunk, err error) bool {
		if err != nil {
			retErr = err
			return false
		}
		chunks = append(chunks, c)
		return true
	})
	return chunks, retErr
}

func TestChunkByChars_Basic(t *testing.T) {
	text := "abcdefghij"
	chunks, err := collect(ChunkByChars(strings.NewReader(text), 4, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	want := []string{"abcd", "efgh", "ij"}
	for i, c := range chunks {
		if c.Content != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, c.Content, want[i])
		}
		if c.Metadata.Strategy != "characters" {
			t.Errorf("chunk %d: strategy = %q, want %q", i, c.Metadata.Strategy, "characters")
		}
		if c.Metadata.Index != i {
			t.Errorf("chunk %d: index = %d, want %d", i, c.Metadata.Index, i)
		}
	}
}

func TestChunkByChars_Overlap(t *testing.T) {
	text := "abcdefghij"
	chunks, err := collect(ChunkByChars(strings.NewReader(text), 4, 2))
	if err != nil {
		t.Fatal(err)
	}
	// size=4, overlap=2, step=2: starts at 0,2,4,6,8
	want := []string{"abcd", "cdef", "efgh", "ghij", "ij"}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d", len(chunks), len(want))
	}
	for i, c := range chunks {
		if c.Content != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, c.Content, want[i])
		}
	}
}

func TestChunkByChars_Multibyte(t *testing.T) {
	// 5 runes, each 3 bytes in UTF-8
	text := "αβγδε"
	chunks, err := collect(ChunkByChars(strings.NewReader(text), 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "αβγ" {
		t.Errorf("chunk 0: got %q, want %q", chunks[0].Content, "αβγ")
	}
	if chunks[0].Metadata.Chars != 3 {
		t.Errorf("chunk 0: chars = %d, want 3 (runes)", chunks[0].Metadata.Chars)
	}
	if chunks[1].Content != "δε" {
		t.Errorf("chunk 1: got %q, want %q", chunks[1].Content, "δε")
	}
}

func TestChunkByChars_SmallText(t *testing.T) {
	text := "hi"
	chunks, err := collect(ChunkByChars(strings.NewReader(text), 100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].Content != "hi" {
		t.Errorf("got %q, want %q", chunks[0].Content, "hi")
	}
}

func TestChunkByChars_Empty(t *testing.T) {
	chunks, err := collect(ChunkByChars(strings.NewReader(""), 10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("got %d chunks, want 0", len(chunks))
	}
}

func TestChunkByChars_PanicOnBadParams(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		overlap int
	}{
		{"zero size", 0, 0},
		{"negative size", -1, 0},
		{"overlap equals size", 5, 5},
		{"overlap exceeds size", 5, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic")
				}
			}()
			ChunkByChars(strings.NewReader("test"), tt.size, tt.overlap)
		})
	}
}

func TestChunkBySeparators_Paragraphs(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	chunks, err := collect(ChunkBySeparators(strings.NewReader(text), 30, 0, "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for _, c := range chunks {
		if c.Metadata.Strategy != "separators" {
			t.Errorf("strategy = %q, want %q", c.Metadata.Strategy, "separators")
		}
		if c.Metadata.Source != "test.md" {
			t.Errorf("source = %q, want %q", c.Metadata.Source, "test.md")
		}
	}
}

func TestChunkBySeparators_FitsInOneChunk(t *testing.T) {
	text := "Short text."
	chunks, err := collect(ChunkBySeparators(strings.NewReader(text), 1000, 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].Content != text {
		t.Errorf("got %q, want %q", chunks[0].Content, text)
	}
}

func TestChunkBySeparators_HeadingSections(t *testing.T) {
	text := "## Introduction\n\nSome intro text here that is long enough to be its own chunk.\n\n## Methods\n\nSome methods text here that is also fairly long."
	chunks, err := collect(ChunkBySeparators(strings.NewReader(text), 60, 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}
	// First chunk should be under Introduction section
	foundIntro := false
	foundMethods := false
	for _, c := range chunks {
		if c.Metadata.Section == "## Introduction" {
			foundIntro = true
		}
		if c.Metadata.Section == "## Methods" {
			foundMethods = true
		}
	}
	if !foundIntro {
		t.Error("expected a chunk in the Introduction section")
	}
	if !foundMethods {
		t.Error("expected a chunk in the Methods section")
	}
}

func TestChunkBySeparators_CharsIsRuneCount(t *testing.T) {
	// Text with multibyte characters
	text := "αβγ\n\nδεζ"
	chunks, err := collect(ChunkBySeparators(strings.NewReader(text), 5, 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		runeCount := len([]rune(c.Content))
		if c.Metadata.Chars != runeCount {
			t.Errorf("Chars = %d, but rune count = %d for %q", c.Metadata.Chars, runeCount, c.Content)
		}
	}
}

func TestChunkBySeparators_PanicOnBadParams(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	ChunkBySeparators(strings.NewReader("test"), 5, 5, "")
}

func TestChunkByChars_EarlyBreak(t *testing.T) {
	text := "abcdefghij"
	var first Chunk
	ChunkByChars(strings.NewReader(text), 3, 0)(func(c Chunk, err error) bool {
		first = c
		return false // stop after first
	})
	if first.Content != "abc" {
		t.Errorf("got %q, want %q", first.Content, "abc")
	}
}

// Tests for internal helpers.

func TestBuildHeadingIndex(t *testing.T) {
	text := "## First\n\nContent.\n\n### Second\n\nMore content."
	headings := buildHeadingIndex(text)
	if len(headings) != 2 {
		t.Fatalf("got %d headings, want 2", len(headings))
	}
	if headings[0].title != "First" || headings[0].level != 2 {
		t.Errorf("heading 0: got %+v", headings[0])
	}
	if headings[1].title != "Second" || headings[1].level != 3 {
		t.Errorf("heading 1: got %+v", headings[1])
	}
	if headings[0].position >= headings[1].position {
		t.Error("headings not sorted by position")
	}
}

func TestFindSection(t *testing.T) {
	text := "## Intro\n\nThe introduction covers the background of this research project in detail.\n\n## Body\n\nThe body section discusses methodology and experimental results thoroughly."
	headings := buildHeadingIndex(text)

	section := findSection(text, "The introduction covers the background of this research project in detail.", headings)
	if section != "## Intro" {
		t.Errorf("got %q, want %q", section, "## Intro")
	}

	section = findSection(text, "The body section discusses methodology and experimental results thoroughly.", headings)
	if section != "## Body" {
		t.Errorf("got %q, want %q", section, "## Body")
	}
}

func TestFindSection_NoHeadings(t *testing.T) {
	section := findSection("plain text", "plain text", nil)
	if section != "" {
		t.Errorf("got %q, want empty", section)
	}
}

func TestSplitBySeparators_Recursive(t *testing.T) {
	// Text that needs paragraph split, then sentence split
	text := "First sentence. Second sentence. Third sentence.\n\nAnother paragraph here."
	chunks := splitBySeparators(text, 30, 0, defaultSeparators)
	for _, c := range chunks {
		if len(c) > 30 {
			t.Errorf("chunk too large (%d > 30): %q", len(c), c)
		}
	}
	// Reconstruct should cover all content
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "First") || !strings.Contains(joined, "Another") {
		t.Error("content lost during splitting")
	}
}

func TestPickOverlap_Basic(t *testing.T) {
	text := "first line\nsecond line\nthird line"
	result := pickOverlap(text, 15, "\n")
	if result == "" {
		t.Fatal("expected non-empty overlap")
	}
	// Should find a newline boundary within last 15 chars
	if strings.Contains(result, "\n\n") {
		t.Error("overlap should not contain the separator")
	}
}

func TestPickOverlap_ZeroOverlap(t *testing.T) {
	result := pickOverlap("some text", 0, " ")
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestPickOverlap_NoBreakPoint(t *testing.T) {
	result := pickOverlap("abcdefghij", 5, " ")
	if result != "" {
		t.Errorf("got %q, want empty (no whitespace/newline)", result)
	}
}

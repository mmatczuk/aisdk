package aisdk

import "math"

// charsPerToken is a conservative estimate (~4 chars/token for English).
// Using 3.5 so estimates run slightly high, giving a safety margin.
const charsPerToken = 3.5

// EstimateTokens returns a rough token count for text using a character-based heuristic.
func EstimateTokens(text string) int {
	return int(math.Ceil(float64(len(text)) / charsPerToken))
}

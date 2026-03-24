package aisdk

import (
	"context"
	"fmt"
	"math"
)

// EmbeddingConfig holds configuration for an EmbeddingClient.
type EmbeddingConfig struct {
	Model string // model identifier (e.g. "openai/text-embedding-3-small")
}

// EmbeddingClient creates text embeddings via the OpenRouter Embeddings API.
type EmbeddingClient struct {
	api *APIClient
	cfg EmbeddingConfig
}

// NewEmbeddingClient creates an EmbeddingClient.
func NewEmbeddingClient(api *APIClient, cfg EmbeddingConfig) *EmbeddingClient {
	return &EmbeddingClient{
		api: api,
		cfg: cfg,
	}
}

// Embed returns the embedding vector for the given text.
func (c *EmbeddingClient) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody := embeddingRequest{
		Model: c.cfg.Model,
		Input: text,
	}

	res, status, err := doJSONRequest[embeddingResponse](ctx, c.api.doer, c.api.cfg, reqBody)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(status, res.Error, "embedding API error"); err != nil {
		return nil, err
	}

	if len(res.Data) == 0 {
		return nil, fmt.Errorf("embedding: no data in response")
	}
	return res.Data[0].Embedding, nil
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value between -1 and 1 (typically 0 to 1 for embeddings).
// Panics if the vectors have different lengths.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		panic("CosineSimilarity: vectors must have equal length")
	}

	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// Embedding API request/response types.

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Error *apiError       `json:"error,omitempty"`
}

type embeddingData struct {
	Embedding []float64 `json:"embedding"`
}

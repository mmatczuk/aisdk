package aisdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPDoer abstracts HTTP request execution.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPDoer returns an HTTPDoer backed by http.DefaultClient.
func DefaultHTTPDoer() HTTPDoer {
	return http.DefaultClient
}

// ProviderConfig holds API provider configuration.
type ProviderConfig struct {
	APIKey   string
	Endpoint string
}

// Well-known OpenRouter endpoints.
const (
	OpenRouterResponsesEndpoint       = "https://openrouter.ai/api/v1/responses"
	OpenRouterChatCompletionsEndpoint = "https://openrouter.ai/api/v1/chat/completions"
	OpenRouterEmbeddingsEndpoint      = "https://openrouter.ai/api/v1/embeddings"
)

// doJSONRequest marshals reqBody as JSON, POSTs it to cfg.Endpoint with Bearer auth,
// and unmarshals the response into Res. Returns the parsed response and HTTP status code.
func doJSONRequest[Res any](ctx context.Context, doer HTTPDoer, cfg ProviderConfig, reqBody any) (*Res, int, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	res, err := doer.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}

	r := new(Res)
	if err := json.Unmarshal(resBody, r); err != nil {
		return nil, 0, fmt.Errorf("unmarshal response (status %d): %w", res.StatusCode, err)
	}
	return r, res.StatusCode, nil
}

func checkAPIError(statusCode int, apiErr *apiError, prefix string) error {
	if statusCode == http.StatusOK {
		return nil
	}
	msg := fmt.Sprintf("status %d", statusCode)
	if apiErr != nil {
		msg = apiErr.Message
	}
	return fmt.Errorf("%s: %s", prefix, msg)
}

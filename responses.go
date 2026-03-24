package aisdk

import (
	"context"
	"encoding/json"
)

// M is a shorthand for unstructured JSON-like maps.
type M = map[string]any

// ResponsesConfig holds configuration for a ResponsesClient.
type ResponsesConfig struct {
	Model           string // model identifier
	MaxOutputTokens int    // max tokens in model response (0 for API default)
}

// ResponsesClient performs single requests against the Responses API.
type ResponsesClient struct {
	api *APIClient
	cfg ResponsesConfig
}

// NewResponsesClient creates a ResponsesClient.
func NewResponsesClient(api *APIClient, cfg ResponsesConfig) *ResponsesClient {
	return &ResponsesClient{api: api, cfg: cfg}
}

// Do sends input (and optional tool definitions) to the Responses API and
// returns the parsed response.
func (r *ResponsesClient) Do(ctx context.Context, input []M, tools json.RawMessage) (*responsesResponse, error) {
	reqBody := responsesRequest{
		Model:           r.cfg.Model,
		Input:           input,
		Tools:           tools,
		MaxOutputTokens: r.cfg.MaxOutputTokens,
	}

	res, status, err := doJSONRequest[responsesResponse](ctx, r.api.doer, r.api.cfg, reqBody)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(status, res.Error, "responses API error"); err != nil {
		return nil, err
	}
	return res, nil
}

// Responses API request/response types.

type responsesRequest struct {
	Model           string          `json:"model"`
	Input           []M             `json:"input"`
	Tools           json.RawMessage `json:"tools,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
}

type responsesResponse struct {
	Output     []responsesOutputItem `json:"output"`
	OutputText string                `json:"output_text"`
	Usage      Usage                 `json:"usage,omitempty"`
	Error      *apiError             `json:"error,omitempty"`
}

type apiError struct {
	Message string `json:"message"`
}

type responsesOutputItem struct {
	Type      string                 `json:"type"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Content   []responsesContentItem `json:"content,omitempty"`
}

type responsesContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Internal helpers.

func filterToolCalls(output []responsesOutputItem) []responsesOutputItem {
	var calls []responsesOutputItem
	for _, item := range output {
		if item.Type == "function_call" {
			calls = append(calls, item)
		}
	}
	return calls
}

func (r *responsesResponse) text() string {
	if r.OutputText != "" {
		return r.OutputText
	}
	for _, item := range r.Output {
		if item.Type == "message" {
			for _, c := range item.Content {
				if c.Type == "text" || c.Type == "output_text" {
					return c.Text
				}
			}
		}
	}
	return ""
}

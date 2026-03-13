package aisdk

import (
	"context"
	"encoding/json"
	"fmt"
)

// M is a shorthand for unstructured JSON-like maps.
type M = map[string]any

// API request/response types

type apiRequest struct {
	Model           string          `json:"model"`
	Input           []M             `json:"input"`
	Tools           json.RawMessage `json:"tools,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
}

type apiResponse struct {
	Output     []apiOutputItem `json:"output"`
	OutputText string          `json:"output_text"`
	Usage      Usage           `json:"usage,omitempty"`
	Error      *apiError       `json:"error,omitempty"`
}

type apiError struct {
	Message string `json:"message"`
}

type apiOutputItem struct {
	Type      string           `json:"type"`
	Name      string           `json:"name,omitempty"`
	Arguments string           `json:"arguments,omitempty"`
	CallID    string           `json:"call_id,omitempty"`
	Content   []apiContentItem `json:"content,omitempty"`
}

type apiContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Internal helpers

func (c *Chat) callAPI(ctx context.Context) (*apiResponse, error) {
	reqBody := apiRequest{
		Model:           c.cfg.Model,
		Input:           c.history,
		MaxOutputTokens: c.cfg.MaxOutputTokens,
	}
	if c.tools != nil {
		toolDefs, err := c.tools.marshalDefs()
		if err != nil {
			return nil, fmt.Errorf("marshal tool definitions: %w", err)
		}
		reqBody.Tools = toolDefs
	}

	res, status, err := doJSONRequest[apiResponse](ctx, c.doer, c.cfg.ProviderConfig, reqBody)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(status, res.Error, "API error"); err != nil {
		return nil, err
	}

	c.usage.add(&res.Usage)
	return res, nil
}

func filterToolCalls(output []apiOutputItem) []apiOutputItem {
	var calls []apiOutputItem
	for _, item := range output {
		if item.Type == "function_call" {
			calls = append(calls, item)
		}
	}
	return calls
}

func (r *apiResponse) text() string {
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

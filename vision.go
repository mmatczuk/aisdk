package aisdk

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
)

// VisionConfig holds configuration for a VisionClient.
type VisionConfig struct {
	ProviderConfig
	Model           string // model identifier (e.g. "openai/gpt-4o")
	MaxOutputTokens int    // max tokens in model response (0 for API default)
}

// VisionClient queries a vision model with images via the Responses API.
type VisionClient struct {
	cfg   VisionConfig
	doer  HTTPDoer
	usage Usage
}

// NewVisionClient creates a VisionClient.
func NewVisionClient(cfg VisionConfig, doer HTTPDoer) *VisionClient {
	return &VisionClient{
		cfg:  cfg,
		doer: doer,
	}
}

// QueryImage sends an image and question to the vision model, returning the text response.
func (v *VisionClient) QueryImage(ctx context.Context, imageData []byte, mimeType, question string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

	reqBody := apiRequest{
		Model: v.cfg.Model,
		Input: []M{{
			"role": "user",
			"content": []M{
				{"type": "input_text", "text": question},
				{"type": "input_image", "image_url": dataURL},
			},
		}},
		MaxOutputTokens: v.cfg.MaxOutputTokens,
	}

	res, status, err := doJSONRequest[apiResponse](ctx, v.doer, v.cfg.ProviderConfig, reqBody)
	if err != nil {
		return "", err
	}
	if err := checkAPIError(status, res.Error, "vision API error"); err != nil {
		return "", err
	}

	v.usage.add(&res.Usage)

	text := res.text()
	if text == "" {
		return "", fmt.Errorf("vision: no text in response")
	}

	slog.Info("vision response", "text", text)
	return text, nil
}

// Usage returns the cumulative token usage for this client.
func (v *VisionClient) Usage() Usage {
	return v.usage
}

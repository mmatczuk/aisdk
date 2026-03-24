package aisdk

import (
	"context"
	"encoding/base64"
	"fmt"
)

// VisionClient queries a vision model with images via the Responses API.
type VisionClient struct {
	client   *ResponsesClient
	listener *VisionListener
	usage    Usage
}

// NewVisionClient creates a VisionClient.
func NewVisionClient(client *ResponsesClient, listener *VisionListener) *VisionClient {
	return &VisionClient{
		client:   client,
		listener: listener,
	}
}

// QueryImage sends an image and question to the vision model, returning the text response.
func (v *VisionClient) QueryImage(ctx context.Context, imageData []byte, mimeType, question string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

	input := []M{{
		"role": "user",
		"content": []M{
			{"type": "input_text", "text": question},
			{"type": "input_image", "image_url": dataURL},
		},
	}}

	res, err := v.client.Do(ctx, input, nil)
	if err != nil {
		return "", err
	}

	v.usage.add(&res.Usage)

	text := res.text()
	if text == "" {
		return "", fmt.Errorf("vision: no text in response")
	}

	v.listener.response(VisionResponseEvent{Text: text, Tokens: res.Usage})
	return text, nil
}

// Usage returns the cumulative token usage for this client.
func (v *VisionClient) Usage() Usage {
	return v.usage
}

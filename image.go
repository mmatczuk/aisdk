package aisdk

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ImageRef is a reference image for editing operations.
type ImageRef struct {
	Data     []byte // raw image bytes
	MimeType string // e.g. "image/png"
}

// ImageOptions configures image generation parameters.
type ImageOptions struct {
	AspectRatio string // e.g. "1:1", "16:9"
	ImageSize   string // e.g. "1k", "2k", "4k"
}

// ImageGenConfig holds configuration for an ImageGenClient.
type ImageGenConfig struct {
	Model     string // model identifier (e.g. "google/gemini-2.0-flash-exp:free")
	OutputDir string // directory where generated images are saved
}

// ImageGenClient generates and edits images via OpenRouter's Gemini image model.
type ImageGenClient struct {
	api      *APIClient
	cfg      ImageGenConfig
	listener *ImageListener
}

// NewImageClient creates an ImageGenClient.
func NewImageClient(api *APIClient, cfg ImageGenConfig, listener *ImageListener) *ImageGenClient {
	return &ImageGenClient{
		api:      api,
		cfg:      cfg,
		listener: listener,
	}
}

// Generate creates an image from a text prompt.
// Returns the relative file path within outputDir.
func (c *ImageGenClient) Generate(ctx context.Context, prompt string, opts ImageOptions) (string, error) {
	return c.requestImage(ctx, prompt, nil, opts, "Generating image")
}

// Edit modifies a single image according to instructions.
// Returns the relative file path within outputDir.
func (c *ImageGenClient) Edit(ctx context.Context, instructions string, imageData []byte, mimeType string, opts ImageOptions) (string, error) {
	return c.requestImage(ctx, instructions, []ImageRef{{Data: imageData, MimeType: mimeType}}, opts, "Editing image")
}

// EditWithReferences modifies images using multiple reference images.
// Returns the relative file path within outputDir.
func (c *ImageGenClient) EditWithReferences(ctx context.Context, instructions string, refs []ImageRef, opts ImageOptions) (string, error) {
	return c.requestImage(ctx, instructions, refs, opts, "Editing with references")
}

func (c *ImageGenClient) requestImage(
	ctx context.Context,
	prompt string,
	refs []ImageRef,
	opts ImageOptions,
	action string,
) (string, error) {
	c.listener.started(ImageStartedEvent{
		Action: action,
		Prompt: prompt,
		Refs:   len(refs),
	})

	data, err := c.callOpenRouter(ctx, prompt, refs, opts)
	if err != nil {
		return "", err
	}

	imgData, mimeType, err := extractImageFromResponse(data, action)
	if err != nil {
		return "", err
	}

	filename, err := c.saveImage(imgData, mimeType)
	if err != nil {
		return "", err
	}

	c.listener.completed(ImageCompletedEvent{
		Action:   action,
		Filename: filename,
		MimeType: mimeType,
	})
	return filename, nil
}

// OpenRouter request/response types

type orImageRequest struct {
	Model       string         `json:"model"`
	Messages    []orMessage    `json:"messages"`
	Modalities  []string       `json:"modalities"`
	ImageConfig *orImageConfig `json:"image_config,omitempty"`
}

type orMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []orContentPart
}

type orContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *orImageURL `json:"image_url,omitempty"`
}

type orImageURL struct {
	URL string `json:"url"`
}

type orImageConfig struct {
	AspectRatio string `json:"aspect_ratio,omitempty"`
	ImageSize   string `json:"image_size,omitempty"`
}

type orResponse struct {
	Choices []orChoice `json:"choices"`
	Error   *apiError  `json:"error,omitempty"`
}

type orChoice struct {
	Message orChoiceMessage `json:"message"`
}

type orChoiceMessage struct {
	Content any       `json:"content"` // string or []orContentPart
	Images  []orImage `json:"images"`
}

type orImage struct {
	ImageURL *orImageURLField `json:"image_url,omitempty"`
}

type orImageURLField struct {
	URL string `json:"url"`
}

func (c *ImageGenClient) callOpenRouter(ctx context.Context, prompt string, refs []ImageRef, opts ImageOptions) (*orResponse, error) {
	var content any
	if len(refs) == 0 {
		content = prompt
	} else {
		parts := []orContentPart{{Type: "text", Text: prompt}}
		for _, ref := range refs {
			b64 := base64.StdEncoding.EncodeToString(ref.Data)
			parts = append(parts, orContentPart{
				Type: "image_url",
				ImageURL: &orImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", ref.MimeType, b64),
				},
			})
		}
		content = parts
	}

	reqBody := orImageRequest{
		Model:      c.cfg.Model,
		Messages:   []orMessage{{Role: "user", Content: content}},
		Modalities: []string{"image", "text"},
	}

	if opts.AspectRatio != "" || opts.ImageSize != "" {
		reqBody.ImageConfig = &orImageConfig{
			AspectRatio: opts.AspectRatio,
			ImageSize:   normalizeImageSize(opts.ImageSize),
		}
	}

	res, status, err := doJSONRequest[orResponse](ctx, c.api.doer, c.api.cfg, reqBody)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(status, res.Error, "image API error"); err != nil {
		return nil, err
	}

	return res, nil
}

var dataURLRegex = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

func extractImageFromResponse(res *orResponse, action string) ([]byte, string, error) {
	if len(res.Choices) == 0 {
		return nil, "", fmt.Errorf("no choices in image response")
	}

	msg := res.Choices[0].Message

	// Check images array
	for _, img := range msg.Images {
		if img.ImageURL == nil {
			continue
		}
		matches := dataURLRegex.FindStringSubmatch(img.ImageURL.URL)
		if matches == nil {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(matches[2])
		if err != nil {
			return nil, "", fmt.Errorf("decode image base64: %w", err)
		}
		return decoded, matches[1], nil
	}

	// No image found — extract text for error message
	textMsg := extractTextFromContent(msg.Content)
	if textMsg != "" {
		return nil, "", fmt.Errorf("%s failed: %s", action, textMsg)
	}
	return nil, "", fmt.Errorf("no image output received from OpenRouter")
}

func extractTextFromContent(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

func (c *ImageGenClient) saveImage(data []byte, mimeType string) (string, error) {
	ext := extFromMIME(mimeType)
	id := shortID()
	filename := fmt.Sprintf("%d_%s%s", time.Now().Unix(), id, ext)

	if err := os.MkdirAll(c.cfg.OutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	fullPath := filepath.Join(c.cfg.OutputDir, filename)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}

	return filename, nil
}

func normalizeImageSize(size string) string {
	if strings.HasSuffix(size, "k") {
		return size[:len(size)-1] + "K"
	}
	return size
}

func extFromMIME(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func shortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}

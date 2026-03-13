package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmatczuk/aisdk"
)

type understandImageArgs struct {
	ImagePath string `json:"image_path" jsonschema:"path to the image file (absolute or relative to working directory)"`
	Query     string `json:"query" jsonschema:"what you want to know about the image"`
}

// RegisterUnderstandImageTool registers an understand_image tool that analyzes
// images using the provided VisionClient.
func RegisterUnderstandImageTool(t *aisdk.Tools, vc *aisdk.VisionClient) {
	aisdk.RegisterTool(t, "understand_image",
		"Analyze an image and answer questions about it. Use this to identify people, objects, scenes, or any visual content in images.",
		func(ctx context.Context, args understandImageArgs) any {
			data, err := os.ReadFile(args.ImagePath)
			if err != nil {
				return aisdk.M{"error": fmt.Sprintf("read image: %v", err), "image_path": args.ImagePath}
			}

			answer, err := vc.QueryImage(ctx, data, mimeType(args.ImagePath), args.Query)
			if err != nil {
				return aisdk.M{"error": fmt.Sprintf("vision: %v", err), "image_path": args.ImagePath}
			}

			return aisdk.M{"answer": answer, "image_path": args.ImagePath}
		},
	)
}

var mimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func mimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}
	return "image/jpeg"
}

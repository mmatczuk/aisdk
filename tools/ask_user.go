package tools

import (
	"context"

	"github.com/mmatczuk/aisdk"
)

type askUserArgs struct {
	Question string `json:"question" jsonschema:"the question to ask the user"`
}

// RegisterAskUserTool registers an ask_user tool that calls fn with the
// question and returns the user's answer.
func RegisterAskUserTool(t *aisdk.Tools, fn func(question string) string) {
	aisdk.RegisterTool(t, "ask_user",
		"Ask the user a question and wait for their response. "+
			"Use this when you need clarification, confirmation, or additional "+
			"information that only the user can provide.",
		func(_ context.Context, args askUserArgs) any {
			if args.Question == "" {
				return aisdk.M{"error": "question is required"}
			}
			return aisdk.M{"answer": fn(args.Question)}
		},
	)
}

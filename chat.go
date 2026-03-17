package aisdk

import (
	"context"
	"fmt"
)

// ChatResult is returned by UserMessage after the model produces a final text
// response (or the turn limit is reached).
type ChatResult struct {
	Text        string
	TurnsUsed   int
	ToolsCalled []string
}

// ChatConfig holds configuration for a Chat.
type ChatConfig struct {
	ProviderConfig
	Model           string // model identifier (e.g. "gpt-5-mini")
	System          string // system prompt
	MaxTurns        int    // max API round-trips per UserMessage call
	MaxOutputTokens int    // max tokens in model response (0 for API default)
}

// Chat manages a persistent conversation with the Responses API,
// including automatic tool dispatch.
type Chat struct {
	cfg      ChatConfig
	tools    *Tools
	doer     HTTPDoer
	listener *ChatListener

	history []M
	usage   Usage
}

// NewChat creates a Chat.
func NewChat(cfg ChatConfig, tools *Tools, doer HTTPDoer, listener *ChatListener) *Chat {
	c := &Chat{
		cfg:      cfg,
		tools:    tools,
		doer:     doer,
		listener: listener,
	}

	if cfg.System != "" {
		c.history = append(c.history, M{
			"role":    "system",
			"content": cfg.System,
		})
	}

	return c
}

// UserMessage sends a user message and runs the tool-calling loop until the
// model produces a text response or maxTurns is exhausted.
func (c *Chat) UserMessage(ctx context.Context, text string) (ChatResult, error) {
	c.listener.agentStarted(AgentStartedEvent{Message: text})

	c.history = append(c.history, M{
		"role":    "user",
		"content": text,
	})

	var res ChatResult
	for turn := 0; turn < c.cfg.MaxTurns; turn++ {
		c.listener.turnStarted(TurnStartedEvent{Turn: turn + 1})

		apiRes, err := c.callAPI(ctx)
		if err != nil {
			err = fmt.Errorf("turn %d: %w", turn+1, err)
			c.listener.agentFailed(AgentFailedEvent{Err: err, Turn: turn + 1})
			return res, err
		}

		toolCalls := filterToolCalls(apiRes.Output)
		res.TurnsUsed = turn + 1

		if len(toolCalls) == 0 {
			res.Text = apiRes.text()
			c.listener.turnCompleted(TurnCompletedEvent{
				Turn:    turn + 1,
				Final:   true,
				TextLen: len(res.Text),
			})
			c.listener.agentCompleted(AgentCompletedEvent{
				Text:      res.Text,
				TurnsUsed: res.TurnsUsed,
			})
			// Append assistant response to history for multi-turn context
			c.history = append(c.history, M{
				"role":    "assistant",
				"content": res.Text,
			})
			return res, nil
		}

		names := make([]string, len(toolCalls))
		for i, tc := range toolCalls {
			names[i] = tc.Name
			res.ToolsCalled = append(res.ToolsCalled, tc.Name)
		}
		c.listener.turnCompleted(TurnCompletedEvent{
			Turn:      turn + 1,
			ToolCalls: names,
		})

		// Append function_call items to history
		for _, tc := range toolCalls {
			c.history = append(c.history, M{
				"type":      "function_call",
				"name":      tc.Name,
				"arguments": tc.Arguments,
				"call_id":   tc.CallID,
			})
		}

		// Execute tools concurrently, append results
		c.history = append(c.history, c.tools.execute(ctx, toolCalls)...)
	}

	err := fmt.Errorf("tool calling did not finish within %d turns", c.cfg.MaxTurns)
	c.listener.agentFailed(AgentFailedEvent{Err: err, Turn: c.cfg.MaxTurns})
	return res, err
}

// Usage tracks token usage across API calls.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// Requests is a client-side counter incremented per API call; not part of the API response.
	Requests int `json:"-"`
}

func (u *Usage) add(other *Usage) {
	if other == nil {
		return
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.Requests++
}

// Usage returns the cumulative token usage for this chat.
func (c *Chat) Usage() Usage {
	return c.usage
}

// History returns the raw conversation history for inspection.
func (c *Chat) History() []M {
	return c.history
}

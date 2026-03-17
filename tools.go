package aisdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

type toolDef struct {
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Parameters  *jsonschema.Schema `json:"parameters"`
	Strict      bool               `json:"strict"`
}

// Tools holds tool definitions and their handler functions.
type Tools struct {
	native   []toolDef
	mcp      []json.RawMessage // raw JSON tool definitions from MCP servers
	handlers map[string]func(ctx context.Context, args json.RawMessage) any
	listener *ToolListener
}

// NewTools creates an empty tool registry.
func NewTools(listener *ToolListener) *Tools {
	return &Tools{
		handlers: make(map[string]func(ctx context.Context, args json.RawMessage) any),
		listener: listener,
	}
}

// RegisterTool adds a tool with a typed handler. The parameter schema is
// inferred from T using jsonschema.For[T], so T should be a struct with
// json and jsonschema tags.
// Handlers must not return Go errors; return an error object instead
// (e.g. M{"error": "something went wrong"}).
func RegisterTool[T any](
	t *Tools,
	name,
	description string,
	handler func(ctx context.Context, args T) any,
) {
	params, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("RegisterTool %s: schema inference failed: %v", name, err))
	}
	t.native = append(t.native, toolDef{
		Type:        "function",
		Name:        name,
		Description: description,
		Parameters:  params,
		Strict:      true,
	})
	t.handlers[name] = func(ctx context.Context, raw json.RawMessage) any {
		var args T
		if err := json.Unmarshal(raw, &args); err != nil {
			return M{"error": fmt.Sprintf("failed to parse arguments: %v", err)}
		}
		return handler(ctx, args)
	}
}

// marshalDefs returns all tool definitions (native + MCP) as a single JSON array.
// Returns (nil, nil) if there are no tools.
func (t *Tools) marshalDefs() (json.RawMessage, error) {
	if len(t.native) == 0 && len(t.mcp) == 0 {
		return nil, nil
	}

	var all []json.RawMessage
	for _, d := range t.native {
		raw, err := json.Marshal(d)
		if err != nil {
			return nil, fmt.Errorf("marshal tool %s: %w", d.Name, err)
		}
		all = append(all, raw)
	}
	all = append(all, t.mcp...)

	out, err := json.Marshal(all)
	if err != nil {
		return nil, fmt.Errorf("marshal tool definitions: %w", err)
	}
	return out, nil
}

// execute runs all tool calls concurrently and returns results in the same order.
func (t *Tools) execute(ctx context.Context, calls []apiOutputItem) []M {
	results := make([]M, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(i int, call apiOutputItem) {
			defer wg.Done()

			handler, ok := t.handlers[call.Name]
			if !ok {
				t.listener.toolCalled(ToolCalledEvent{Name: call.Name, Args: call.Arguments})
				t.listener.toolCompleted(ToolCompletedEvent{
					Name: call.Name,
					Err:  fmt.Sprintf("unknown tool: %s", call.Name),
				})
				results[i] = M{
					"type":    "function_call_output",
					"call_id": call.CallID,
					"output":  fmt.Sprintf(`{"error":"unknown tool: %s"}`, call.Name),
				}
				return
			}

			t.listener.toolCalled(ToolCalledEvent{Name: call.Name, Args: call.Arguments})

			start := time.Now()
			result := handler(ctx, json.RawMessage(call.Arguments))
			duration := time.Since(start)

			output, _ := json.Marshal(result)
			t.listener.toolCompleted(ToolCompletedEvent{
				Name:     call.Name,
				Result:   string(output),
				Duration: duration,
			})

			results[i] = M{
				"type":    "function_call_output",
				"call_id": call.CallID,
				"output":  string(output),
			}
		}(i, call)
	}

	wg.Wait()
	return results
}

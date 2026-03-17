package aisdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// MCPConfig represents the mcp.json configuration file.
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPServerConfig describes how to spawn an MCP server.
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// LoadMCPConfig reads and parses an mcp.json file.
func LoadMCPConfig(path string) (*MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mcp config: %w", err)
	}
	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp config: %w", err)
	}
	return &cfg, nil
}

// JSON-RPC 2.0 types

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// syncWriteCloser wraps an io.WriteCloser with a mutex for concurrent writes.
type syncWriteCloser struct {
	mu sync.Mutex
	wc io.WriteCloser
}

func (s *syncWriteCloser) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wc.Write(p)
}

func (s *syncWriteCloser) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wc.Close()
}

// MCPClient manages a connection to an MCP server over stdio.
// When the context passed to NewMCPClient is canceled, the server process is killed.
type MCPClient struct {
	cmd      *exec.Cmd
	stdin    *syncWriteCloser
	nextID   atomic.Int64
	listener *MCPListener

	pendingMu sync.Mutex
	pending   map[int64]chan *jsonRPCResponse

	done    chan struct{} // closed when readLoop exits
	readErr error
}

// ConnectMCP loads config from the given path, spawns the named MCP server,
// and performs the initialize handshake. The context controls the server
// process lifetime.
func ConnectMCP(ctx context.Context, configPath, serverName, cwd string, listener *MCPListener) (*MCPClient, error) {
	cfg, err := LoadMCPConfig(configPath)
	if err != nil {
		return nil, err
	}

	serverCfg, ok := cfg.MCPServers[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found in config", serverName)
	}

	return NewMCPClient(ctx, serverCfg, cwd, listener)
}

// NewMCPClient spawns an MCP server process and performs the initialize handshake.
// The context controls the server process lifetime: canceling it kills the process.
func NewMCPClient(ctx context.Context, cfg MCPServerConfig, cwd string, listener *MCPListener) (*MCPClient, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = cwd
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), envMapToSlice(cfg.Env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start MCP server: %w", err)
	}

	c := &MCPClient{
		cmd:      cmd,
		stdin:    &syncWriteCloser{wc: stdin},
		listener: listener,
		pending:  make(map[int64]chan *jsonRPCResponse),
		done:     make(chan struct{}),
	}

	c.listener.spawned(MCPSpawnedEvent{
		Command: cfg.Command,
		Args:    cfg.Args,
		Pid:     cmd.Process.Pid,
	})

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	go c.readLoop(scanner)

	// Initialize handshake
	_, err = c.call(ctx, "initialize", M{
		"protocolVersion": "2024-11-05",
		"capabilities":    M{},
		"clientInfo": M{
			"name":    "aisdk-go",
			"version": "1.0.0",
		},
	})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("MCP initialize: %w", err)
	}

	// Send initialized notification (no response expected)
	if err := c.notify("notifications/initialized", nil); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("MCP initialized notification: %w", err)
	}

	c.listener.connected(MCPConnectedEvent{})
	return c, nil
}

// readLoop reads JSON-RPC responses from stdout and dispatches them
// to pending request channels.
func (c *MCPClient) readLoop(scanner *bufio.Scanner) {
	defer close(c.done)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var res jsonRPCResponse
		if err := json.Unmarshal(line, &res); err != nil {
			// Skip lines that aren't valid JSON-RPC (e.g. log output)
			continue
		}

		if res.ID == 0 {
			// Notification or unparseable; skip
			continue
		}

		c.pendingMu.Lock()
		ch, ok := c.pending[res.ID]
		if ok {
			delete(c.pending, res.ID)
		}
		c.pendingMu.Unlock()

		if ok {
			resCopy := res
			ch <- &resCopy
		}
	}
	c.readErr = scanner.Err()
}

// call sends a JSON-RPC request and waits for the response.
// Multiple calls can execute concurrently; responses are matched by ID.
func (c *MCPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	ch := make(chan *jsonRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if _, err = c.stdin.Write(append(data, '\n')); err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", res.Error.Code, res.Error.Message)
		}
		return res.Result, nil
	case <-c.done:
		return nil, fmt.Errorf("MCP server closed: %v", c.readErr)
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	}
}

func (c *MCPClient) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

// notify sends a JSON-RPC notification (no response expected).
func (c *MCPClient) notify(method string, params any) error {
	msg := M{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

// MCP tool types from tools/list response

type mcpToolListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools returns the tools available on the MCP server.
func (c *MCPClient) ListTools(ctx context.Context) ([]mcpTool, error) {
	result, err := c.call(ctx, "tools/list", M{})
	if err != nil {
		return nil, err
	}

	var list mcpToolListResult
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}

	return list.Tools, nil
}

// MCP tool call types

type mcpCallToolResult struct {
	Content []mcpContent `json:"content"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallTool invokes a tool on the MCP server and returns the text result.
func (c *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	// Parse args from string to object if needed
	var argsObj any
	if err := json.Unmarshal(args, &argsObj); err != nil {
		return "", fmt.Errorf("parse tool args: %w", err)
	}

	result, err := c.call(ctx, "tools/call", M{
		"name":      name,
		"arguments": argsObj,
	})
	if err != nil {
		return "", err
	}

	var callResult mcpCallToolResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return "", fmt.Errorf("parse tools/call result: %w", err)
	}

	// Return first text content
	for _, content := range callResult.Content {
		if content.Type == "text" {
			return content.Text, nil
		}
	}

	return "", nil
}

// RegisterTools discovers all tools from the MCP server and registers them
// into the given Tools registry. MCP tool calls are dispatched to the server.
func (c *MCPClient) RegisterTools(ctx context.Context, t *Tools) error {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list MCP tools: %w", err)
	}

	for _, tool := range tools {
		// Build OpenAI-compatible tool definition as raw JSON
		def := M{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  json.RawMessage(tool.InputSchema),
			"strict":      false,
		}
		raw, err := json.Marshal(def)
		if err != nil {
			return fmt.Errorf("marshal MCP tool %s: %w", tool.Name, err)
		}
		t.mcp = append(t.mcp, raw)

		// Register handler that dispatches to the MCP server
		toolName := tool.Name
		t.handlers[toolName] = func(ctx context.Context, args json.RawMessage) any {
			result, err := c.CallTool(ctx, toolName, args)
			if err != nil {
				return M{"error": err.Error()}
			}
			// Try to parse as JSON; return raw string if not valid JSON
			var parsed any
			if json.Unmarshal([]byte(result), &parsed) == nil {
				return parsed
			}
			return result
		}

		c.listener.toolRegistered(MCPToolRegisteredEvent{Name: tool.Name})
	}

	c.listener.allToolsRegistered(MCPAllToolsRegisteredEvent{Count: len(tools)})
	return nil
}

// Close terminates the MCP server process. It closes stdin, waits up to
// 5 seconds for the process to exit, then kills it.
func (c *MCPClient) Close() error {
	_ = c.stdin.Close()

	select {
	case <-c.done:
		// Reader goroutine exited, process should be done
	case <-time.After(5 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.done
	}

	return c.cmd.Wait()
}

func envMapToSlice(env map[string]string) []string {
	s := make([]string, 0, len(env))
	for k, v := range env {
		s = append(s, k+"="+v)
	}
	return s
}

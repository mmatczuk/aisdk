package aisdk

import (
	"fmt"
	"log/slog"
	"time"
)

// ---------------------------------------------------------------------------
// Chat events
// ---------------------------------------------------------------------------

type AgentStartedEvent struct {
	Message string // user message that started the agent
}

type TurnStartedEvent struct {
	Turn int // 1-based turn number
}

type TurnCompletedEvent struct {
	Turn      int
	ToolCalls []string // tool names called this turn (empty if final text)
	Final     bool     // true if this turn produced final text
	TextLen   int      // length of final text (0 if not final)
}

type AgentCompletedEvent struct {
	Text      string
	TurnsUsed int
}

type AgentFailedEvent struct {
	Err  error
	Turn int
}

// ChatListener receives events from the Chat agent loop.
type ChatListener struct {
	OnAgentStarted   func(AgentStartedEvent)
	OnTurnStarted    func(TurnStartedEvent)
	OnTurnCompleted  func(TurnCompletedEvent)
	OnAgentCompleted func(AgentCompletedEvent)
	OnAgentFailed    func(AgentFailedEvent)
}

// NewLogChatListener returns a ChatListener that logs events using slog.
func NewLogChatListener() *ChatListener {
	return &ChatListener{
		OnAgentStarted: func(e AgentStartedEvent) {
			slog.Info("agent started", "message", truncate(e.Message, 100))
		},
		OnTurnStarted: func(e TurnStartedEvent) {
			slog.Info("turn started", "turn", e.Turn)
		},
		OnTurnCompleted: func(e TurnCompletedEvent) {
			if e.Final {
				slog.Info("final text", "turn", e.Turn, "chars", e.TextLen)
			} else {
				slog.Info("tool calls", "turn", e.Turn, "count", len(e.ToolCalls), "names", e.ToolCalls)
			}
		},
		OnAgentCompleted: func(e AgentCompletedEvent) {
			slog.Info("agent completed", "turns", e.TurnsUsed)
		},
		OnAgentFailed: func(e AgentFailedEvent) {
			slog.Error("agent failed", "turn", e.Turn, "error", e.Err)
		},
	}
}

// ---------------------------------------------------------------------------
// Tool events
// ---------------------------------------------------------------------------

type ToolCalledEvent struct {
	Name string
	Args string // raw JSON arguments
}

type ToolCompletedEvent struct {
	Name     string
	Result   string // raw JSON result
	Duration time.Duration
	Err      string // non-empty if the tool was unknown or failed
}

// ToolListener receives events from tool execution.
type ToolListener struct {
	OnToolCalled    func(ToolCalledEvent)
	OnToolCompleted func(ToolCompletedEvent)
}

// NewLogToolListener returns a ToolListener that logs events using slog.
func NewLogToolListener() *ToolListener {
	return &ToolListener{
		OnToolCalled: func(e ToolCalledEvent) {
			slog.Info("tool call", "name", e.Name, "args", e.Args)
		},
		OnToolCompleted: func(e ToolCompletedEvent) {
			if e.Err != "" {
				slog.Warn("tool error", "name", e.Name, "error", e.Err, "duration", e.Duration)
			} else {
				slog.Info("tool result", "name", e.Name, "result", e.Result, "duration", e.Duration)
			}
		},
	}
}

// ---------------------------------------------------------------------------
// MCP events
// ---------------------------------------------------------------------------

type MCPSpawnedEvent struct {
	Command string
	Args    []string
	Pid     int
}

type MCPConnectedEvent struct{}

type MCPToolRegisteredEvent struct {
	Name string
}

type MCPAllToolsRegisteredEvent struct {
	Count int
}

// MCPListener receives events from the MCP client lifecycle.
type MCPListener struct {
	OnSpawned            func(MCPSpawnedEvent)
	OnConnected          func(MCPConnectedEvent)
	OnToolRegistered     func(MCPToolRegisteredEvent)
	OnAllToolsRegistered func(MCPAllToolsRegisteredEvent)
}

// NewLogMCPListener returns an MCPListener that logs events using slog.
func NewLogMCPListener() *MCPListener {
	return &MCPListener{
		OnSpawned: func(e MCPSpawnedEvent) {
			slog.Info("spawned MCP server", "command", e.Command, "args", e.Args, "pid", e.Pid)
		},
		OnConnected: func(e MCPConnectedEvent) {
			slog.Info("MCP connected and initialized")
		},
		OnToolRegistered: func(e MCPToolRegisteredEvent) {
			slog.Info("registered MCP tool", "name", e.Name)
		},
		OnAllToolsRegistered: func(e MCPAllToolsRegisteredEvent) {
			slog.Info("registered MCP tools", "count", e.Count)
		},
	}
}

// ---------------------------------------------------------------------------
// Vision events
// ---------------------------------------------------------------------------

type VisionResponseEvent struct {
	Text   string
	Tokens Usage
}

// VisionListener receives events from the VisionClient.
type VisionListener struct {
	OnResponse func(VisionResponseEvent)
}

// NewLogVisionListener returns a VisionListener that logs events using slog.
func NewLogVisionListener() *VisionListener {
	return &VisionListener{
		OnResponse: func(e VisionResponseEvent) {
			slog.Info("vision response", "text", e.Text)
		},
	}
}

// ---------------------------------------------------------------------------
// Image events
// ---------------------------------------------------------------------------

type ImageStartedEvent struct {
	Action string
	Prompt string
	Refs   int // number of reference images
}

type ImageCompletedEvent struct {
	Action   string
	Filename string
	MimeType string
}

// ImageListener receives events from the ImageGenClient.
type ImageListener struct {
	OnStarted   func(ImageStartedEvent)
	OnCompleted func(ImageCompletedEvent)
}

// NewLogImageListener returns an ImageListener that logs events using slog.
func NewLogImageListener() *ImageListener {
	return &ImageListener{
		OnStarted: func(e ImageStartedEvent) {
			preview := truncate(e.Prompt, 100)
			if e.Refs > 0 {
				preview = fmt.Sprintf("%d ref image(s)", e.Refs)
			}
			slog.Info("image request", "action", e.Action, "preview", preview)
		},
		OnCompleted: func(e ImageCompletedEvent) {
			slog.Info("image done", "action", e.Action, "filename", e.Filename, "mimeType", e.MimeType)
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Nil-safe emit helpers for each listener type.

func (l *ChatListener) agentStarted(e AgentStartedEvent) {
	if l != nil && l.OnAgentStarted != nil {
		l.OnAgentStarted(e)
	}
}

func (l *ChatListener) turnStarted(e TurnStartedEvent) {
	if l != nil && l.OnTurnStarted != nil {
		l.OnTurnStarted(e)
	}
}

func (l *ChatListener) turnCompleted(e TurnCompletedEvent) {
	if l != nil && l.OnTurnCompleted != nil {
		l.OnTurnCompleted(e)
	}
}

func (l *ChatListener) agentCompleted(e AgentCompletedEvent) {
	if l != nil && l.OnAgentCompleted != nil {
		l.OnAgentCompleted(e)
	}
}

func (l *ChatListener) agentFailed(e AgentFailedEvent) {
	if l != nil && l.OnAgentFailed != nil {
		l.OnAgentFailed(e)
	}
}

func (l *ToolListener) toolCalled(e ToolCalledEvent) {
	if l != nil && l.OnToolCalled != nil {
		l.OnToolCalled(e)
	}
}

func (l *ToolListener) toolCompleted(e ToolCompletedEvent) {
	if l != nil && l.OnToolCompleted != nil {
		l.OnToolCompleted(e)
	}
}

func (l *MCPListener) spawned(e MCPSpawnedEvent) {
	if l != nil && l.OnSpawned != nil {
		l.OnSpawned(e)
	}
}

func (l *MCPListener) connected(e MCPConnectedEvent) {
	if l != nil && l.OnConnected != nil {
		l.OnConnected(e)
	}
}

func (l *MCPListener) toolRegistered(e MCPToolRegisteredEvent) {
	if l != nil && l.OnToolRegistered != nil {
		l.OnToolRegistered(e)
	}
}

func (l *MCPListener) allToolsRegistered(e MCPAllToolsRegisteredEvent) {
	if l != nil && l.OnAllToolsRegistered != nil {
		l.OnAllToolsRegistered(e)
	}
}

func (l *VisionListener) response(e VisionResponseEvent) {
	if l != nil && l.OnResponse != nil {
		l.OnResponse(e)
	}
}

func (l *ImageListener) started(e ImageStartedEvent) {
	if l != nil && l.OnStarted != nil {
		l.OnStarted(e)
	}
}

func (l *ImageListener) completed(e ImageCompletedEvent) {
	if l != nil && l.OnCompleted != nil {
		l.OnCompleted(e)
	}
}

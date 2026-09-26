package model

import (
	"encoding/json"
	"time"
)

// Version is the protocol and product version announced in hello.
const Version = "2.0.0"

// Core → UI. Each message is one stdout line in production and one SSE
// event, named after its type, in development.

type Hello struct {
	Type     string   `json:"type"`
	Version  string   `json:"version"`
	Platform string   `json:"platform"`
	Config   any      `json:"config"`
	Widgets  []string `json:"widgets"`
}

type SnapshotMessage struct {
	Type   string   `json:"type"`
	Widget Snapshot `json:"widget"`
}

type ToastMessage struct {
	Type  string `json:"type"`
	Toast Toast  `json:"toast"`
}

type Toast struct {
	Widget string    `json:"widget"`
	Tone   Tone      `json:"tone"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

// Tick replaces the snapshot after a poll that changed nothing: every poll
// is heard, so a widget unheard of for three poll intervals is stale.
type Tick struct {
	Type   string    `json:"type"`
	Widget string    `json:"widget"`
	At     time.Time `json:"at"`
}

type ConfigMessage struct {
	Type   string `json:"type"`
	Config any    `json:"config"`
}

// Result answers one action, matched by the id the interface chose.
type Result struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func NewHello(platform string, config any, widgets []string) Hello {
	return Hello{Type: "hello", Version: Version, Platform: platform, Config: config, Widgets: widgets}
}

func NewSnapshot(s Snapshot) SnapshotMessage { return SnapshotMessage{Type: "snapshot", Widget: s} }
func NewToast(t Toast) ToastMessage          { return ToastMessage{Type: "toast", Toast: t} }
func NewConfig(config any) ConfigMessage     { return ConfigMessage{Type: "config", Config: config} }

func NewTick(widget string, at time.Time) Tick {
	return Tick{Type: "tick", Widget: widget, At: at}
}

func NewResult(id string, ok bool, message string) Result {
	return Result{Type: "result", ID: id, OK: ok, Message: message}
}

// Request is any UI → core message: one stdin line, or one POST /v1/send.
type Request struct {
	Type   string            `json:"type"` // "action" | "refresh" | "config.patch"
	ID     string            `json:"id,omitempty"`
	Widget string            `json:"widget,omitempty"`
	Action string            `json:"action,omitempty"`
	Args   map[string]string `json:"args,omitempty"`
	Patch  json.RawMessage   `json:"patch,omitempty"`
}

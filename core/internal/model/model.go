// Package model is the wire contract between ncd-core and the interface:
// widget snapshots, the blocks a panel is built from, and the messages both
// transports carry. docs/rewrite/ARCHITECTURE.md is the authority; a field
// added here needs a line there.
package model

import "time"

// State is a widget's overall health. It picks the dial's LED and rim ring.
type State string

const (
	StateOK   State = "ok"
	StateWarn State = "warn"
	StateCrit State = "crit"
	StateIdle State = "idle" // unknown, offline, nothing to measure
	StateBusy State = "busy" // work in progress (a ComfyUI job)
)

// Tone colours a single element. The UI maps tones to tokens; the core never
// sends a colour. The zero value is neutral and is omitted on the wire.
type Tone string

const (
	ToneNone Tone = ""
	ToneOK   Tone = "ok"
	ToneWarn Tone = "warn"
	ToneCrit Tone = "crit"
	ToneIdle Tone = "idle"
	ToneInfo Tone = "info"
	ToneBusy Tone = "busy"
)

// Tone is the colour a state is announced in: a toast, a page badge.
func (s State) Tone() Tone {
	switch s {
	case StateOK:
		return ToneOK
	case StateWarn:
		return ToneWarn
	case StateCrit:
		return ToneCrit
	case StateBusy:
		return ToneInfo
	default:
		return ToneIdle
	}
}

// Snapshot is everything the interface knows about one widget. It is
// replaced whole on every change; the UI keeps no widget state of its own.
type Snapshot struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Order      int    `json:"order"`
	State      State  `json:"state"`
	StateLabel string `json:"stateLabel"`
	Glance     Glance `json:"glance"`
	// Progress is 0..1 while busy; nil draws the indeterminate arc.
	Progress  *float64   `json:"progress,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
	PollSecs  int        `json:"pollSecs"`
	Error     string     `json:"error,omitempty"`
	Width     int        `json:"width,omitempty"` // preferred panel width, logical px; 0 = the UI default
	Pages     []Page     `json:"pages"`
	Footer    string     `json:"footer,omitempty"`
	Menu      []MenuItem `json:"menu,omitempty"`

	// Issues are the widget's current problems under stable ids. The hub
	// toasts an id that was not there on the previous poll; issues never
	// reach the interface themselves.
	Issues []Issue `json:"-"`
}

// Glance is the dock readout under the dial: at most five characters.
type Glance struct {
	Text string `json:"text"`
	Tone Tone   `json:"tone,omitempty"`
}

// Page is one tab of a panel. A single-page widget has one page with an
// empty label, and no tab strip is drawn.
type Page struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Badge  string `json:"badge,omitempty"`
	Tone   Tone   `json:"tone,omitempty"`
	Blocks Blocks `json:"blocks"`
}

// MenuItem is a widget's own entry in the dial's right-click menu. Choosing
// it sends an action with the item's id and no arguments — unless it carries
// a URL to open or text to copy, which the shell does itself.
type MenuItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url,omitempty"`
	Copy  string `json:"copy,omitempty"`
}

// Action is a button that changes the world. With Confirm set, the first
// click only arms it and a second click within four seconds sends it.
type Action struct {
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	Confirm string            `json:"confirm,omitempty"`
	Danger  bool              `json:"danger,omitempty"`
	Args    map[string]string `json:"args,omitempty"`
}

// Issue is a current problem, for toasts on its first appearance.
type Issue struct {
	ID   string
	Text string
	Tone Tone
}

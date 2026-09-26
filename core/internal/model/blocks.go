package model

import (
	"bytes"
	"encoding/json"
)

// Block is one element of a panel, top to bottom. On the wire every block is
// an object carrying its "kind"; the Go types stay plain structs and Blocks
// adds the discriminator when it encodes them.
type Block interface{ Kind() string }

// Blocks encodes each block with its kind, and an empty list as [] rather
// than null so the interface can iterate without guarding.
type Blocks []Block

func (bs Blocks) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, b := range bs {
		if i > 0 {
			buf.WriteByte(',')
		}
		body, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		kind, _ := json.Marshal(b.Kind())
		buf.WriteString(`{"kind":`)
		buf.Write(kind)
		// body is the struct's own object: splice its fields after the kind.
		if len(body) > 2 {
			buf.WriteByte(',')
		}
		buf.Write(body[1:])
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// Section is a printed strip that starts a group: "FILESYSTEMS ....... 4".
type Section struct {
	Title string `json:"title"`
	Meta  string `json:"meta,omitempty"`
	Tone  Tone   `json:"tone,omitempty"`
}

// Row is a name, an optional quiet line under it, and a right-aligned value.
type Row struct {
	Name    string   `json:"name"`
	Sub     string   `json:"sub,omitempty"`
	Value   string   `json:"value,omitempty"`
	Tone    Tone     `json:"tone,omitempty"` // the value's tone
	LED     Tone     `json:"led,omitempty"`  // a leading LED, when set
	Mono    bool     `json:"mono,omitempty"` // the name is an identifier, not prose
	Actions []Action `json:"actions,omitempty"`
}

// Meter is anything bounded: label, a segmented level, the value spelled out.
type Meter struct {
	Label string  `json:"label"`
	Frac  float64 `json:"frac"`
	Value string  `json:"value,omitempty"`
	Note  string  `json:"note,omitempty"`
	Tone  Tone    `json:"tone,omitempty"`
	Mono  bool    `json:"mono,omitempty"` // the label is an identifier (a mount point)
}

// Limit is one plan window: how much is gone and when it comes back.
type Limit struct {
	Label  string  `json:"label"`
	Pct    float64 `json:"pct"`
	Resets string  `json:"resets,omitempty"`
	Tone   Tone    `json:"tone,omitempty"`
}

// Stats is one inline mono line of key/value pairs. It replaces tiles.
type Stats struct {
	Items []Stat `json:"items"`
}

type Stat struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Tone  Tone   `json:"tone,omitempty"`
}

// Chips is every item of a set at a glance, each with its LED.
type Chips struct {
	Items   []Chip `json:"items"`
	Columns int    `json:"columns"`
}

type Chip struct {
	Label string `json:"label"`
	Tone  Tone   `json:"tone"`
	Value string `json:"value,omitempty"`
}

// Alert is a problem in full: what, since when, the evidence, what to do.
type Alert struct {
	Title   string   `json:"title"`
	Meta    string   `json:"meta,omitempty"`
	Body    string   `json:"body,omitempty"`
	Mono    bool     `json:"mono,omitempty"` // the body is a log line
	Tone    Tone     `json:"tone"`
	Actions []Action `json:"actions,omitempty"`
}

// Note is a quiet line of supporting text.
type Note struct {
	Text string `json:"text"`
	Tone Tone   `json:"tone,omitempty"`
	Mono bool   `json:"mono,omitempty"`
}

// Table is aligned columns of numbers (Power's draw table).
type Table struct {
	Columns []Column   `json:"columns"`
	Rows    []TableRow `json:"rows"`
}

type Column struct {
	Label string `json:"label"`
	Align string `json:"align,omitempty"` // "left" | "right"
	Mono  bool   `json:"mono,omitempty"`
}

type TableRow struct {
	Cells []Cell `json:"cells"`
	Tone  Tone   `json:"tone,omitempty"`
	LED   Tone   `json:"led,omitempty"`
	Sub   string `json:"sub,omitempty"`
	// Group opens a group of rows instead: a printed strip with its title
	// and meta, a hairline above, in the same table — so every group's
	// names and numbers share one set of columns. It has no cells.
	Group string `json:"group,omitempty"`
	Meta  string `json:"meta,omitempty"`
}

// Group is a table row that opens a group of rows.
func Group(title, meta string) TableRow { return TableRow{Cells: []Cell{}, Group: title, Meta: meta} }

// Cell is one table cell: plain text on the wire, or {"text","tone"} when a
// single reading needs its own colour (a GPU's VRAM temperature).
type Cell struct {
	Text string
	Tone Tone
}

func (c Cell) MarshalJSON() ([]byte, error) {
	if c.Tone == ToneNone {
		return json.Marshal(c.Text)
	}
	return json.Marshal(struct {
		Text string `json:"text"`
		Tone Tone   `json:"tone"`
	}{c.Text, c.Tone})
}

// Cells is shorthand for a row of untoned cells.
func Cells(texts ...string) []Cell {
	out := make([]Cell, len(texts))
	for i, t := range texts {
		out[i] = Cell{Text: t}
	}
	return out
}

// Donut is a split of one total (Power's monthly cost by component).
type Donut struct {
	Segments []Segment `json:"segments"`
	Center   string    `json:"center"`
	Unit     string    `json:"unit,omitempty"`
}

type Segment struct {
	Label   string  `json:"label"`
	Value   float64 `json:"value"`
	Display string  `json:"display"`
}

// Bars is a small series (Power's last 24 hours).
type Bars struct {
	Values    []float64 `json:"values"`
	Labels    []string  `json:"labels,omitempty"`
	Highlight *int      `json:"highlight,omitempty"`
	Unit      string    `json:"unit,omitempty"`
}

// Empty is a panel with nothing to list saying what is true, in one
// sentence, followed by any commands that would change it, drawn as mono
// chips.
type Empty struct {
	Text     string   `json:"text"`
	Commands []string `json:"commands,omitempty"`
}

// Split lays blocks out in columns (Power's two-column body).
type Split struct {
	Columns []Blocks `json:"columns"`
}

func (Section) Kind() string { return "section" }
func (Row) Kind() string     { return "row" }
func (Meter) Kind() string   { return "meter" }
func (Limit) Kind() string   { return "limit" }
func (Stats) Kind() string   { return "stats" }
func (Chips) Kind() string   { return "chips" }
func (Alert) Kind() string   { return "alert" }
func (Note) Kind() string    { return "note" }
func (Table) Kind() string   { return "table" }
func (Donut) Kind() string   { return "donut" }
func (Bars) Kind() string    { return "bars" }
func (Split) Kind() string   { return "split" }
func (Empty) Kind() string   { return "empty" }

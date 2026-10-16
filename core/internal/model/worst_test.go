package model

import "testing"

func TestStateIsTheWorstOfItsSignals(t *testing.T) {
	page := func(blocks ...Block) []Page { return []Page{{ID: "main", Blocks: blocks}} }
	for _, x := range []struct {
		name   string
		s      Snapshot
		raised bool
		state  State
		label  string
	}{
		{"a crit limit under a green lamp",
			Snapshot{Title: "Codex", State: StateOK, StateLabel: "operational", Glance: Glance{Text: "90%", Tone: ToneCrit},
				Pages: page(Limit{Label: "Week", Pct: 90, Tone: ToneCrit}, Limit{Label: "5h", Pct: 20})},
			true, StateCrit, "week 90%"},
		{"a worse status page wins",
			Snapshot{State: StateCrit, StateLabel: "2 services down", Pages: page(Limit{Label: "Week", Pct: 80, Tone: ToneWarn})},
			false, StateCrit, "2 services down"},
		{"as bad keeps its words",
			Snapshot{State: StateWarn, StateLabel: "Console degraded", Pages: page(Limit{Label: "Week", Pct: 80, Tone: ToneWarn})},
			false, StateWarn, "Console degraded"},
		{"inside a split and a table cell",
			Snapshot{State: StateOK, StateLabel: "normal · 180W", Pages: page(Split{Columns: []Blocks{{}, {
				Table{Rows: []TableRow{{Cells: []Cell{{Text: "GPU0"}, {Text: "92°", Tone: ToneWarn}}}}},
			}}})},
			true, StateWarn, "GPU0 92°"},
		{"a meter, named by its share",
			Snapshot{State: StateOK, StateLabel: "idle", Pages: page(Meter{Label: "VRAM", Frac: 0.88, Tone: ToneWarn})},
			true, StateWarn, "VRAM 88%"},
		{"a job stays busy under a warn",
			Snapshot{State: StateBusy, StateLabel: "generating", Pages: page(Meter{Label: "VRAM", Frac: 0.9, Tone: ToneWarn})},
			false, StateBusy, "generating"},
		{"but not under a crit",
			Snapshot{State: StateBusy, StateLabel: "generating", Pages: page(Meter{Label: "VRAM", Frac: 0.97, Tone: ToneCrit})},
			true, StateCrit, "VRAM 97%"},
		{"an unreachable page with a full plan",
			Snapshot{State: StateIdle, StateLabel: "status page unreachable", Pages: page(Row{Name: "a-very-long-container-name-indeed", Value: "unhealthy", Tone: ToneCrit})},
			true, StateCrit, "a-very-long-container-name-…"},
		{"quiet tones raise nothing",
			Snapshot{State: StateOK, StateLabel: "operational", Pages: page(Chips{Items: []Chip{{Label: "API", Tone: ToneOK}}}, Note{Text: "x", Tone: ToneIdle})},
			false, StateOK, "operational"},
	} {
		s := x.s
		if got := RaiseToWorst(&s); got != x.raised || s.State != x.state || s.StateLabel != x.label {
			t.Errorf("%s: raised %v state %q label %q; want %v %q %q", x.name, got, s.State, s.StateLabel, x.raised, x.state, x.label)
		}
	}
}

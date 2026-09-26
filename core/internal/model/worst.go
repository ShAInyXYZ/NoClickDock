package model

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// LabelBudget is a state label's length (dock-core §11).
const LabelBudget = 28

// RaiseToWorst applies DESIGN A20, "state is the worst of its signals": a
// warn or crit limit, meter, row, table cell, chip or alert raises the
// widget's state to it, and the state label then names that signal
// ("week 90%"). A state already as bad keeps its own words (a status page
// that is worse wins). Work in progress stays busy under a warn — its arc
// is the job — and yields only to crit. It reports whether it raised.
//
// The hub applies it to every snapshot; a writer that words its own
// transitions calls it first, to know.
func RaiseToWorst(s *Snapshot) bool {
	best, name := 0, ""
	consider := func(t Tone, what func() string) {
		if sev := toneSeverity(t); sev > best {
			best, name = sev, what()
		}
	}
	for _, p := range s.Pages {
		walkSignals(p.Blocks, consider)
	}
	consider(s.Glance.Tone, func() string { return strings.ToLower(s.Title) + " " + s.Glance.Text })

	have := stateSeverity(s.State)
	if best <= have || (s.State == StateBusy && best < 2) {
		return false
	}
	s.State = StateWarn
	if best == 2 {
		s.State = StateCrit
	}
	s.StateLabel = fit(strings.TrimSpace(name))
	return true
}

func walkSignals(blocks Blocks, consider func(Tone, func() string)) {
	for _, b := range blocks {
		switch v := b.(type) {
		case Limit:
			consider(v.Tone, func() string { return fmt.Sprintf("%s %.0f%%", strings.ToLower(v.Label), v.Pct) })
		case Meter:
			consider(v.Tone, func() string { return fmt.Sprintf("%s %.0f%%", v.Label, v.Frac*100) })
		case Row:
			tone := v.Tone
			if toneSeverity(v.LED) > toneSeverity(tone) {
				tone = v.LED
			}
			consider(tone, func() string { return v.Name + " " + v.Value })
		case Alert:
			consider(v.Tone, func() string { return v.Title })
		case Chips:
			for _, c := range v.Items {
				consider(c.Tone, func() string { return c.Label + " " + c.Value })
			}
		case Table:
			for _, r := range v.Rows {
				first := ""
				if len(r.Cells) > 0 {
					first = r.Cells[0].Text
				}
				consider(r.Tone, func() string { return first })
				for _, c := range r.Cells {
					consider(c.Tone, func() string { return first + " " + c.Text })
				}
			}
		case Split:
			for _, col := range v.Columns {
				walkSignals(col, consider)
			}
		}
	}
}

func toneSeverity(t Tone) int {
	switch t {
	case ToneCrit:
		return 2
	case ToneWarn:
		return 1
	}
	return 0
}

func stateSeverity(s State) int {
	switch s {
	case StateCrit:
		return 2
	case StateWarn:
		return 1
	}
	return 0
}

// fit cuts a label to the budget, with an ellipsis.
func fit(label string) string {
	if utf8.RuneCountInString(label) <= LabelBudget {
		return label
	}
	r := []rune(label)
	return strings.TrimSpace(string(r[:LabelBudget-1])) + "…"
}

package widgets

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// The snapshot writers' budgets (dock-core §11, DESIGN A12), checked on
// every mock and every variant, still and live, so the gallery never shows
// a panel the real widgets could not produce.
func TestMocksKeepTheContract(t *testing.T) {
	for _, live := range []bool{false, true} {
		for _, c := range Mocks(live) {
			id := c.Meta().ID
			s, err := c.Collect(context.Background())
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			check(t, id, s)
			for name, v := range c.(hub.Varianter).Variants(time.Now()) {
				check(t, id+"/"+name, v)
			}
		}
	}
}

func check(t *testing.T, id string, s model.Snapshot) {
	t.Helper()
	if n := utf8.RuneCountInString(s.Glance.Text); n > 5 {
		t.Errorf("%s: glance %q is %d characters", id, s.Glance.Text, n)
	}
	if s.State == "" || s.StateLabel == "" || utf8.RuneCountInString(s.StateLabel) > 28 {
		t.Errorf("%s: state %q label %q", id, s.State, s.StateLabel)
	}
	if len(s.Pages) == 0 {
		t.Errorf("%s: no pages", id)
	}
	if len(s.Pages) > 1 {
		for _, p := range s.Pages {
			if p.Label == "" {
				t.Errorf("%s: page %q has no tab label", id, p.ID)
			}
		}
	}
	for _, p := range s.Pages {
		walk(t, id, p.Blocks)
	}
	if _, err := json.Marshal(s); err != nil {
		t.Errorf("%s: %v", id, err)
	}
}

func walk(t *testing.T, id string, blocks model.Blocks) {
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Alert:
			if n := utf8.RuneCountInString(v.Meta); n > 24 {
				t.Errorf("%s: alert %q meta %q is %d characters", id, v.Title, v.Meta, n)
			}
		case model.Split:
			for _, col := range v.Columns {
				walk(t, id, col)
			}
		case model.Meter:
			if v.Frac < 0 || v.Frac > 1 {
				t.Errorf("%s: meter %q frac %v", id, v.Label, v.Frac)
			}
		case model.Limit:
			if v.Pct < 0 || v.Pct > 100 {
				t.Errorf("%s: limit %q pct %v", id, v.Label, v.Pct)
			}
		}
		for _, a := range actionsOf(b) {
			if a.Confirm == "" {
				t.Errorf("%s: action %q runs on one click (every world-changing click asks twice)", id, a.ID)
			}
		}
	}
}

func actionsOf(b model.Block) []model.Action {
	switch v := b.(type) {
	case model.Row:
		return v.Actions
	case model.Alert:
		return v.Actions
	}
	return nil
}

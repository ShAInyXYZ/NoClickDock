package claude

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

const statusPage = "https://status.claude.com"

// The status page's own names, shortened where they carry a host
// (design M2 r1: "Claude Code", "Console").
var componentShort = map[string]string{
	"Claude Console (platform.claude.com)": "Console",
	"Claude API (api.anthropic.com)":       "Claude API",
	"Claude Cowork":                        "Cowork",
}

func short(name string) string {
	if s, ok := componentShort[name]; ok {
		return s
	}
	return statuspage.Trim(name)
}

var modelShort = map[string]string{
	"claude-opus-5":             "opus 5",
	"claude-sonnet-5":           "sonnet 5",
	"claude-fable-5":            "fable 5",
	"claude-haiku-4-5-20251001": "haiku 4.5",
}

var (
	providerRE = regexp.MustCompile(`^(us\.)?anthropic\.`)
	dateRE     = regexp.MustCompile(`-\d{8}$`)
)

// shortModel is a model id as people say it: "claude-haiku-4-5-20251001" →
// "haiku 4.5"; ids not in the table lose their provider, date and "claude-",
// and a trailing run of numbers is a version ("opus-5-5" → "opus 5.5").
func shortModel(id string) string {
	if s, ok := modelShort[id]; ok {
		return s
	}
	m := providerRE.ReplaceAllString(id, "")
	m = dateRE.ReplaceAllString(m, "")
	m = strings.TrimPrefix(m, "claude-")
	parts := strings.Split(m, "-")
	i := len(parts)
	for i > 0 && isNumber(parts[i-1]) {
		i--
	}
	name := strings.Join(parts[:i], " ")
	if i < len(parts) {
		name = strings.TrimSpace(name + " " + strings.Join(parts[i:], "."))
	}
	if name == "" {
		return "unknown"
	}
	return name
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return s != "" && err == nil
}

func limitTone(pct float64) model.Tone {
	switch {
	case pct >= 90:
		return model.ToneCrit
	case pct >= 75:
		return model.ToneWarn
	}
	return model.ToneNone
}

// poll is one collect: the status page now, the CLI's limits and the
// transcripts' spend as last read on their own cadences.
type poll struct {
	page     statuspage.Page
	limits   []limit
	limitsAt time.Time
	usage    *usage // nil until the first scan has finished
	now      time.Time
}

func render(p poll) model.Snapshot {
	s := model.Snapshot{
		Width:      440,
		State:      statuspage.State(p.page),
		StateLabel: statuspage.StateLabel(p.page, short),
		Menu:       []model.MenuItem{{ID: "open-status", Label: "Open status page", URL: statusPage}},
	}

	// The dial reads the plan: the highest limit, else the day's tokens.
	s.Glance = model.Glance{Text: "—"}
	if len(p.limits) > 0 {
		top := p.limits[0].Pct
		for _, l := range p.limits {
			top = max(top, l.Pct)
		}
		s.Glance = model.Glance{Text: fmt.Sprintf("%.0f%%", top), Tone: limitTone(top)}
	} else if p.usage != nil && p.usage.H24 > 0 {
		s.Glance = model.Glance{Text: sys.FmtTokens(p.usage.H24)}
	}
	if !p.limitsAt.IsZero() && len(p.limits) > 0 {
		s.Footer = "limits as of " + sys.TimeAgo(p.limitsAt, p.now)
	}

	// Problems first (A19): broken services and open incidents, then how
	// much of the plan is gone, then the healthy services, quietly.
	blocks := append(statuspage.Degraded(p.page, short), statuspage.Incidents(p.page, p.now, 3)...)
	blocks = append(blocks, usageBlocks(p)...)
	blocks = append(blocks, services(p.page)...)
	s.Pages = []model.Page{{ID: "main", Blocks: blocks}}
	return s
}

func usageBlocks(p poll) model.Blocks {
	has := p.usage != nil && p.usage.Available
	if len(p.limits) == 0 && !has {
		if p.usage == nil {
			return nil // the first scan has not finished: nothing to say yet
		}
		return model.Blocks{
			model.Section{Title: "Usage"},
			model.Empty{Text: "No Claude Code sessions on this machine in the last 7 days."},
		}
	}
	out := model.Blocks{model.Section{Title: "Usage"}}
	for _, l := range p.limits {
		resets := l.Raw
		if !l.Resets.IsZero() {
			resets = sys.Clock(l.Resets, p.now)
		}
		if resets != "" {
			resets = "resets " + resets
		}
		out = append(out, model.Limit{Label: l.Label, Pct: l.Pct, Resets: resets, Tone: limitTone(l.Pct)})
	}
	if has {
		u := p.usage
		out = append(out, model.Stats{Items: []model.Stat{
			{Key: "1h", Value: sys.FmtTokens(u.H1)}, {Key: "24h", Value: sys.FmtTokens(u.H24)},
			{Key: "7d", Value: sys.FmtTokens(u.D7)}, {Key: "sessions", Value: strconv.Itoa(u.Sessions)},
		}})
		if len(u.Models) > 0 {
			// Readings, not status: a line of stats, not chips (design M2 r1).
			line := model.Stats{}
			for _, m := range u.Models {
				line.Items = append(line.Items, model.Stat{Key: shortModel(m.Model), Value: sys.FmtTokens(m.Tokens)})
			}
			out = append(out, line)
		}
	}
	return out
}

// services is the healthy majority as chips; a broken one is already a row
// above (nothing twice).
func services(page statuspage.Page) model.Blocks {
	if !page.Reached {
		return model.Blocks{model.Section{Title: "Services"}, model.Note{Text: "status.claude.com did not answer", Tone: model.ToneIdle}}
	}
	chips := model.Chips{Columns: 3}
	for _, c := range page.Components {
		if c.Status == "operational" {
			chips.Items = append(chips.Items, model.Chip{Label: short(c.Name), Tone: model.ToneOK})
		}
	}
	if len(chips.Items) == 0 {
		return nil
	}
	return model.Blocks{model.Section{Title: "Services", Meta: fmt.Sprintf("%d operational", len(chips.Items))}, chips}
}

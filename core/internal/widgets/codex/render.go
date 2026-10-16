package codex

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

const statusPage = "https://status.openai.com"

// The components a Codex user depends on, in chip order. Anything else on
// the (whole-OpenAI) page gets words only when it is broken.
var codexComponents = []string{
	"Codex API", "Codex Web", "Codex in ChatGPT Desktop", "CLI", "VS Code extension",
	"Responses", "Chat Completions", "Login", "Conversations",
}

var componentShort = map[string]string{
	"Codex in ChatGPT Desktop": "Desktop app",
	"VS Code extension":        "VS Code",
	"Chat Completions":         "Completions",
}

func short(name string) string {
	if s, ok := componentShort[name]; ok {
		return s
	}
	return statuspage.Trim(name)
}

var dateRE = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

func shortModel(id string) string {
	if m := dateRE.ReplaceAllString(id, ""); m != "" {
		return m
	}
	return "unknown"
}

// windowLabel: 300 → "5h", 10080 → "week", 2880 → "2d", 45 → "45m".
func windowLabel(minutes int) string {
	switch {
	case minutes < 0:
		return "limit"
	case minutes == 10080:
		return "week"
	case minutes > 1440 && minutes%1440 == 0:
		return fmt.Sprintf("%dd", minutes/1440)
	case minutes%60 == 0 && minutes > 0:
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
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

type poll struct {
	page  statuspage.Page
	spend *spend // nil until the first scan has finished
	now   time.Time
}

func render(p poll) model.Snapshot {
	s := model.Snapshot{
		Width:      440,
		State:      statuspage.State(p.page),
		StateLabel: statuspage.StateLabel(p.page, short),
		Menu:       []model.MenuItem{{ID: "open-status", Label: "Open status page", URL: statusPage}},
		Glance:     model.Glance{Text: "—"},
	}
	if sp := p.spend; sp != nil {
		// The dial reads the plan: the highest live window that has not
		// reset since it was recorded, else the day's tokens.
		top, any := 0.0, false
		for _, g := range sp.Groups {
			for _, r := range g.Rows {
				if !g.Idle && !lapsed(r, p.now) {
					top, any = max(top, r.Pct), true
				}
			}
		}
		switch {
		case any:
			s.Glance = model.Glance{Text: fmt.Sprintf("%.0f%%", top), Tone: limitTone(top)}
		case sp.H24 > 0:
			s.Glance = model.Glance{Text: sys.FmtTokens(sp.H24)}
		}
		if !sp.Seen.IsZero() {
			// The reading is as old as the last Codex turn: say so.
			s.Footer = "limits as of " + sys.TimeAgo(sp.Seen, p.now)
		}
	}

	blocks := append(statuspage.Degraded(p.page, short), statuspage.Incidents(p.page, p.now, 3)...)
	blocks = append(blocks, usage(p)...)
	blocks = append(blocks, services(p.page)...)
	s.Pages = []model.Page{{ID: "main", Blocks: blocks}}
	return s
}

// lapsed is a window whose reset has passed since it was recorded: its
// percentage no longer means anything (the Python widget kept showing it).
func lapsed(r row, now time.Time) bool { return !r.Resets.IsZero() && r.Resets.Before(now) }

func usage(p poll) model.Blocks {
	sp := p.spend
	if sp == nil {
		return nil
	}
	if len(sp.Groups) == 0 && !sp.Available {
		return model.Blocks{model.Section{Title: "Usage"}, model.Note{Text: "No Codex sessions in the last 7 days", Tone: model.ToneIdle}}
	}
	sec := model.Section{Title: "Usage"}
	if sp.Plan != "" {
		sec.Meta = sp.Plan + " plan"
	}
	out := model.Blocks{sec}
	// Every allowance, each under its own name when there are several: two
	// allowances are two meanings of "used", never merged into one bar.
	for _, g := range sp.Groups {
		name := g.Name
		if name == "" {
			name = g.ID
		}
		if g.Idle {
			// Unknown is never zero: a bucket that never moves is words.
			text := "never reports usage"
			if name != "" {
				text = name + " · " + text
			}
			out = append(out, model.Note{Text: text, Tone: model.ToneIdle})
			continue
		}
		for _, r := range g.Rows {
			label := windowLabel(r.Minutes)
			if len(sp.Groups) > 1 && name != "" {
				label = name + " " + label
			} else {
				label = statuspage.Sentence(label)
			}
			l := model.Limit{Label: label, Pct: r.Pct, Tone: limitTone(r.Pct)}
			switch {
			case lapsed(r, p.now):
				l.Pct, l.Tone, l.Resets = 0, model.ToneNone, "reset "+sys.TimeAgo(r.Resets, p.now)
			case !r.Resets.IsZero():
				l.Resets = "resets " + sys.Clock(r.Resets, p.now)
			}
			out = append(out, l)
		}
	}
	if sp.Available {
		out = append(out, model.Stats{Items: []model.Stat{
			{Key: "1h", Value: sys.FmtTokens(sp.H1)}, {Key: "24h", Value: sys.FmtTokens(sp.H24)},
			{Key: "7d", Value: sys.FmtTokens(sp.D7)}, {Key: "sessions", Value: strconv.Itoa(sp.Sessions)},
		}})
		if len(sp.Models) > 0 {
			line := model.Stats{}
			for _, m := range sp.Models {
				line.Items = append(line.Items, model.Stat{Key: shortModel(m.Model), Value: sys.FmtTokens(m.Tokens)})
			}
			out = append(out, line)
		}
	}
	return out
}

// services: the Codex components as chips (a broken one is already a row
// above), then how many others are fine.
func services(page statuspage.Page) model.Blocks {
	if !page.Reached {
		return model.Blocks{model.Section{Title: "Services"}, model.Note{Text: "status.openai.com did not answer", Tone: model.ToneIdle}}
	}
	if len(page.Components) == 0 {
		return nil
	}
	byName := map[string]string{}
	for _, c := range page.Components {
		byName[c.Name] = c.Status
	}
	chips := model.Chips{Columns: 3}
	shown := map[string]bool{}
	for _, name := range codexComponents {
		status, ok := byName[name]
		if !ok || shown[name] {
			continue
		}
		shown[name] = true
		if status == "operational" {
			chips.Items = append(chips.Items, model.Chip{Label: short(name), Tone: model.ToneOK})
		}
	}
	out := model.Blocks{model.Section{Title: "Services", Meta: fmt.Sprintf("%d of %d", len(chips.Items), len(shown))}}
	if len(chips.Items) > 0 {
		out = append(out, chips)
	}
	if others := len(page.Components) - len(shown); others > 0 && len(statuspage.Broken(page)) == 0 {
		out = append(out, model.Note{Text: fmt.Sprintf("%d other OpenAI %s operational", others, plural(others, "service", "services"))})
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

package statuspage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// Short names a component the way the panel shows it; each widget has its
// own table and falls back to Trim.
type Short func(name string) string

// Trim drops a trailing "(host.example.com)" from a component name.
func Trim(name string) string {
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		return name[:i]
	}
	return name
}

// Label is a component status in the panel's words.
func Label(status string) string {
	switch status {
	case "operational":
		return "operational"
	case "degraded_performance":
		return "degraded"
	case "partial_outage":
		return "partial outage"
	case "major_outage":
		return "major outage"
	case "under_maintenance":
		return "maintenance"
	}
	return strings.ReplaceAll(status, "_", " ")
}

// Tone is a component status's colour (dock-core §7).
func Tone(status string) model.Tone {
	switch status {
	case "operational":
		return model.ToneOK
	case "degraded_performance", "partial_outage":
		return model.ToneWarn
	case "major_outage":
		return model.ToneCrit
	}
	return model.ToneIdle
}

func severity(status string) int {
	switch status {
	case "major_outage":
		return 3
	case "partial_outage":
		return 2
	case "degraded_performance":
		return 1
	}
	return 0
}

// ImpactTone is an incident's colour.
func ImpactTone(impact string) model.Tone {
	switch impact {
	case "minor":
		return model.ToneWarn
	case "major", "critical":
		return model.ToneCrit
	case "none", "maintenance":
		return model.ToneInfo
	}
	return model.ToneWarn
}

// State is the widget's state for the page's indicator. A page that did not
// answer is idle: it says nothing about the service.
func State(p Page) model.State {
	if !p.Reached {
		return model.StateIdle
	}
	switch p.Indicator {
	case "none":
		return model.StateOK
	case "minor":
		return model.StateWarn
	case "major", "critical":
		return model.StateCrit
	}
	return model.StateIdle
}

// Broken is every component that is not operational, worst first.
func Broken(p Page) []Component {
	var out []Component
	for _, c := range p.Components {
		if c.Status != "operational" && c.Status != "" {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return severity(out[i].Status) > severity(out[j].Status) })
	return out
}

// StateLabel names the fact (DESIGN "State labels"), never the page's Title
// Case phrase: "Console degraded" (a name keeps its case), "3 services
// degraded", "1 active incident", "operational", "status page unreachable".
func StateLabel(p Page, short Short) string {
	if !p.Reached {
		return "status page unreachable"
	}
	// Failures name the fact; maintenance is said only when nothing fails.
	var failing, resting []Component
	for _, c := range Broken(p) {
		if c.Status == "under_maintenance" {
			resting = append(resting, c)
		} else {
			failing = append(failing, c)
		}
	}
	word := func(c Component) string {
		if c.Status == "major_outage" {
			return "down"
		}
		return "degraded"
	}
	switch {
	case len(failing) == 1:
		// The name when it fits the label's budget; the problems-first
		// block below the header carries it whole either way.
		if l := short(failing[0].Name) + " " + Label(failing[0].Status); fits(l) {
			return l
		}
		if l := short(failing[0].Name) + " " + word(failing[0]); fits(l) {
			return l
		}
		return "1 service " + word(failing[0])
	case len(failing) > 1:
		return fmt.Sprintf("%d services %s", len(failing), word(failing[0]))
	case len(p.Incidents) > 0:
		n := len(p.Incidents)
		return fmt.Sprintf("%d active %s", n, plural(n, "incident", "incidents"))
	case len(resting) == 1:
		if l := short(resting[0].Name) + " maintenance"; fits(l) {
			return l
		}
		return "1 service in maintenance"
	case len(resting) > 1:
		return fmt.Sprintf("%d services in maintenance", len(resting))
	case p.Indicator == "none":
		return "operational"
	}
	if l := strings.ToLower(p.Description); fits(l) {
		return l
	}
	return "service disruption"
}

// LabelBudget is a state label's length (dock-core §11): the panel header
// and the dial's label never have to cut one.
const LabelBudget = 28

func fits(label string) bool { return utf8.RuneCountInString(label) <= LabelBudget }

// Issues are the page's problems for toasts: every broken component, as
// comp:<name>:<status>, and every open incident, as inc:<id>.
func Issues(p Page, short Short) []model.Issue {
	var out []model.Issue
	for _, c := range Broken(p) {
		tone := Tone(c.Status)
		if tone == model.ToneIdle {
			tone = model.ToneInfo // maintenance is news, not a failure
		}
		out = append(out, model.Issue{ID: "comp:" + c.Name + ":" + c.Status, Text: Sentence(short(c.Name) + " " + Label(c.Status)), Tone: tone})
	}
	for _, i := range p.Incidents {
		out = append(out, model.Issue{ID: "inc:" + i.ID, Text: i.Name, Tone: ImpactTone(i.Impact)})
	}
	return out
}

// Transition words a change of state for its toast: one plain sentence.
func Transition(to model.State, label string) string {
	switch to {
	case model.StateOK:
		return "All systems operational"
	case model.StateIdle:
		return "Status page unreachable"
	}
	return Sentence(label)
}

// Degraded is the problems-first block: every broken component as a row
// with words, under a section in the worst one's tone. Nothing when all is
// well.
func Degraded(p Page, short Short) model.Blocks {
	broken := Broken(p)
	if len(broken) == 0 {
		return nil
	}
	tone := Tone(broken[0].Status)
	if tone == model.ToneIdle {
		tone = model.ToneNone
	}
	out := model.Blocks{model.Section{Title: "Degraded", Meta: strconv.Itoa(len(broken)), Tone: tone}}
	for _, c := range broken {
		t := Tone(c.Status)
		out = append(out, model.Row{Name: short(c.Name), Value: Label(c.Status), Tone: t, LED: t})
	}
	return out
}

// Incidents is up to max open incidents as alerts: the status and its age
// in the meta (≤ 24 characters), the latest update in the body.
func Incidents(p Page, now time.Time, max int) model.Blocks {
	if len(p.Incidents) == 0 {
		return nil
	}
	tone := model.ToneWarn
	for _, i := range p.Incidents {
		if ImpactTone(i.Impact) == model.ToneCrit {
			tone = model.ToneCrit
		}
	}
	out := model.Blocks{model.Section{Title: "Active incidents", Meta: strconv.Itoa(len(p.Incidents)), Tone: tone}}
	for _, i := range p.Incidents[:min(len(p.Incidents), max)] {
		when := i.Latest.CreatedAt
		if when.IsZero() {
			when = i.UpdatedAt
		}
		meta := i.Status
		if age := sys.Age(when, now); age != "" && utf8.RuneCountInString(meta+" · "+age) <= 24 {
			meta += " · " + age
		}
		out = append(out, model.Alert{Title: i.Name, Meta: cut(meta, 24), Body: cut(strings.TrimSpace(i.Latest.Body), 160), Tone: ImpactTone(i.Impact)})
	}
	return out
}

// Sentence starts a phrase with a capital, leaving the rest as written
// (names keep their own case).
func Sentence(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

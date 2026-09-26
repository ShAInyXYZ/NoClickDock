package statuspage

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

func TestStateLabelNamesTheFact(t *testing.T) {
	c := func(name, status string) Component { return Component{Name: name, Status: status} }
	for _, x := range []struct {
		page Page
		want string
	}{
		{Page{}, "status page unreachable"},
		{Page{Reached: true, Indicator: "none", Components: []Component{c("API", "operational")}}, "operational"},
		{Page{Reached: true, Indicator: "minor", Components: []Component{c("Console (x.example.com)", "degraded_performance")}}, "Console degraded"},
		{Page{Reached: true, Indicator: "major", Components: []Component{c("A", "degraded_performance"), c("B", "major_outage")}}, "2 services down"},
		{Page{Reached: true, Indicator: "minor", Components: []Component{c("A", "partial_outage"), c("B", "degraded_performance")}}, "2 services degraded"},
		{Page{Reached: true, Indicator: "minor", Incidents: []Incident{{ID: "1"}}}, "1 active incident"},
		{Page{Reached: true, Indicator: "maintenance", Components: []Component{c("Batch", "under_maintenance")}}, "Batch maintenance"},
		{Page{Reached: true, Indicator: "minor", Description: "Minor Service Outage"}, "minor service outage"},
		// Over the 28-character budget: a shorter word, then the count form.
		{Page{Reached: true, Indicator: "minor", Components: []Component{c("Compliance API", "partial_outage")}}, "Compliance API degraded"},
		{Page{Reached: true, Indicator: "minor", Components: []Component{c("Claude for Government", "partial_outage")}}, "1 service degraded"},
		{Page{Reached: true, Indicator: "major", Components: []Component{c("Image Generation Pipelines", "major_outage")}}, "1 service down"},
		{Page{Reached: true, Indicator: "maintenance", Components: []Component{c("Regional Data Residency", "under_maintenance")}}, "1 service in maintenance"},
		{Page{Reached: true, Indicator: "critical", Description: "Multiple Systems Are Experiencing An Outage"}, "service disruption"},
	} {
		got := StateLabel(x.page, Trim)
		if got != x.want {
			t.Errorf("%+v → %q, want %q", x.page, got, x.want)
		}
		if utf8.RuneCountInString(got) > LabelBudget {
			t.Errorf("%q is over the budget", got)
		}
	}
}

func TestIncidentAlertsFitTheirBudget(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	p := Page{Reached: true, Incidents: []Incident{
		{ID: "1", Name: "One", Status: "investigating", Impact: "major", Latest: Update{Body: strings.Repeat("word ", 60), CreatedAt: now.Add(-3 * time.Hour)}},
		{ID: "2", Name: "Two", Status: "a-status-name-far-too-long-for-a-meta", Impact: "none", UpdatedAt: now.Add(-time.Minute)},
		{ID: "3", Name: "Three"}, {ID: "4", Name: "Four"},
	}}
	blocks := Incidents(p, now, 3)
	if s := blocks[0].(model.Section); s.Tone != model.ToneCrit || s.Meta != "4" {
		t.Errorf("section %+v", s)
	}
	if len(blocks) != 4 {
		t.Fatalf("at most three alerts: %d blocks", len(blocks))
	}
	a := blocks[1].(model.Alert)
	if a.Meta != "investigating · 3h" || utf8.RuneCountInString(a.Body) > 160 || a.Tone != model.ToneCrit {
		t.Errorf("alert %+v", a)
	}
	if b := blocks[2].(model.Alert); utf8.RuneCountInString(b.Meta) > 24 || b.Tone != model.ToneInfo {
		t.Errorf("meta over budget or tone: %+v", b)
	}
	if Transition(model.StateOK, "operational") != "All systems operational" || Transition(model.StateWarn, "console degraded") != "Console degraded" {
		t.Error("transitions")
	}
}

package localservers

import (
	"strings"
	"testing"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

func TestLaunchSetupLivesInSettingsAndEndpointsAreSingleRows(t *testing.T) {
	reason := "Launch configuration is unknown"
	s := server{Key: "123:1", Name: "studio", Dir: "/projects/studio", Runtime: "Node.js", Dev: true, Running: true, Proc: process{PID: 123}, RestartReason: reason, Endpoints: []endpoint{{5173, "127.0.0.1", "1"}, {5174, "127.0.0.1", "2"}}}
	snap := render([]server{s}, false, nil, nil, nil)
	endpoints, stops := 0, 0
	for _, page := range snap.Pages {
		for _, block := range page.Blocks {
			switch b := block.(type) {
			case model.Note:
				if b.Text == reason {
					t.Fatal("launch explanation leaked into preview")
				}
			case model.Row:
				if b.Name == "Launch command" || strings.HasPrefix(b.Name, ":") {
					t.Fatal("redundant row in preview")
				}
				for _, a := range b.Actions {
					if a.ID == "register" {
						t.Fatal("registration still in preview")
					}
					if a.ID == "stop" {
						stops++
					}
				}
				if b.NameAction != nil {
					endpoints++
					if !strings.Contains(b.Name, ":517") || b.Sub != "Node.js · PID 123" || b.Value != "running" {
						t.Fatalf("missing server identity: %+v", b)
					}
					found := false
					for _, m := range snap.Menu {
						if m.ID == b.NameAction.ID && m.URL == "http://"+b.Name {
							found = true
						}
					}
					if !found {
						t.Fatal("address has no snapshot-owned browser shortcut")
					}
				}
			}
		}
	}
	if endpoints != 2 || stops != 1 {
		t.Fatalf("%d endpoints, %d process stop buttons", endpoints, stops)
	}
	register, explanation := false, false
	for _, block := range snap.Settings {
		switch b := block.(type) {
		case model.Row:
			for _, a := range b.Actions {
				if a.ID == "register" && a.Args["server"] == s.Key && a.Confirm != "" {
					register = true
				}
			}
		case model.Note:
			explanation = explanation || b.Text == reason
		}
	}
	if !register || !explanation {
		t.Fatal("registration/explanation missing from Settings")
	}
}

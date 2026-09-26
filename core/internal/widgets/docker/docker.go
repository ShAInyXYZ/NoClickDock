// Package docker watches every container, grouped as compose stacks, and
// can start, stop, restart and remove them. Until its collector is ported
// (M3) it only has a mock.
package docker

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "docker", Title: "Docker", Order: 50, PollSecs: 20}
}

func container(name string, actions ...string) []model.Action {
	var out []model.Action
	for _, a := range actions {
		act := model.Action{ID: a, Label: a, Args: map[string]string{"container": name}}
		switch a {
		case "stop", "restart":
			act.Confirm = a + "?"
		case "remove":
			act.Confirm, act.Danger = "remove?", true
		}
		out = append(out, act)
	}
	return out
}

// Mock is an invented host with one unhealthy service and one crash: every
// stack and container name here is made up.
func (Widget) Mock(time.Time) model.Snapshot {
	ok, crit, idle := model.ToneOK, model.ToneCrit, model.ToneIdle
	return model.Snapshot{
		State:      model.StateCrit,
		StateLabel: "2 need attention",
		Glance:     model.Glance{Text: "12", Tone: crit}, // running containers
		Width:      440,
		Pages: []model.Page{
			{ID: "now", Label: "Now", Badge: "2", Tone: crit, Blocks: model.Blocks{
				model.Section{Title: "Needs attention", Meta: "2", Tone: crit},
				model.Alert{
					Title: "api-gateway", Meta: "unhealthy · 3m", Tone: crit, Mono: true,
					Body:    "GET /healthz 503 upstream connect error",
					Actions: container("api-gateway", "restart", "stop"),
				},
				model.Alert{
					Title: "worker-queue", Meta: "exited 1 · 12m ago", Tone: crit, Mono: true,
					Body:    "panic: dial tcp: lookup cache: no such host",
					Actions: container("worker-queue", "start", "remove"),
				},
				model.Section{Title: "Running", Meta: "12"},
				model.Row{Name: "storefront", Sub: "compose · 5/5 up", Value: "1.2G · 4%", LED: ok, Actions: container("storefront", "restart", "stop")},
				model.Row{Name: "postgres", Sub: "postgres:16 · :5432", Value: "412M · 1%", LED: ok, Actions: container("postgres", "restart", "stop")},
				model.Row{Name: "cache", Sub: "redis:7 · healthy", Value: "38M · 0%", LED: ok, Actions: container("cache", "restart", "stop")},
				model.Row{Name: "metrics", Sub: "compose · 4/4 up", Value: "610M · 2%", LED: ok, Actions: container("metrics", "restart", "stop")},
			}},
			{ID: "stopped", Label: "Stopped", Badge: "3", Blocks: model.Blocks{
				model.Section{Title: "Stopped this week", Meta: "2"},
				model.Row{Name: "docs-preview", Sub: "exited 143 · docker stop · 2d ago", LED: idle, Actions: container("docs-preview", "start", "remove")},
				model.Row{Name: "load-test", Sub: "exited 0 · 4d ago", LED: idle, Actions: container("load-test", "start", "remove")},
				model.Section{Title: "Dormant", Meta: "1"},
				model.Row{Name: "legacy-cron", Sub: "exited 0 · 41d ago", LED: idle, Actions: container("legacy-cron", "start", "remove")},
			}},
			{ID: "system", Label: "System", Blocks: model.Blocks{
				model.Section{Title: "Engine"},
				model.Stats{Items: []model.Stat{{Key: "containers", Value: "17"}, {Key: "images", Value: "46"}, {Key: "volumes", Value: "21"}}},
				model.Section{Title: "Reclaimable"},
				model.Row{Name: "Images", Value: "38G", Tone: model.ToneWarn},
				model.Row{Name: "Build cache", Value: "22G", Tone: model.ToneWarn},
				model.Row{Name: "Containers", Value: "1.4G"},
				model.Note{Text: "Volumes are never pruned from here."},
			}},
		},
		Footer: "engine 27.3 · compose 2.29",
		Menu:   []model.MenuItem{{ID: "open-portainer", Label: "Open Portainer"}},
	}
}

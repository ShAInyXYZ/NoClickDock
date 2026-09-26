// Package docker watches every container, grouped as compose stacks, and
// can start, stop, restart and remove them. Parity: docs/rewrite/specs/docker.md.
// Until its collector is ported (M3) it only has a mock.
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

// Every Docker action asks twice (decision Q1); remove cannot be undone.
var confirms = map[string]string{
	"start": "start?", "stop": "stop?", "restart": "restart?", "resume": "resume?", "remove": "remove?",
}

func actions(scope, key, name string, verbs ...string) []model.Action {
	var out []model.Action
	for _, v := range verbs {
		out = append(out, model.Action{
			ID: scope + "." + v, Label: v, Confirm: confirms[v], Danger: v == "remove",
			Args: map[string]string{key: name},
		})
	}
	return out
}

func container(name string, verbs ...string) []model.Action {
	return actions("container", "container", name, verbs...)
}

func unit(name string, verbs ...string) []model.Action {
	return actions("unit", "unit", name, verbs...)
}

func prune(id, label, confirm string) []model.Action {
	return []model.Action{{ID: id, Label: label, Confirm: confirm, Danger: true}}
}

// Mock is an invented host: one service failing its health check, one
// OOM-killed two hours ago, a stack running one member short. Every stack and
// container name here is made up. Live, the failing service recovers for
// twenty seconds of every minute, so its toasts can be watched.
func (Widget) Mock(now time.Time) model.Snapshot {
	ok, warn, crit, idle := model.ToneOK, model.ToneWarn, model.ToneCrit, model.ToneIdle
	ill := now.Second() < 40 // the still instant (…:07) is ill

	s := model.Snapshot{
		Width:  440,
		Footer: "engine 27.3 · compose 2.29",
		Menu:   []model.MenuItem{{ID: "open-portainer", Label: "Open Portainer", URL: "https://localhost:9443"}},
	}
	attention := model.Blocks{}
	badge := "1"
	if ill {
		s.State, s.StateLabel = model.StateCrit, "1 container unhealthy"
		s.Glance = model.Glance{Text: "11", Tone: crit}
		s.Issues = []model.Issue{{ID: "unhealthy:ledger-api", Text: "ledger-api is unhealthy", Tone: crit}}
		badge = "2"
		attention = append(attention, model.Alert{
			Title: "ledger-api", Meta: "unhealthy · 3m", Tone: crit, Mono: true,
			Body:    "3 checks failed · GET /healthz 503 upstream db timeout after 5000 ms",
			Actions: container("ledger-api", "restart", "stop"),
		})
	} else {
		s.State, s.StateLabel = model.StateWarn, "1 crashed today"
		s.Glance = model.Glance{Text: "11", Tone: warn}
	}
	attention = append(attention, model.Alert{
		Title: "thumbnailer", Meta: "OOM (137) · 2h", Tone: warn, Mono: true,
		Body:    "Killed: memory cgroup limit 512M reached (rss 511.8M)",
		Actions: container("thumbnailer", "start", "remove"),
	})
	pageTone := warn
	if ill {
		pageTone = crit
	}

	current := append(model.Blocks{model.Section{Title: "Needs attention", Meta: badge, Tone: pageTone}}, attention...)
	current = append(current,
		model.Section{Title: "Running", Meta: "11"},
		model.Row{Name: "shopfront", Sub: "api, cache, web, worker", Value: "4/4 up · 1.2G · 6%", LED: ok,
			Actions: unit("shopfront", "restart", "stop")},
		model.Row{Name: "metrics", Sub: "grafana, prometheus   down: alertmanager", Value: "2/3 up · 612M · 2%", LED: warn,
			Actions: unit("metrics", "restart", "stop")},
		model.Row{Name: "postgres", Sub: "postgres:16 · :5432 · healthy", Value: "up 12d · 384M", LED: ok,
			Actions: unit("postgres", "restart", "stop")},
		model.Row{Name: "redis", Sub: "redis:7 · 127.0.0.1:6379", Value: "up 12d · 41M", LED: ok,
			Actions: unit("redis", "restart", "stop")},
		model.Section{Title: "Elsewhere"},
		model.Note{Text: "3 stopped · 1 broken — see Stopped"},
	)

	stopped := model.Blocks{
		model.Section{Title: "Broken", Meta: "1 · crashed, not touched since", Tone: crit},
		model.Row{Name: "thumbnailer", Sub: "OOM-killed (137) · restart=unless-stopped but not back", Value: "OOM · 2h ago", LED: crit,
			Actions: unit("thumbnailer", "start", "remove")},
		model.Section{Title: "Stopped", Meta: "1 · this week"},
		model.Row{Name: "docs-preview", Sub: "stopped (SIGTERM)", Value: "2d ago", LED: idle,
			Actions: unit("docs-preview", "start", "remove")},
		model.Section{Title: "Dormant", Meta: "1 · not run in a week"},
		model.Row{Name: "legacy-cron", Sub: "exited cleanly", Value: "5w ago", LED: idle,
			Actions: unit("legacy-cron", "start", "remove")},
	}

	system := model.Blocks{
		model.Section{Title: "Engine"},
		model.Row{Name: "Docker", Value: "27.3.1"},
		model.Row{Name: "Compose", Value: "2.29.7"},
		model.Row{Name: "Containers", Value: "15 · 11 running"},
		model.Row{Name: "Images", Value: "46 · 7 dangling"},
		model.Row{Name: "Volumes", Value: "21 · 14 in use"},
		model.Section{Title: "Disk · reclaimable"},
		model.Row{Name: "images", Sub: "7 dangling · prune drops every image no container uses", Value: "38G of 61G", LED: warn,
			Actions: prune("prune.images", "prune unused", "free 38G?")},
		model.Row{Name: "containers", Sub: "4 stopped · prune removes every one of them", Value: "1.4G of 3.1G", LED: idle,
			Actions: prune("prune.containers", "prune stopped", "remove 4?")},
		model.Row{Name: "build cache", Sub: "rebuilds cost time, not data", Value: "22G of 22G", LED: warn,
			Actions: prune("prune.builder", "prune", "prune?")},
		model.Row{Name: "volumes", Sub: "data lives here · never pruned from this panel", Value: "6.8G of 48G", LED: idle},
		model.Note{Text: "Buttons ask twice. Nothing in use is ever removed."},
	}

	s.Pages = []model.Page{
		{ID: "now", Label: "Now", Badge: badge, Tone: pageTone, Blocks: current},
		{ID: "stopped", Label: "Stopped", Badge: "3", Tone: crit, Blocks: stopped},
		{ID: "system", Label: "System", Blocks: system},
	}
	return s
}

// Variants: the daemon out of reach.
func (Widget) Variants(time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{
		"unavailable": {
			State: model.StateIdle, StateLabel: "daemon unreachable",
			Glance: model.Glance{Text: "off", Tone: model.ToneIdle}, Width: 440,
			Pages: []model.Page{{ID: "now", Blocks: model.Blocks{
				model.Empty{Text: "Docker is not running, or this user cannot reach its socket. Check with `docker info`."},
			}}},
		},
	}
}

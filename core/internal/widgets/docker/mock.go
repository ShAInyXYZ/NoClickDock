package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// Mock invents a Docker host for --mock: the gallery, the README images and
// the design review. It is drawn by the real widget's own render, so what
// is reviewed is what a real host shows. It is its own type so that mock
// mode can never reach the real widget's actions.
type Mock struct{}

func (Mock) Meta() hub.Meta { return meta }

const gib = 1 << 30

// Mock is an invented host: one service failing its health check, one
// OOM-killed two hours ago, a stack running one member short. Every stack and
// container name here is made up. Live, the failing service recovers for
// twenty seconds of every minute, so its toasts can be watched.
func (Mock) Mock(now time.Time) model.Snapshot {
	ill := now.Second() < 40 // the still instant (…:07) is ill
	up := func(name, image string, hours, cpu, mem float64) *container {
		return &container{Name: name, State: "running", Image: image, UpH: hours, AgeH: -1, CPU: cpu, MemB: mem, Policy: "unless-stopped"}
	}
	stack := func(project string, cs ...*container) []*container {
		for _, c := range cs {
			c.Project = project
			c.Service = strings.TrimPrefix(c.Name, project+"-")
		}
		return cs
	}
	stopped := func(name, image string, code int, hours float64) *container {
		return &container{Name: name, State: "exited", Image: image, HasExit: true, Exit: code, AgeH: hours, UpH: -1, Finished: "mock"}
	}

	api := up("ledger-api", "example/ledger-api:2.4", 0.05, 3.1, 0.31*gib)
	api.Log = "3 checks failed · GET /healthz 503 upstream db timeout after 5000 ms"
	if ill {
		api.Health = "unhealthy"
	} else {
		api.Health = "healthy"
	}
	thumbs := stopped("thumbnailer", "example/thumbnailer:0.9", 137, 2)
	thumbs.OOM, thumbs.Crashed, thumbs.Policy = true, true, "unless-stopped"
	thumbs.Log = "Killed: memory cgroup limit 512M reached (rss 511.8M)"
	postgres := up("postgres", "postgres:16", 12*24+5, 0.4, 384<<20)
	postgres.Health, postgres.Ports = "healthy", []string{":5432"}
	redis := up("redis", "redis:7", 12*24+5, 0.2, 41<<20)
	redis.Ports = []string{"127.0.0.1:6379"}
	portainer := up("portainer", "portainer/portainer-ce:2.21", 30*24, 0.1, 22<<20)
	portainer.Ports = []string{":8000", ":9443"}
	alert := stopped("metrics-alertmanager", "prom/alertmanager:0.27", 143, 30)

	var all []*container
	all = append(all, stack("shopfront",
		up("shopfront-web", "example/shop-web:5", 70, 2.4, 0.35*gib),
		up("shopfront-api", "example/shop-api:5", 70, 2.9, 0.48*gib),
		up("shopfront-cache", "redis:7", 70, 0.3, 0.09*gib),
		up("shopfront-worker", "example/shop-worker:5", 70, 0.6, 0.28*gib))...)
	all = append(all, stack("metrics",
		up("metrics-grafana", "grafana/grafana:11", 200, 0.8, 0.21*gib),
		up("metrics-prometheus", "prom/prometheus:2.54", 200, 1.3, 0.39*gib), alert)...)
	all = append(all, api, postgres, redis, portainer, thumbs,
		stopped("docs-preview", "example/docs:latest", 143, 50),
		stopped("legacy-cron", "example/legacy-cron:1", 0, 5*168))

	d := &data{
		available: true, all: all, units: group(all), portainer: "https://localhost:9443",
		sys: &system{engine: "27.3.1", compose: "2.29.7", dangling: 7, df: map[string]dfRow{
			"images":        {total: "46", active: "12", size: 61.3e9, recl: 38.1e9, pct: "62%"},
			"containers":    {total: "15", active: "11", size: 3.1e9, recl: 1.4e9, pct: "45%"},
			"local volumes": {total: "21", active: "14", size: 48.2e9, recl: 6.8e9, pct: "14%"},
			"build cache":   {total: "0", active: "0", size: 22.4e9, recl: 22.4e9, pct: "100%"},
		}},
	}
	return render(d, pending{})
}

// Variants: the daemon out of reach.
func (Mock) Variants(time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{"unavailable": render(&data{}, pending{})}
}

// Act plays the mock's actions out without touching anything: every one
// succeeds, except removing the dormant legacy-cron stack, which is refused
// the way a daemon refuses — so a failed action can be reviewed in place.
func (Mock) Act(_ context.Context, action string, args map[string]string) (string, error) {
	name := args["unit"] + args["container"]
	if action == "unit.remove" && name == "legacy-cron" {
		return "", errors.New("legacy-cron: Error response from daemon: volume is in use by another container")
	}
	scope, verb, _ := strings.Cut(action, ".")
	if scope == "prune" {
		return "Pruned " + map[string]string{"images": "images", "containers": "containers", "builder": "build cache"}[verb], nil
	}
	if past, ok := done[verb]; ok && name != "" {
		return past + " " + name, nil
	}
	return fmt.Sprintf("mock: %s", action), nil
}

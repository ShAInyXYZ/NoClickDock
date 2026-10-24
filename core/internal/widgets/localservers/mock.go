package localservers

import (
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"time"
)

type Mock struct{}

func (Mock) Meta() hub.Meta { return meta }
func (Mock) Mock(time.Time) model.Snapshot {
	all := []server{
		{Key: "4100:1", Name: "studio", Dir: "/home/dev/projects/studio", Runtime: "Vite", Dev: true, Running: true, Unit: "studio-web.service", Proc: process{PID: 4100}, Endpoints: []endpoint{{5173, "127.0.0.1", "1"}}},
		{Key: "4101:1", Name: "studio", Dir: "/home/dev/projects/studio", Runtime: "Uvicorn", Dev: true, Running: true, Proc: process{PID: 4101}, Endpoints: []endpoint{{8000, "127.0.0.1", "2"}}, RestartReason: "Launch arguments, environment and terminal setup are not known. Register a direct launch command."},
		{Key: "4200:1", Name: "notes", Dir: "/home/dev/projects/notes", Runtime: "Next.js", Dev: true, Proc: process{PID: 4200}, Endpoints: []endpoint{{3000, "127.0.0.1", "3"}}},
		{Key: "500:1", Name: "PostgreSQL", Runtime: "postgres", Running: true, Protected: "Infrastructure service", Endpoints: []endpoint{{5432, "127.0.0.1", "4"}}},
	}
	return render(all, false, nil, nil, nil)
}
func (m Mock) Variants(t time.Time) map[string]model.Snapshot {
	empty := render(nil, false, nil, nil, nil)
	busy := m.Mock(t)
	busy.State = model.StateBusy
	busy.StateLabel = "server action in progress"
	busy.Glance.Tone = model.ToneBusy
	failed := render([]server{{Key: "4100:1", Name: "studio", Dir: "/home/dev/projects/studio", Runtime: "Vite", Dev: true, Running: true, Proc: process{PID: 4100}, Endpoints: []endpoint{{5173, "127.0.0.1", "1"}}}}, false, nil, map[string]string{"4100:1": "Process exited or changed; refresh before acting"}, nil)
	return map[string]model.Snapshot{"empty": empty, "unavailable": unavailable("Linux process details are not accessible."), "busy": busy, "action-error": failed}
}

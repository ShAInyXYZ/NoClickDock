package localservers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

var meta = hub.Meta{ID: "localservers", Title: "Local Servers", Order: 55, PollSecs: 10}

type launch struct {
	Directory string   `json:"directory"`
	Argv      []string `json:"argv"`
}
type service struct {
	Unit, Invocation, Group, Active string
	PID                             int
}
type backend interface {
	Scan(context.Context) ([]server, error)
	Verify(server) error
	Stop(context.Context, server) error
	Unit(process) string
	Service(context.Context, string) (service, error)
	Exclusive(server, service) bool
	Control(context.Context, server, string) error
	Launch(context.Context, launch, string) error
	Logs(context.Context, server) (string, error)
}

type Widget struct {
	mu        sync.Mutex
	backend   backend
	dir       string
	last      map[string]server
	history   map[string]server
	pending   map[string]string
	failures  map[string]string
	logText   map[string]string
	showOther bool
	refresh   func()
}

func New(dir string) *Widget {
	return &Widget{backend: newBackend(), dir: filepath.Join(dir, "local-servers"), last: map[string]server{}, history: map[string]server{}, pending: map[string]string{}, failures: map[string]string{}, logText: map[string]string{}}
}
func (*Widget) Meta() hub.Meta        { return meta }
func (w *Widget) SetRefresh(f func()) { w.mu.Lock(); w.refresh = f; w.mu.Unlock() }
func (w *Widget) poke() {
	w.mu.Lock()
	f := w.refresh
	w.mu.Unlock()
	if f != nil {
		f()
	}
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	all, err := w.backend.Scan(ctx)
	if err != nil {
		return unavailable(err.Error()), nil
	}
	for i := range all {
		s := &all[i]
		if !s.Dev || s.Protected != "" {
			continue
		}
		s.Registration = registrationKey(*s)
		s.RestartReason = "Launch arguments, environment and terminal setup are not known. Register a direct launch command."
		if unit := w.backend.Unit(s.Proc); unit != "" {
			info, err := w.backend.Service(ctx, unit)
			if err == nil && w.backend.Exclusive(*s, info) {
				s.Unit = unit
				s.Invocation = info.Invocation
				capturePrivate(&s.Proc)
				s.RestartReason = ""
			} else {
				s.Protected = "Service has shared processes or its manager is unavailable"
			}
		} else if hasSupervisor(s.Proc) {
			s.Protected = "Managed process: use its existing supervisor"
		}
		if s.Unit == "" && s.Protected == "" {
			if _, err := w.spec(*s); err == nil {
				s.RestartReason = ""
			} else if _, exists := os.Stat(filepath.Join(w.dir, s.Registration+".json")); exists == nil {
				s.RestartReason = err.Error()
			}
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	current := map[string]server{}
	occupied := map[string]bool{}
	for _, s := range all {
		current[s.Key] = s
		for _, e := range s.Endpoints {
			occupied[fmt.Sprint(e.Port)] = true
		}
		if s.Dev {
			w.history[s.Key] = s
		}
	}
	for key, old := range w.history {
		if _, ok := current[key]; ok {
			continue
		}
		old.Running = false
		old.RestartReason = "Process exited. Register a launch command to start it again."
		if _, err := w.spec(old); err == nil {
			old.RestartReason = ""
		}
		// Keep the old process separate when another process takes its port.
		for _, e := range old.Endpoints {
			if occupied[fmt.Sprint(e.Port)] {
				old.RestartReason = "Port is now in use; the new process is a separate server"
			}
		}
		current[key] = old
	}
	// Bound stopped history; running entries are never evicted.
	if len(w.history) > 128 {
		for key := range w.history {
			if s := current[key]; !s.Running && w.pending[key] == "" {
				delete(w.history, key)
				delete(current, key)
				delete(w.failures, key)
				delete(w.logText, key)
				if len(w.history) <= 128 {
					break
				}
			}
		}
	}
	w.last = current
	rows := make([]server, 0, len(current))
	for _, s := range current {
		rows = append(rows, s)
	}
	return render(rows, w.showOther, w.pending, w.failures, w.logText), nil
}

func hasSupervisor(p process) bool {
	// Parent metadata is inspected on Linux by the backend-specific helper.
	return supervised(p)
}
func (w *Widget) spec(s server) (launch, error) {
	if s.Registration == "" {
		return launch{}, errors.New("No registered launch")
	}
	l, err := readLaunch(filepath.Join(w.dir, s.Registration+".json"))
	if err != nil {
		return l, err
	}
	if filepath.Clean(l.Directory) != filepath.Clean(s.Proc.Cwd) {
		return l, errors.New("Launch directory must match the discovered process directory")
	}
	return l, nil
}
func (w *Widget) Act(ctx context.Context, action string, args map[string]string) (message string, err error) {
	if action == "refresh" {
		w.poke()
		return "Refreshing local servers", nil
	}
	if action == "other" {
		w.mu.Lock()
		w.showOther = !w.showOther
		w.mu.Unlock()
		w.poke()
		return "Updated service visibility", nil
	}
	w.mu.Lock()
	s, ok := w.last[args["server"]]
	if !ok {
		w.mu.Unlock()
		return "", errors.New("Server is no longer in this snapshot; refresh")
	}
	busy := w.pending[s.Key] != ""
	for key := range w.pending {
		if other, exists := w.last[key]; exists && s.Registration != "" && other.Registration == s.Registration {
			busy = true
		}
	}
	if busy {
		w.mu.Unlock()
		return "", errors.New("An action is already running for this server")
	}
	if action != "stop" && action != "restart" && action != "register" && action != "logs" {
		w.mu.Unlock()
		return "", errors.New("Unsupported server action")
	}
	if s.Protected != "" || !s.Dev {
		w.mu.Unlock()
		return "", errors.New("This service is protected")
	}
	w.pending[s.Key] = map[string]string{"stop": "stopping", "restart": "restarting", "register": "registering", "logs": "reading logs"}[action]
	delete(w.failures, s.Key)
	w.mu.Unlock()
	w.poke()
	defer func() {
		w.mu.Lock()
		delete(w.pending, s.Key)
		if err != nil {
			w.failures[s.Key] = err.Error()
		}
		w.mu.Unlock()
		w.poke()
	}()
	if s.Running {
		if err = w.backend.Verify(s); err != nil {
			return "", err
		}
	}
	switch action {
	case "register":
		path := filepath.Join(w.dir, s.Registration+".json")
		if err = os.MkdirAll(w.dir, 0o700); err != nil {
			return "", errors.New("Cannot create the private launch directory")
		}
		file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if e == nil {
			b, _ := json.MarshalIndent(launch{Directory: s.Proc.Cwd, Argv: []string{}}, "", "  ")
			_, e = file.Write(append(b, '\n'))
			file.Close()
		}
		if e != nil && !os.IsExist(e) {
			return "", errors.New("Cannot write the launch template")
		}
		if e = privateLaunchFile(path); e != nil {
			return "", errors.New("Launch template must remain a private, user-owned regular file (0600)")
		}
		// Open only a backend-chosen private file, never a path supplied by the UI.
		if e = sys.Start("xdg-open", path); e != nil {
			return "", fmt.Errorf("Edit %s with an absolute executable and arguments, then refresh", safeText(path))
		}
		return "Opened launch file; fill argv, save, then refresh", nil
	case "logs":
		if s.Unit == "" {
			return "", errors.New("No process-manager logs are available")
		}
		text, e := w.backend.Logs(ctx, s)
		if e != nil {
			return "", e
		}
		w.mu.Lock()
		w.logText[s.Key] = text
		w.mu.Unlock()
		return "Loaded service logs", nil
	case "stop":
		if !s.Running {
			return "", errors.New("Process already exited")
		}
		if s.Unit != "" {
			err = w.backend.Control(ctx, s, "stop")
		} else {
			err = w.backend.Stop(ctx, s)
		}
		if err != nil {
			return "", err
		}
		return "Stopped server", nil
	case "restart":
		if s.RestartReason != "" {
			return "", errors.New(s.RestartReason)
		}
		if s.Running && s.Unit != "" {
			if err = w.backend.Control(ctx, s, "restart"); err != nil {
				return "", err
			}
			return w.awaitListener(ctx, s, s.Unit)
		}
		spec, e := w.spec(s)
		if e != nil {
			return "", e
		}
		// Resolve and validate the complete registered launch BEFORE stopping anything.
		unit := "ncd-dev-" + s.Registration + ".service"
		if _, e = w.backend.Service(ctx, unit); e != nil {
			return "", e
		}
		if s.Running {
			if err = w.backend.Stop(ctx, s); err != nil {
				return "", err
			}
		}
		all, e := w.backend.Scan(ctx)
		if e != nil {
			return "", e
		}
		for _, a := range all {
			for _, ep := range a.Endpoints {
				for _, old := range s.Endpoints {
					if ep.Port == old.Port {
						return "", errors.New("Port is occupied; no new process was launched")
					}
				}
			}
		}
		if err = w.backend.Launch(ctx, spec, unit); err != nil {
			return "", err
		}
		return w.awaitListener(ctx, s, unit)
	}
	return "", errors.New("Unsupported action")
}
func (w *Widget) awaitListener(ctx context.Context, old server, unit string) (string, error) {
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		all, err := w.backend.Scan(ctx)
		if err != nil {
			return "", err
		}
		for _, s := range all {
			if w.backend.Unit(s.Proc) == unit && s.Key != old.Key {
				for _, ep := range s.Endpoints {
					for _, want := range old.Endpoints {
						if ep.Port == want.Port {
							return "Restarted server", nil
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("Launch requested, but the server has not listened on its previous port within 12 seconds; refresh and check its user journal")
		case <-tick.C:
		}
	}
}
func sortServers(all []server) {
	sort.Slice(all, func(i, j int) bool {
		if all[i].Dir != all[j].Dir {
			return all[i].Dir < all[j].Dir
		}
		return strings.Compare(all[i].Key, all[j].Key) < 0
	})
}

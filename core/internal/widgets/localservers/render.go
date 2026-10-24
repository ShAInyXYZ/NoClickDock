package localservers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

func action(id, label, key string) model.Action {
	return model.Action{ID: id, Label: label, Args: map[string]string{"server": key}}
}
func control(id, label, key string) model.Action {
	a := action(id, label, key)
	a.Confirm = label + "?"
	a.Danger = id == "stop"
	return a
}
func render(all []server, other bool, pending, failures, logs map[string]string) model.Snapshot {
	sortServers(all)
	s := model.Snapshot{Width: 440, State: model.StateOK, Footer: "local TCP listeners · checked every 10 s"}
	now, stopped := model.Page{ID: "now", Label: "Now"}, model.Page{ID: "stopped", Label: "Stopped"}
	now.Blocks = append(now.Blocks, model.Row{Name: "Development servers", Actions: []model.Action{{ID: "refresh", Label: "refresh"}}})
	running, hidden := 0, 0
	groups := map[string]string{}
	icons := map[string]string{}
	for _, a := range all {
		if a.Icon != "" && icons[a.Dir] == "" {
			icons[a.Dir] = a.Icon
		}
	}
	for _, a := range all {
		if a.Running && a.Dev {
			running++
		}
		if !a.Dev && !other {
			hidden++
			continue
		}
		page := &now
		if !a.Running {
			page = &stopped
		}
		group := a.Dir
		if group == "" {
			group = "Other services"
		}
		if groups[page.ID] != group {
			title := safeText(a.Name)
			if !a.Dev {
				title = "Other services · " + title
			}
			page.Blocks = append(page.Blocks, model.Section{Title: title, Icon: icons[a.Dir], Initials: projectInitials(a.Name)})
			if a.Dir != "" {
				page.Blocks = append(page.Blocks, model.Row{Name: safeText(a.Dir), Mono: true})
			}
			groups[page.ID] = group
		}
		status, led := "running", model.ToneOK
		if !a.Running {
			status = "stopped"
			led = model.ToneIdle
		}
		if verb := pending[a.Key]; verb != "" {
			status = verb + "…"
			led = model.ToneBusy
			s.State = model.StateBusy
		}
		var controls []model.Action
		if a.Dev && a.Protected == "" {
			if a.RestartReason == "" {
				label := "restart"
				if !a.Running {
					label = "start"
				}
				controls = append(controls, control("restart", label, a.Key))
			}
			if a.Running {
				controls = append(controls, control("stop", "stop", a.Key))
			}
		}
		port := ""
		for _, e := range a.Endpoints {
			if port != "" {
				port += ", "
			}
			port += ":" + strconv.Itoa(e.Port)
		}
		sub := safeText(a.Runtime)
		if a.Proc.PID > 0 {
			sub += fmt.Sprintf(" · PID %d", a.Proc.PID)
		}
		if a.Protected != "" {
			sub += " · protected"
		}
		// The address is the server identity and its browser shortcut. Keep one
		// row per endpoint; process controls occur only on the first endpoint.
		if a.Running {
			for i, e := range a.Endpoints {
				id := a.Key + ":" + strconv.Itoa(e.Port)
				open, copy := "open:"+id, "copy:"+id
				s.Menu = append(s.Menu, model.MenuItem{ID: open, Label: "Open " + safeText(a.Name) + " :" + strconv.Itoa(e.Port), URL: e.URL()}, model.MenuItem{ID: copy, Label: "Copy local URL", Copy: e.URL()})
				buttons := []model.Action{{ID: copy, Label: "copy"}}
				if i == 0 {
					buttons = append(buttons, controls...)
				}
				page.Blocks = append(page.Blocks, model.Row{Name: strings.TrimPrefix(e.URL(), "http://"), Sub: sub, Value: status, LED: led, NameAction: &model.Action{ID: open, Label: "Open " + e.URL()}, Actions: buttons})
			}
		} else {
			page.Blocks = append(page.Blocks, model.Row{Name: port, Sub: sub, Value: status, LED: led, Actions: controls})
		}
		if msg := failures[a.Key]; msg != "" {
			page.Blocks = append(page.Blocks, model.Note{Text: msg, Tone: model.ToneCrit})
			s.State = model.StateWarn
		}
		if a.Protected != "" {
			page.Blocks = append(page.Blocks, model.Note{Text: a.Protected})
		} else if a.Dev {
			if a.Unit != "" {
				if a.RestartReason != "" {
					s.Settings = append(s.Settings, model.Section{Title: safeText(a.Name) + " · " + port}, model.Note{Text: a.RestartReason})
				}
				page.Blocks = append(page.Blocks, model.Row{Name: "User service", Sub: safeText(a.Unit), Actions: []model.Action{action("logs", "logs", a.Key)}})
			} else {
				// Registration is configuration, not part of the running preview.
				label := "not registered"
				if a.RestartReason == "" {
					label = "registered · clean environment"
				}
				s.Settings = append(s.Settings,
					model.Section{Title: safeText(a.Name) + " · " + port},
					model.Row{Name: safeText(a.Dir), Mono: true},
					model.Row{Name: "Launch command", Sub: label, Actions: []model.Action{control("register", "edit launch", a.Key)}},
				)
				if a.RestartReason != "" {
					s.Settings = append(s.Settings, model.Note{Text: a.RestartReason})
				}
			}
		}
		if text := logs[a.Key]; text != "" {
			for _, line := range strings.Split(text, "\n") {
				page.Blocks = append(page.Blocks, model.Note{Text: line, Mono: true})
			}
		}
	}
	if running == 0 {
		now.Blocks = append(now.Blocks, model.Empty{Text: "No development servers are listening. Start a project, then refresh."})
	}
	label := "Show other services"
	if other {
		label = "Hide other services"
	}
	now.Blocks = append(now.Blocks, model.Row{Name: label, Sub: fmt.Sprintf("%d hidden · infrastructure is read-only", hidden), Actions: []model.Action{{ID: "other", Label: map[bool]string{true: "hide", false: "show"}[other]}}})
	if len(stopped.Blocks) == 0 {
		stopped.Blocks = append(stopped.Blocks, model.Empty{Text: "No development servers have stopped since NCD started."})
	}
	if len(s.Settings) == 0 {
		s.Settings = model.Blocks{model.Empty{Text: "No servers need a launch command. Servers managed by a user service use their existing configuration."}}
	}
	s.StateLabel = fmt.Sprintf("%d running", running)
	s.Glance = model.Glance{Text: strconv.Itoa(running)}
	if s.State == model.StateBusy {
		s.StateLabel = "server action in progress"
		s.Glance.Tone = model.ToneBusy
	}
	if s.State == model.StateWarn {
		s.StateLabel = "server action failed"
		s.Glance.Tone = model.ToneWarn
	}
	if running == 0 && s.State == model.StateOK {
		s.State = model.StateIdle
	}
	s.Pages = []model.Page{now, stopped}
	return s
}
func unavailable(reason string) model.Snapshot {
	return model.Snapshot{Settings: model.Blocks{model.Empty{Text: strings.TrimSpace(reason)}}, Width: 440, State: model.StateIdle, StateLabel: "discovery unavailable", Glance: model.Glance{Text: "—", Tone: model.ToneIdle}, Pages: []model.Page{{ID: "now", Blocks: model.Blocks{model.Empty{Text: strings.TrimSpace(reason)}, model.Row{Name: "Local Servers", Actions: []model.Action{{ID: "refresh", Label: "refresh"}}}}}}, Footer: "retrying every 10 s"}
}

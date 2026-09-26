// Package statuspage reads a public Statuspage v2 API (status.claude.com,
// and status.openai.com's incident.io mirror of it) and says what it means
// in the panel's words: which services are broken, the incidents, a state
// label that names the fact. No credentials are involved: the pages are
// public.
package statuspage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	requestTimeout = 10 * time.Second
	maxBody        = 4 << 20
)

// Page is one read of a status page. Reached is false when status.json did
// not answer; a failure after it keeps what was already read.
type Page struct {
	Reached     bool
	Indicator   string // none minor major critical; "unknown" when not reached
	Description string
	Components  []Component
	Incidents   []Incident
}

type Component struct {
	Name   string
	Status string // operational degraded_performance partial_outage major_outage under_maintenance
}

type Incident struct {
	ID        string
	Name      string
	Status    string // investigating identified monitoring …
	Impact    string // none minor major critical
	UpdatedAt time.Time
	Latest    Update
}

type Update struct {
	Body      string
	CreatedAt time.Time
}

// Source is one status page.
type Source struct {
	Base      string // "https://status.claude.com"
	UserAgent string
	// Incidents is the path of the incident list: Statuspage's
	// unresolved.json, or the full list where there is none (incident.io).
	Incidents string
	Client    *http.Client
}

// Fetch reads status, components and incidents, in that order. It never
// fails: what could not be read is left out, and a page that did not answer
// at all reads as unknown.
func (s Source) Fetch(ctx context.Context) Page {
	p := Page{Indicator: "unknown", Description: "Unable to reach status page"}
	var status struct {
		Status struct{ Indicator, Description string }
	}
	if s.get(ctx, "/api/v2/status.json", &status) != nil || status.Status.Indicator == "" {
		return p
	}
	p.Reached, p.Indicator, p.Description = true, status.Status.Indicator, status.Status.Description

	var comps struct {
		Components []struct {
			Name   string
			Status string
			Group  bool
		}
	}
	if s.get(ctx, "/api/v2/components.json", &comps) == nil {
		for _, c := range comps.Components {
			if c.Group || c.Name == "" { // a group container is not a service
				continue
			}
			p.Components = append(p.Components, Component{Name: c.Name, Status: c.Status})
		}
	}

	var incs struct {
		Incidents []struct {
			ID, Name, Status, Impact string
			UpdatedAt                string `json:"updated_at"`
			Updates                  []struct {
				Body      string
				CreatedAt string `json:"created_at"`
			} `json:"incident_updates"`
		}
	}
	if s.get(ctx, s.Incidents, &incs) == nil {
		for _, i := range incs.Incidents {
			// incident.io's mirror lists every incident: keep the open ones.
			if i.Status == "resolved" || i.Status == "postmortem" || i.ID == "" {
				continue
			}
			inc := Incident{ID: i.ID, Name: i.Name, Status: i.Status, Impact: i.Impact, UpdatedAt: instant(i.UpdatedAt)}
			if len(i.Updates) > 0 {
				inc.Latest = Update{Body: i.Updates[0].Body, CreatedAt: instant(i.Updates[0].CreatedAt)}
			}
			p.Incidents = append(p.Incidents, inc)
		}
	}
	return p
}

// instant reads a timestamp; anything unreadable is unknown (zero).
func instant(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s Source) get(ctx context.Context, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(s.Base, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/json")
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v)
}

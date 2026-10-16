package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const recent = 7 * 24 * time.Hour

// rateLimits is one token_count record's view of the account's allowance.
// An account can carry several at once (a general one, a per-model one),
// each with its own id, windows and reset clocks.
type rateLimits struct {
	LimitID   string  `json:"limit_id"`
	LimitName string  `json:"limit_name"`
	PlanType  string  `json:"plan_type"`
	Primary   *window `json:"primary"`
	Secondary *window `json:"secondary"`
}

type window struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *int     `json:"window_minutes"`
	ResetsAt      *float64 `json:"resets_at"` // epoch seconds
}

type sample struct {
	at time.Time
	rl rateLimits
}

type cturn struct {
	at    time.Time
	total int64
	model string
}

// fileData is one rollout's contribution, kept until the file changes.
type fileData struct {
	size    int64
	mtime   time.Time
	session string
	turns   []cturn
	newest  map[string]sample // per limit id
	live    map[string]bool   // limit ids seen above 0 % in this file
}

// scanner reads Codex's rollouts, parsing only the files that changed.
type scanner struct {
	roots []string
	files map[string]*fileData
}

func newScanner(roots ...string) *scanner {
	return &scanner{roots: roots, files: map[string]*fileData{}}
}

// rolloutDirs are ~/.codex/sessions and archived_sessions (CODEX_HOME if
// set; %USERPROFILE%\.codex on Windows).
func rolloutDirs() []string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		home = filepath.Join(h, ".codex")
	}
	return []string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")}
}

// spend is local usage and the account's limits as the newest rollouts
// recorded them — purely local, no API key, no account call.
type spend struct {
	H1, H24, D7 int64
	Sessions    int
	Models      []modelSpend
	Available   bool
	Plan        string
	Seen        time.Time // the newest limits reading
	Groups      []group
}

type modelSpend struct {
	Model  string
	Tokens int64
}

// group is one allowance: its windows, shortest first.
type group struct {
	ID, Name string
	Seen     time.Time
	Rows     []row
	Idle     bool // never above 0 %: a bucket the account reports but never fills
}

type row struct {
	Minutes int // -1 unknown
	Pct     float64
	Resets  time.Time
}

func (s *scanner) scan(now time.Time) spend {
	seen := map[string]bool{}
	for _, root := range s.roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil || now.Sub(info.ModTime()) > recent {
				return nil
			}
			seen[path] = true
			if f := s.files[path]; f != nil && f.size == info.Size() && f.mtime.Equal(info.ModTime()) {
				return nil
			}
			f := readRollout(path)
			f.size, f.mtime = info.Size(), info.ModTime()
			s.files[path] = f
			return nil
		})
	}
	for path := range s.files {
		if !seen[path] {
			delete(s.files, path)
		}
	}
	return s.total(now)
}

func (s *scanner) total(now time.Time) spend {
	var out spend
	sessions := map[string]bool{}
	models := map[string]int64{}
	newest := map[string]sample{}
	live := map[string]bool{}
	for _, f := range s.files {
		for _, t := range f.turns {
			age := max(now.Sub(t.at), 0)
			if age >= recent {
				continue
			}
			out.D7 += t.total
			if age < time.Hour {
				out.H1 += t.total
			}
			if age < 24*time.Hour {
				out.H24 += t.total
				if f.session != "" {
					sessions[f.session] = true
				}
				m := t.model
				if m == "" {
					m = "unknown"
				}
				models[m] += t.total
			}
		}
		for id, smp := range f.newest {
			if n, ok := newest[id]; !ok || smp.at.After(n.at) {
				newest[id] = smp
			}
		}
		for id := range f.live {
			live[id] = true
		}
	}
	out.Sessions = len(sessions)
	for m, n := range models {
		out.Models = append(out.Models, modelSpend{m, n})
	}
	sort.Slice(out.Models, func(i, j int) bool {
		if out.Models[i].Tokens != out.Models[j].Tokens {
			return out.Models[i].Tokens > out.Models[j].Tokens
		}
		return out.Models[i].Model < out.Models[j].Model
	})
	out.Models = out.Models[:min(len(out.Models), 3)]
	out.Available = out.D7 > 0

	for id, smp := range newest {
		g := group{ID: id, Name: smp.rl.LimitName, Seen: smp.at}
		for _, w := range []*window{smp.rl.Primary, smp.rl.Secondary} {
			if w == nil || w.UsedPercent == nil {
				continue
			}
			r := row{Minutes: -1, Pct: *w.UsedPercent}
			if w.WindowMinutes != nil {
				r.Minutes = *w.WindowMinutes
			}
			if w.ResetsAt != nil && *w.ResetsAt > 0 {
				r.Resets = time.Unix(int64(*w.ResetsAt), 0)
			}
			g.Rows = append(g.Rows, r)
		}
		if len(g.Rows) == 0 {
			continue
		}
		sort.SliceStable(g.Rows, func(i, j int) bool { return minutes(g.Rows[i]) < minutes(g.Rows[j]) })
		g.Idle = !live[id]
		for _, r := range g.Rows {
			g.Idle = g.Idle && r.Pct == 0
		}
		out.Groups = append(out.Groups, g)
		if smp.at.After(out.Seen) {
			out.Seen = smp.at
			out.Plan = smp.rl.PlanType // the newest bucket's, deterministically
		}
	}
	// Real usage leads; a bucket that never moves sinks; newest first.
	sort.SliceStable(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.Idle != b.Idle {
			return !a.Idle
		}
		if !a.Seen.Equal(b.Seen) {
			return a.Seen.After(b.Seen)
		}
		return a.ID < b.ID
	})
	return out
}

func minutes(r row) int {
	if r.Minutes < 0 {
		return 1 << 30
	}
	return r.Minutes
}

// record is the part of a rollout line the totals need.
type record struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type           string      `json:"type"`
		ID             string      `json:"id"`
		SessionID      string      `json:"session_id"`
		ParentThreadID string      `json:"parent_thread_id"`
		ForkedFromID   string      `json:"forked_from_id"`
		Model          string      `json:"model"`
		RateLimits     *rateLimits `json:"rate_limits"`
		Info           *struct {
			Last *struct {
				Total int64 `json:"total_tokens"`
			} `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

var (
	markMeta  = []byte(`"session_meta"`)
	markTurn  = []byte(`"turn_context"`)
	markCount = []byte(`"token_count"`)
)

// readRollout streams one rollout. History replayed under a single
// timestamp is dropped (a deliberate under-count: its real times are lost,
// and counting it would charge a whole session to one moment, or count it
// twice):
//   - the same timestamp on several turns (a resumed session rewrote its
//     history);
//   - any turn in the same second as a session_meta record: a fork replays
//     its parent's turns at the moment of the fork, and a resume replays at
//     the moment it starts — even a single turn, which the first rule
//     cannot see (research M4 r1). A real turn never completes within a
//     second of its session starting.
func readRollout(path string) *fileData {
	d := &fileData{newest: map[string]sample{}, live: map[string]bool{}}
	f, err := os.Open(path)
	if err != nil {
		return d
	}
	defer f.Close()

	type pending struct {
		ts    string
		total int64
		model string
	}
	var turns []pending
	count := map[string]int{}
	var model string
	metaSeen := false
	metaSeconds := map[string]bool{}

	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			switch {
			case bytes.Contains(line, markMeta):
				var rec record
				if json.Unmarshal(line, &rec) == nil && rec.Type == "session_meta" {
					if !metaSeen {
						metaSeen = true
						d.session = rec.Payload.SessionID
						if d.session == "" {
							d.session = rec.Payload.ID
						}
					}
					if rec.Timestamp != "" {
						metaSeconds[second(rec.Timestamp)] = true
					}
				}
			case bytes.Contains(line, markTurn):
				var rec record
				if json.Unmarshal(line, &rec) == nil && rec.Payload.Model != "" {
					model = rec.Payload.Model // until the next turn_context
				}
			case bytes.Contains(line, markCount):
				var rec record
				if json.Unmarshal(line, &rec) != nil || rec.Payload.Type != "token_count" {
					break
				}
				if rl := rec.Payload.RateLimits; rl != nil && rl.Primary != nil && rec.Timestamp != "" {
					if at, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
						// Parsed instants, not strings: ".9Z" is before ".15Z".
						if n, ok := d.newest[rl.LimitID]; !ok || at.After(n.at) {
							d.newest[rl.LimitID] = sample{at: at, rl: *rl}
						}
					}
					for _, w := range []*window{rl.Primary, rl.Secondary} {
						if w != nil && w.UsedPercent != nil && *w.UsedPercent > 0 {
							d.live[rl.LimitID] = true
						}
					}
				}
				if info := rec.Payload.Info; info != nil && info.Last != nil && info.Last.Total > 0 && rec.Timestamp != "" {
					turns = append(turns, pending{rec.Timestamp, info.Last.Total, model})
					count[rec.Timestamp]++
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				return d
			}
			break
		}
	}
	for _, t := range turns {
		if count[t.ts] > 1 || metaSeconds[second(t.ts)] {
			continue // replayed history
		}
		at, err := time.Parse(time.RFC3339Nano, t.ts)
		if err != nil {
			continue
		}
		d.turns = append(d.turns, cturn{at: at, total: t.total, model: t.model})
	}
	return d
}

// second is a timestamp to the second: "2026-09-26T10:00:00".
func second(ts string) string {
	if len(ts) < 19 {
		return ts
	}
	return ts[:19]
}

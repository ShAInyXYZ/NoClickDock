package claude

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Claude Code persists the same numeric quota observations used by /usage.
// Read only that cache and the account identity; never execute a model prompt,
// decode credentials, or derive a plan percentage from transcript token counts.
// Schema verified against Claude Code 2.1.303. Unknown fields are ignored.
type limit struct {
	Label  string
	Pct    float64
	Resets time.Time
}

const limitsMaxAge = time.Hour // Claude Code's own persisted-cache TTL
const maxConfigBytes = 8 << 20
const limitsMissing = "Plan limits unavailable · open /usage in an updated Claude Code."
const limitsStale = "Plan limits are stale · open /usage in Claude Code to refresh."

type limitsReading struct {
	rows []limit
	at   time.Time
	note string
	keep bool // transient read failure may keep a still-current last observation
}

// The global file lives beside .claude by default, but inside an explicit
// CLAUDE_CONFIG_DIR. UserHomeDir also handles USERPROFILE on Windows. Do not
// fall back to a different account's default directory when an override exists.
func limitsPath() string {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		var err error
		root, err = os.UserHomeDir()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(root, ".claude.json")
}

func readLimits(path string, now time.Time) limitsReading {
	failed := limitsReading{note: "Plan cache could not be read · retry with Refresh now.", keep: true}
	st, err := os.Stat(path)
	if os.IsNotExist(err) || path == "" {
		return limitsReading{note: limitsMissing}
	}
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxConfigBytes {
		return failed
	}
	f, err := os.Open(path)
	if err != nil {
		return failed
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(data) > maxConfigBytes {
		return failed
	}
	return parseLimits(data, now)
}

func parseLimits(data []byte, now time.Time) limitsReading {
	var config struct {
		Account struct {
			UUID string `json:"accountUuid"`
		} `json:"oauthAccount"`
		Cache *struct {
			FetchedAt int64                      `json:"fetchedAtMs"`
			Account   string                     `json:"accountUuid"`
			Windows   map[string]json.RawMessage `json:"utilization"`
		} `json:"cachedUsageUtilization"`
	}
	if json.Unmarshal(data, &config) != nil {
		// Claude may be replacing the file. Never include parser errors or raw
		// configuration in a snapshot or log: unrelated fields can be secrets.
		return limitsReading{note: "Plan cache could not be read · retry with Refresh now.", keep: true}
	}
	c := config.Cache
	if c == nil {
		return limitsReading{note: limitsMissing}
	}
	if c.Account != config.Account.UUID {
		return limitsReading{note: "Plan cache belongs to another login · open /usage in Claude Code."}
	}
	at := time.UnixMilli(c.FetchedAt)
	if c.FetchedAt <= 0 || at.After(now) || now.Sub(at) > limitsMaxAge {
		return limitsReading{note: limitsStale}
	}
	r := limitsReading{at: at}
	for _, scope := range []struct{ key, label string }{
		{"five_hour", "Session"}, {"seven_day", "Week"},
		{"seven_day_opus", "Week · Opus"}, {"seven_day_sonnet", "Week · Sonnet"},
		{"seven_day_oauth_apps", "Week · OAuth apps"}, {"seven_day_cowork", "Week · Cowork"},
	} {
		var window struct {
			Pct    *float64 `json:"utilization"`
			Resets *string  `json:"resets_at"`
		}
		if json.Unmarshal(c.Windows[scope.key], &window) != nil || window.Pct == nil || *window.Pct < 0 || *window.Pct > 100 {
			continue
		}
		row := limit{Label: scope.label, Pct: *window.Pct}
		if window.Resets != nil {
			var err error
			row.Resets, err = time.Parse(time.RFC3339Nano, *window.Resets)
			if err != nil {
				continue
			}
		}
		r.rows = append(r.rows, row)
	}
	if len(r.rows) == 0 {
		r.note = limitsMissing
	}
	return r
}

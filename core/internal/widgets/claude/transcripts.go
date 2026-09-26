package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const window = 7 * 24 * time.Hour

// usage is local token spend from Claude Code's transcripts — counts only,
// never a percentage of a limit (only the CLI knows the limit).
type usage struct {
	H1, H24, D7 int64
	Sessions    int // in the last 24 h
	Models      []spend
	Available   bool // anything in the last 7 days
}

type spend struct {
	Model  string
	Tokens int64
}

// turn is one assistant message's usage, as written in a transcript.
type turn struct {
	key     string // message id + request id: one message, whatever its line count
	at      time.Time
	total   int64
	output  int64
	model   string
	session string
}

// fileTurns is one transcript's turns, kept until the file changes.
type fileTurns struct {
	size  int64
	mtime time.Time
	turns []turn
}

// scanner reads ~/.claude/projects — every transcript at any depth, the
// subagents' included — parsing only files that changed since last time.
type scanner struct {
	root  string
	files map[string]*fileTurns
}

func newScanner(root string) *scanner {
	return &scanner{root: root, files: map[string]*fileTurns{}}
}

// projectsDir is where Claude Code keeps its transcripts: CLAUDE_CONFIG_DIR,
// else ~/.claude (%USERPROFILE%\.claude on Windows).
func projectsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// scan totals the last 7 days as of now. Claude Code writes one line per
// content block of a streamed message, each repeating the message's id,
// request id and usage: those count once (the Python widget counted each
// line, ~2.7× too much). Never fails: what cannot be read is left out.
func (s *scanner) scan(now time.Time) usage {
	seen := map[string]bool{}
	if s.root != "" {
		_ = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil || now.Sub(info.ModTime()) > window {
				return nil
			}
			seen[path] = true
			if f := s.files[path]; f != nil && f.size == info.Size() && f.mtime.Equal(info.ModTime()) {
				return nil // unchanged since the last scan
			}
			s.files[path] = &fileTurns{size: info.Size(), mtime: info.ModTime(), turns: readTranscript(path)}
			return nil
		})
	}
	for path := range s.files {
		if !seen[path] {
			delete(s.files, path)
		}
	}

	// One entry per message across every file (a resumed session copies
	// history into its new transcript): the line with the most output wins.
	best := map[string]turn{}
	for _, f := range s.files {
		for _, t := range f.turns {
			if b, ok := best[t.key]; !ok || t.output > b.output {
				best[t.key] = t
			}
		}
	}
	return total(best, now)
}

func total(turns map[string]turn, now time.Time) usage {
	var u usage
	sessions := map[string]bool{}
	models := map[string]int64{}
	for _, t := range turns {
		age := max(now.Sub(t.at), 0)
		if age >= window {
			continue
		}
		u.D7 += t.total
		if age < time.Hour {
			u.H1 += t.total
		}
		if age < 24*time.Hour {
			u.H24 += t.total
			if t.session != "" {
				sessions[t.session] = true
			}
			if !strings.HasPrefix(t.model, "<") { // <synthetic> is not a model
				models[t.model] += t.total
			}
		}
	}
	u.Sessions = len(sessions)
	for m, n := range models {
		u.Models = append(u.Models, spend{m, n})
	}
	sort.Slice(u.Models, func(i, j int) bool {
		if u.Models[i].Tokens != u.Models[j].Tokens {
			return u.Models[i].Tokens > u.Models[j].Tokens
		}
		return u.Models[i].Model < u.Models[j].Model
	})
	u.Models = u.Models[:min(len(u.Models), 3)]
	u.Available = u.D7 > 0
	return u
}

// record is the part of a transcript line the totals need.
type record struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	UUID      string `json:"uuid"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var usageMark = []byte(`"usage"`)

// readTranscript streams one file: lines of any length, a line without
// "usage" skipped before any JSON is decoded.
func readTranscript(path string) []turn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []turn
	r := bufio.NewReaderSize(f, 256<<10)
	for n := 0; ; n++ {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && bytes.Contains(line, usageMark) {
			if t, ok := parseLine(line, path, n); ok {
				out = append(out, t)
			}
		}
		if err != nil {
			if err != io.EOF {
				return out
			}
			break
		}
	}
	return out
}

func parseLine(line []byte, path string, n int) (turn, bool) {
	var rec record
	if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" || rec.Message == nil || rec.Message.Usage == nil {
		return turn{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return turn{}, false
	}
	u := rec.Message.Usage
	t := turn{
		at: at, total: u.Input + u.Output + u.CacheCreation + u.CacheRead, output: u.Output,
		model: rec.Message.Model, session: rec.SessionID,
	}
	if t.total == 0 {
		return turn{}, false
	}
	if t.model == "" {
		t.model = "unknown"
	}
	switch {
	case rec.Message.ID != "" || rec.RequestID != "":
		t.key = rec.Message.ID + "|" + rec.RequestID
	case rec.UUID != "":
		t.key = "uuid:" + rec.UUID
	default:
		t.key = path + ":" + strconv.Itoa(n) // nothing to recognise it by: itself
	}
	return t, true
}

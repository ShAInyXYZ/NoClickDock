package disk

import (
	"context"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

const (
	// `docker system df` walks the whole graph: ask rarely, and never on the
	// poll's own time.
	dockerRefresh = 5 * time.Minute
	dockerBudget  = 40 * time.Second

	// Above these the reclaimable space is worth acting on (decimal, as
	// Docker counts).
	dockerNoteworthy = 20e9
	dockerRowHeavy   = 10e9
)

// dockerSpace is what `docker system df` says could be reclaimed.
type dockerSpace struct {
	total uint64
	rows  []dockerRow
	root  string // where the space would come back
}

type dockerRow struct {
	kind string // "Images", "Containers", "Local Volumes", "Build Cache"
	size uint64
}

// dockerSpace returns the last answer and, when it is older than five
// minutes, asks again in the background; the widget is polled again as
// soon as the new answer lands. Nil means no Docker section: Docker is not
// installed or not reachable, which is not an error for a disk widget.
func (w *Widget) dockerSpace() *dockerSpace {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dockerBusy || (!w.dockerAt.IsZero() && time.Since(w.dockerAt) < dockerRefresh) {
		return w.docker
	}
	w.dockerBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), dockerBudget)
		defer cancel()
		d := queryDocker(ctx)
		w.mu.Lock()
		w.docker, w.dockerAt, w.dockerBusy = d, time.Now(), false
		refresh := w.refresh
		w.mu.Unlock()
		if refresh != nil {
			refresh()
		}
	}()
	return w.docker
}

func queryDocker(ctx context.Context) *dockerSpace {
	out, err := sys.Run(ctx, 25*time.Second, "docker", "system", "df", "--format", "{{.Type}}\t{{.Reclaimable}}")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	d := parseDockerDF(out)
	d.root = "/"
	if runtime.GOOS == "windows" {
		// Docker Desktop's data directory is a path inside its VM.
		d.root = "Docker Desktop's disk image"
	} else if root, err := sys.Run(ctx, 10*time.Second, "docker", "info", "--format", "{{.DockerRootDir}}"); err == nil && strings.TrimSpace(root) != "" {
		d.root = strings.TrimSpace(root)
	}
	return d
}

// parseDockerDF reads `docker system df --format "{{.Type}}\t{{.Reclaimable}}"`.
// Reclaimable reads like "83.54GB (18%)"; only the size matters.
func parseDockerDF(out string) *dockerSpace {
	d := &dockerSpace{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		kind, reclaimable, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		size, _, _ := strings.Cut(reclaimable, "(")
		n := parseSize(size)
		d.total += n
		d.rows = append(d.rows, dockerRow{kind: kind, size: n})
	}
	return d
}

var sizeRE = regexp.MustCompile(`(?i)^([\d.]+)\s*([KMGTP]?)B?$`)

// parseSize reads Docker's human sizes — "83.54GB", "1.2kB", "0B" — which
// are decimal (go-units HumanSize). The Python widget read them as binary
// and overstated them by 7 %. Anything unreadable is 0.
func parseSize(text string) uint64 {
	m := sizeRE.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	scale := 1.0
	if m[2] != "" {
		for range strings.Index("KMGTP", strings.ToUpper(m[2])) + 1 {
			scale *= 1000
		}
	}
	return uint64(v * scale)
}

// blocks say plainly what this is — space you could get back, and where —
// then what it is made of, and the commands when it is worth running them.
// Only the sizes over the line are coloured: a path and a command are not
// problems.
func (d *dockerSpace) blocks() model.Blocks {
	headline := model.Row{
		Name:  sys.FmtBytes(float64(d.total)) + " could be freed",
		Value: d.root,
	}
	out := model.Blocks{model.Section{Title: "Docker", Meta: "reclaimable"}, headline}
	if len(d.rows) > 0 {
		stats := model.Stats{}
		for _, r := range d.rows {
			st := model.Stat{Key: strings.ToLower(r.kind), Value: sys.FmtBytes(float64(r.size))}
			if r.size > dockerRowHeavy {
				st.Tone = model.ToneWarn
			}
			stats.Items = append(stats.Items, st)
		}
		out = append(out, stats)
	}
	if d.total > dockerNoteworthy {
		out = append(out, model.Note{Text: "docker system prune -a · docker builder prune", Mono: true})
	}
	return out
}

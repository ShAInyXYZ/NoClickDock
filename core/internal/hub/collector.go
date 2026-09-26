package hub

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// Meta is what the hub needs to know about a widget before it has data.
type Meta struct {
	ID       string
	Title    string
	Order    int // position on the rail, ascending
	PollSecs int
}

// Collector measures one widget. Collect runs on the collector's own
// goroutine and never concurrently with itself; it should honour ctx, which
// carries the collection timeout.
type Collector interface {
	Meta() Meta
	Collect(ctx context.Context) (model.Snapshot, error)
}

// Actor is a collector whose panel or menu can change the world. It returns
// a short past-tense message for the result ("stopped api-gateway").
type Actor interface {
	Act(ctx context.Context, action string, args map[string]string) (string, error)
}

// Forcer is a collector with slow sources of its own (a CLI on a five-minute
// cadence, a transcript scan): a user's "Refresh now" asks them again, if
// they are older than a few seconds (dock-core §5.2). What was last read is
// shown meanwhile — a refresh never blanks a panel.
type Forcer interface {
	Force()
}

// Asker answers questions that change nothing (Settings asking before it
// saves): "probe" an endpoint, list the "peers" seen. Args are the
// question's own; what is not understood is an error.
type Asker interface {
	Ask(ctx context.Context, what string, args map[string]string) (message string, items []string, err error)
}

// Transitioner words a widget's own state changes ("Generation complete")
// instead of repeating the new state label. An empty message raises no toast.
type Transitioner interface {
	Transition(from, to model.State, s model.Snapshot) string
}

// Mocker is a widget that can invent its own data, for --mock: the design
// review and the README images run on it, never on a real machine's state.
// Mock is a function of time, so a live mock can move.
type Mocker interface {
	Meta() Meta
	Mock(now time.Time) model.Snapshot
}

// Varianter is a mock with named alternative panels — "unavailable",
// "offline" — that the gallery shows next to the main one.
type Varianter interface {
	Variants(now time.Time) map[string]model.Snapshot
}

// Still is the instant a still mock is drawn at: the gallery and README
// images must not change between two shots.
var Still = time.Date(2026, 9, 26, 12, 41, 7, 0, time.Local)

// Mocked turns a Mocker into a collector. Still mocks are drawn at one
// fixed instant; live ones move with the clock (progress, a container that
// falls ill now and then) so toasts can be reviewed. Their actions only say
// what they would have done: a mock session can never touch the machine.
func Mocked(m Mocker, live bool) Collector { return mocked{m, live} }

type mocked struct {
	m    Mocker
	live bool
}

func (w mocked) now() time.Time {
	if w.live {
		return time.Now()
	}
	return Still
}

func (w mocked) Meta() Meta { return w.m.Meta() }

func (w mocked) Collect(context.Context) (model.Snapshot, error) {
	return w.m.Mock(w.now()), nil
}

func (w mocked) Transition(from, to model.State, s model.Snapshot) string {
	if t, ok := w.m.(Transitioner); ok {
		return t.Transition(from, to, s)
	}
	return s.StateLabel
}

func (w mocked) Variants(time.Time) map[string]model.Snapshot {
	if v, ok := w.m.(Varianter); ok {
		return v.Variants(w.now())
	}
	return nil
}

// Ask: a mock answers from its invention, when it has one.
func (w mocked) Ask(ctx context.Context, what string, args map[string]string) (string, []string, error) {
	if a, ok := w.m.(Asker); ok {
		return a.Ask(ctx, what, args)
	}
	return "", nil, fmt.Errorf("%s cannot answer %q", w.m.Meta().Title, what)
}

func (w mocked) Act(ctx context.Context, action string, args map[string]string) (string, error) {
	// A mock may play out its own answers (a refusal, for review); it still
	// never touches the machine.
	if a, ok := w.m.(Actor); ok {
		return a.Act(ctx, action, args)
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{action}
	for _, k := range keys {
		parts = append(parts, args[k])
	}
	return fmt.Sprintf("mock: %s", strings.Join(parts, " ")), nil
}

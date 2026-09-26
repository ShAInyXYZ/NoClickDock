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

// Mocker is a widget that can invent its own data, for --mock: the design
// review and the README images run on it, never on a real machine's state.
type Mocker interface {
	Meta() Meta
	Mock(now time.Time) model.Snapshot
}

// Mocked turns a Mocker into a collector. Its actions only say what they
// would have done, so a mock session can never touch the machine.
func Mocked(m Mocker) Collector { return mocked{m} }

type mocked struct{ m Mocker }

func (w mocked) Meta() Meta { return w.m.Meta() }

func (w mocked) Collect(context.Context) (model.Snapshot, error) {
	return w.m.Mock(time.Now()), nil
}

func (w mocked) Act(_ context.Context, action string, args map[string]string) (string, error) {
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

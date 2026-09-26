package hub

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// scripted returns the next queued result on every Collect.
type scripted struct {
	meta    Meta
	results []result
	acted   []string
}

func (s *scripted) Meta() Meta { return s.meta }

func (s *scripted) Collect(context.Context) (model.Snapshot, error) {
	r := s.results[0]
	if len(s.results) > 1 {
		s.results = s.results[1:]
	}
	return r.snap, r.err
}

func (s *scripted) Act(_ context.Context, action string, args map[string]string) (string, error) {
	s.acted = append(s.acted, action+" "+args["container"])
	if action == "remove" {
		return "", errors.New("container is gone")
	}
	return action + "ped " + args["container"], nil
}

func snap(state model.State, label string, issues ...string) model.Snapshot {
	s := model.Snapshot{State: state, StateLabel: label, Pages: []model.Page{{ID: "main"}}}
	for _, id := range issues {
		s.Issues = append(s.Issues, model.Issue{ID: id, Text: id + " failed", Tone: model.ToneCrit})
	}
	return s
}

func newHub(t *testing.T, cs ...Collector) *Hub {
	t.Helper()
	cfg, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New("linux", cfg, cs)
}

// drain returns every message sent so far, decoded just enough to read.
func drain(s *Subscription) []map[string]any {
	var out []map[string]any
	for {
		select {
		case raw := <-s.C:
			var m map[string]any
			json.Unmarshal(raw, &m)
			out = append(out, m)
		default:
			return out
		}
	}
}

func types(msgs []map[string]any) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m["type"].(string))
	}
	return out
}

func toasts(msgs []map[string]any) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		if m["type"] == "toast" {
			out = append(out, m["toast"].(map[string]any))
		}
	}
	return out
}

func TestEveryPollIsHeardButOnlyChangesCarryASnapshot(t *testing.T) {
	c := &scripted{meta: Meta{ID: "disk", Title: "Disk", PollSecs: 30}, results: []result{
		{snap: snap(model.StateOK, "412G free")},
		{snap: snap(model.StateOK, "412G free")},
		{snap: snap(model.StateOK, "411G free")},
	}}
	h := newHub(t, c)
	_, sub := h.Subscribe()
	ctx := context.Background()
	e := h.entries[0]

	h.poll(ctx, e)
	first := e.last.UpdatedAt
	h.poll(ctx, e)
	if got := types(drain(sub)); !slices.Equal(got, []string{"snapshot", "tick"}) {
		t.Fatalf("an unchanged poll must send a tick, not a snapshot: %v", got)
	}
	if e.last.UpdatedAt != first {
		t.Fatal("updatedAt moved without a change")
	}
	h.poll(ctx, e)
	msgs := drain(sub)
	if len(msgs) != 1 || msgs[0]["widget"].(map[string]any)["stateLabel"] != "411G free" {
		t.Fatalf("a changed poll was not sent: %v", msgs)
	}
}

func TestHubFillsMetaAndEmptyPages(t *testing.T) {
	c := &scripted{meta: Meta{ID: "disk", Title: "Disk", Order: 40, PollSecs: 30},
		results: []result{{snap: model.Snapshot{State: model.StateOK}}}}
	h := newHub(t, c)
	h.poll(context.Background(), h.entries[0])
	s := h.entries[0].last
	if s.ID != "disk" || s.Title != "Disk" || s.Order != 40 || s.PollSecs != 30 || s.Pages == nil {
		t.Fatalf("not normalised: %+v", s)
	}
}

func TestTransitionsBecomeToasts(t *testing.T) {
	c := &scripted{meta: Meta{ID: "docker", PollSecs: 20}, results: []result{
		{snap: snap(model.StateOK, "14 running")},                     // first poll: silent
		{snap: snap(model.StateCrit, "1 unhealthy", "api")},           // the specific issue, not the generic change
		{snap: snap(model.StateCrit, "2 unhealthy", "api", "worker")}, // a new issue toasts itself
		{snap: snap(model.StateCrit, "2 unhealthy", "api", "worker")}, // nothing new
		{snap: snap(model.StateOK, "14 running")},                     // recovery says so
	}}
	h := newHub(t, c)
	_, sub := h.Subscribe()
	var got []map[string]any
	for range c.results {
		h.poll(context.Background(), h.entries[0])
		got = append(got, toasts(drain(sub))...)
	}
	want := []struct{ tone, text string }{
		{"crit", "api failed"},
		{"crit", "worker failed"},
		{"ok", "14 running"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d toasts %v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i]["tone"] != w.tone || got[i]["text"] != w.text || got[i]["widget"] != "docker" {
			t.Errorf("toast %d = %v, want %v", i, got[i], w)
		}
	}
}

func TestAtMostThreeIssueToastsMostSevereFirst(t *testing.T) {
	burst := snap(model.StateCrit, "4 problems")
	for i, tone := range []model.Tone{model.ToneWarn, model.ToneWarn, model.ToneCrit, model.ToneWarn} {
		burst.Issues = append(burst.Issues, model.Issue{ID: string(rune('a' + i)), Text: string(rune('a' + i)), Tone: tone})
	}
	c := &scripted{meta: Meta{ID: "docker"}, results: []result{{snap: snap(model.StateOK, "ok")}, {snap: burst}}}
	h := newHub(t, c)
	_, sub := h.Subscribe()
	h.poll(context.Background(), h.entries[0])
	h.poll(context.Background(), h.entries[0])
	got := toasts(drain(sub))
	if len(got) != 3 || got[0]["text"] != "c" || got[0]["tone"] != "crit" || got[1]["text"] != "a" {
		t.Fatalf("toasts = %v", got)
	}
}

func TestFailureKeepsThePanelShowsTheErrorAndToastsNothing(t *testing.T) {
	good := snap(model.StateOK, "412G free")
	good.Glance = model.Glance{Text: "71%"}
	good.Pages[0].Blocks = model.Blocks{model.Note{Text: "still here"}}
	c := &scripted{meta: Meta{ID: "disk"}, results: []result{
		{snap: good},
		{err: errors.New("statfs /: permission denied")},
		{snap: good},
	}}
	h := newHub(t, c)
	clock := time.Date(2026, 9, 26, 12, 38, 2, 0, time.UTC)
	h.now = func() time.Time { return clock }
	_, sub := h.Subscribe()

	h.poll(context.Background(), h.entries[0])
	clock = clock.Add(30 * time.Second)
	h.poll(context.Background(), h.entries[0])
	s := h.entries[0].last
	if s.Error != "statfs /: permission denied" || s.State != model.StateIdle || s.Glance.Text != "" {
		t.Fatalf("failure not shown: %+v", s)
	}
	if want := time.Date(2026, 9, 26, 12, 38, 2, 0, time.UTC); !s.UpdatedAt.Equal(want) {
		t.Fatalf("updatedAt = %s, want the last good reading %s", s.UpdatedAt, want)
	}
	if len(s.Pages) != 1 || len(s.Pages[0].Blocks) != 1 {
		t.Fatal("the last known panel was dropped")
	}

	h.poll(context.Background(), h.entries[0]) // recovers to the same ok state
	if got := toasts(drain(sub)); len(got) != 0 {
		t.Fatalf("a failed collect raised or cleared toasts: %v", got)
	}
}

type panicky struct{}

func (panicky) Meta() Meta { return Meta{ID: "bad"} }
func (panicky) Collect(context.Context) (model.Snapshot, error) {
	var m map[string]int
	m["boom"]++ // nil map write
	return model.Snapshot{}, nil
}

func TestACrashingCollectorIsReportedNotFatal(t *testing.T) {
	h := newHub(t, panicky{})
	h.poll(context.Background(), h.entries[0])
	if s := h.entries[0].last; s.State != model.StateIdle || len(s.Error) < 10 {
		t.Fatalf("crash not reported: %+v", s)
	}
}

type blocking struct{ release chan struct{} }

func (blocking) Meta() Meta { return Meta{ID: "slow", PollSecs: 30} }
func (b blocking) Collect(context.Context) (model.Snapshot, error) {
	<-b.release
	return snap(model.StateOK, "done"), nil
}

func TestASlowCollectSaysSoAndThenReports(t *testing.T) {
	b := blocking{release: make(chan struct{})}
	h := newHub(t, b)
	h.stillAfter = 20 * time.Millisecond
	_, sub := h.Subscribe()
	go func() {
		time.Sleep(80 * time.Millisecond)
		close(b.release)
	}()
	h.poll(context.Background(), h.entries[0])
	msgs := drain(sub)
	if len(msgs) != 2 {
		t.Fatalf("want 'still collecting' then the result, got %v", types(msgs))
	}
	first := msgs[0]["widget"].(map[string]any)
	if first["error"] != "still collecting…" || first["stateLabel"] != "collecting" {
		t.Fatalf("first = %v", first)
	}
	if last := msgs[1]["widget"].(map[string]any); last["stateLabel"] != "done" || last["error"] != nil {
		t.Fatalf("last = %v", last)
	}
}

func TestSubscribeStartsWithHelloAndLatestSnapshots(t *testing.T) {
	a := &scripted{meta: Meta{ID: "docker", Order: 50}, results: []result{{snap: snap(model.StateOK, "up")}}}
	b := &scripted{meta: Meta{ID: "disk", Order: 40}, results: []result{{snap: snap(model.StateOK, "free")}}}
	h := newHub(t, a, b)
	for _, e := range h.entries {
		h.poll(context.Background(), e)
		h.poll(context.Background(), e) // unchanged: a tick joins the replay
	}
	state := h.State()
	if len(state) != 5 {
		t.Fatalf("state has %d messages, want hello + 2 × (snapshot, tick)", len(state))
	}
	var hello model.Hello
	json.Unmarshal(state[0], &hello)
	if hello.Type != "hello" || hello.Version != model.Version || len(hello.Widgets) != 2 || hello.Widgets[0] != "disk" {
		t.Fatalf("hello = %+v (widgets must follow rail order)", hello)
	}
}

func withActions() model.Snapshot {
	s := snap(model.StateOK, "up")
	s.Pages[0].Blocks = model.Blocks{
		model.Row{Name: "api-gateway", Actions: []model.Action{
			{ID: "stop", Label: "stop", Args: map[string]string{"container": "api-gateway"}},
			{ID: "remove", Label: "remove", Args: map[string]string{"container": "api-gateway"}},
		}},
	}
	s.Menu = []model.MenuItem{{ID: "open-portainer", Label: "Open Portainer"}, {ID: "docs", Label: "Docs", URL: "https://example.com"}}
	return s
}

func TestActionsAreCheckedAgainstWhatWasOffered(t *testing.T) {
	c := &scripted{meta: Meta{ID: "docker"}, results: []result{{snap: withActions()}}}
	h := newHub(t, c)
	h.poll(context.Background(), h.entries[0])
	_, sub := h.Subscribe()

	send := func(line string) map[string]any {
		t.Helper()
		if err := h.Handle(context.Background(), []byte(line)); err != nil {
			t.Fatal(err)
		}
		select {
		case raw := <-sub.C:
			var m map[string]any
			json.Unmarshal(raw, &m)
			return m
		case <-time.After(2 * time.Second):
			t.Fatal("no result")
			return nil
		}
	}

	r := send(`{"type":"action","id":"a1","widget":"docker","action":"stop","args":{"container":"api-gateway"}}`)
	if r["type"] != "result" || r["id"] != "a1" || r["ok"] != true || r["message"] != "stopped api-gateway" {
		t.Fatalf("result = %v", r)
	}
	select {
	case <-h.entries[0].refresh:
	default:
		t.Fatal("a successful action did not refresh its widget")
	}

	r = send(`{"type":"action","id":"a2","widget":"docker","action":"remove","args":{"container":"api-gateway"}}`)
	if r["ok"] != false || r["message"] != "container is gone" {
		t.Fatalf("failure result = %v", r)
	}
	if r = send(`{"type":"action","id":"a3","widget":"docker","action":"open-portainer"}`); r["ok"] != true {
		t.Fatalf("menu action refused: %v", r)
	}

	// Anything the snapshot did not offer is refused before the widget sees it.
	for _, line := range []string{
		`{"type":"action","id":"x1","widget":"docker","action":"stop","args":{"container":"database"}}`,
		`{"type":"action","id":"x2","widget":"docker","action":"stop","args":{"container":"api-gateway","force":"1"}}`,
		`{"type":"action","id":"x3","widget":"docker","action":"exec","args":{"container":"api-gateway"}}`,
		`{"type":"action","id":"x4","widget":"docker","action":"docs"}`,
	} {
		if r := send(line); r["ok"] != false || r["message"] != "unknown action" {
			t.Errorf("%s → %v", line, r)
		}
	}
	if len(c.acted) != 3 {
		t.Fatalf("the widget ran %v", c.acted)
	}
	if r := send(`{"type":"action","id":"a4","widget":"nope","action":"stop"}`); r["ok"] != false || r["id"] != "a4" {
		t.Fatalf("unknown widget result = %v", r)
	}
}

func TestConfigPatchIsBroadcast(t *testing.T) {
	h := newHub(t)
	_, sub := h.Subscribe()
	if err := h.Handle(context.Background(), []byte(`{"type":"config.patch","patch":{"dock":{"orientation":"horizontal"}}}`)); err != nil {
		t.Fatal(err)
	}
	msgs := drain(sub)
	if len(msgs) != 1 || msgs[0]["type"] != "config" {
		t.Fatalf("got %v", msgs)
	}
	dock := msgs[0]["config"].(map[string]any)["dock"].(map[string]any)
	if dock["orientation"] != "horizontal" {
		t.Fatalf("config = %v", dock)
	}
	if err := h.Handle(context.Background(), []byte(`{"type":"config.patch","patch":{"dock":{"orientation":"sideways"}}}`)); err == nil {
		t.Fatal("an invalid patch was accepted")
	}
}

func TestBadRequestsAreRejected(t *testing.T) {
	h := newHub(t)
	for _, line := range []string{`nope`, `{"type":"launch"}`, `{"type":"refresh","widget":"ghost"}`, `{"type":"action","widget":"disk"}`} {
		if err := h.Handle(context.Background(), []byte(line)); err == nil {
			t.Errorf("%s was accepted", line)
		}
	}
}

func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	h := newHub(t)
	_, sub := h.Subscribe()
	for i := 0; i < subscriberBuffer+1; i++ {
		h.broadcast(model.NewResult("x", true, ""))
	}
	n := 0
	for range sub.C { // closed after the buffered messages
		n++
	}
	if n != subscriberBuffer {
		t.Fatalf("read %d messages before close, want %d", n, subscriberBuffer)
	}
}

func TestMockedActionsNeverTouchTheMachine(t *testing.T) {
	msg, err := Mocked(nil).(Actor).Act(context.Background(), "stop", map[string]string{"container": "api-gateway"})
	if err != nil || msg != "mock: stop api-gateway" {
		t.Fatalf("got %q, %v", msg, err)
	}
}

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
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

// stubborn ignores its deadline, like a statfs stuck on a dead mount, and
// counts how many of it run at once.
type stubborn struct {
	release chan struct{}
	running atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
}

func (*stubborn) Meta() Meta { return Meta{ID: "stuck", PollSecs: 1} }
func (s *stubborn) Collect(context.Context) (model.Snapshot, error) {
	s.calls.Add(1)
	n := s.running.Add(1)
	defer s.running.Add(-1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	<-s.release
	return snap(model.StateOK, "answered at last"), nil
}

func TestAStuckCollectIsNeverJoinedByAnother(t *testing.T) {
	s := &stubborn{release: make(chan struct{})}
	h := newHub(t, s)
	h.stillAfter = 5 * time.Millisecond
	_, sub := h.Subscribe()
	e := h.entries[0]

	for range 5 { // five polls fall due while the first collect is stuck
		h.poll(context.Background(), e)
	}
	if calls, peak := s.calls.Load(), s.peak.Load(); calls != 1 || peak != 1 {
		t.Fatalf("%d collects started, %d at once; want 1 and 1", calls, peak)
	}
	got := types(drain(sub))
	if !slices.Equal(got, []string{"snapshot", "tick", "tick", "tick", "tick"}) {
		t.Fatalf("a stuck widget must be heard every poll: %v", got)
	}
	if e.last.Error != "still collecting…" {
		t.Fatalf("error = %q", e.last.Error)
	}

	close(s.release) // the stuck call returns; the next poll applies it
	deadline := time.Now().Add(2 * time.Second)
	for e.last.StateLabel != "answered at last" {
		if time.Now().After(deadline) {
			t.Fatal("the late answer was never applied")
		}
		time.Sleep(5 * time.Millisecond)
		h.poll(context.Background(), e)
	}
	if e.last.Error != "" {
		t.Fatalf("the late answer kept the error %q", e.last.Error)
	}
}

func TestTheLoopAppliesALateAnswerAtOnce(t *testing.T) {
	s := &stubborn{release: make(chan struct{})}
	h := newHub(t, s)
	h.stillAfter = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	close(s.release)
	deadline := time.Now().Add(500 * time.Millisecond) // well before the next 1 s poll
	for {
		state := h.State()
		if len(state) > 1 && strings.Contains(string(state[1]), "answered at last") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop waited for its next poll to apply a finished collect")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.peak.Load() != 1 {
		t.Fatalf("peak %d", s.peak.Load())
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
	h.poll(context.Background(), h.entries[0])
	msgs := drain(sub)
	if len(msgs) != 1 {
		t.Fatalf("want 'still collecting', got %v", types(msgs))
	}
	first := msgs[0]["widget"].(map[string]any)
	if first["error"] != "still collecting…" || first["stateLabel"] != "collecting" {
		t.Fatalf("first = %v", first)
	}
	close(b.release)
	time.Sleep(20 * time.Millisecond)
	h.poll(context.Background(), h.entries[0])
	msgs = drain(sub)
	if len(msgs) != 1 {
		t.Fatalf("want the result, got %v", types(msgs))
	}
	if last := msgs[0]["widget"].(map[string]any); last["stateLabel"] != "done" || last["error"] != nil {
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
		for {
			select {
			case raw := <-sub.C:
				var m map[string]any
				json.Unmarshal(raw, &m)
				if m["type"] == "running" {
					// Every window hears the action start, with what it is.
					if m["widget"] != "docker" || m["action"] == nil {
						t.Fatalf("running = %v", m)
					}
					continue
				}
				return m
			case <-time.After(2 * time.Second):
				t.Fatal("no result")
				return nil
			}
		}
	}

	r := send(`{"type":"action","id":"a1","widget":"docker","action":"stop","args":{"container":"api-gateway"}}`)
	if r["type"] != "result" || r["id"] != "a1" || r["ok"] != true || r["message"] != "stopped api-gateway" ||
		r["widget"] != "docker" || r["action"] != "stop" || r["args"].(map[string]any)["container"] != "api-gateway" {
		t.Fatalf("result = %v", r)
	}
	if toast := send2(t, sub); toast["type"] != "toast" || toast["toast"].(map[string]any)["text"] != "stopped api-gateway" {
		t.Fatalf("a result must also toast (for menu actions): %v", toast)
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
	if toast := send2(t, sub)["toast"].(map[string]any); toast["tone"] != "crit" {
		t.Fatalf("a failure toasts crit: %v", toast)
	}
	if r = send(`{"type":"action","id":"a3","widget":"docker","action":"open-portainer"}`); r["ok"] != true {
		t.Fatalf("menu action refused: %v", r)
	}
	send2(t, sub)

	// Anything the snapshot did not offer is refused before the widget sees it.
	refused := []string{
		`{"type":"action","id":"x1","widget":"docker","action":"stop","args":{"container":"database"}}`,
		`{"type":"action","id":"x2","widget":"docker","action":"stop","args":{"container":"api-gateway","force":"1"}}`,
		`{"type":"action","id":"x3","widget":"docker","action":"exec","args":{"container":"api-gateway"}}`,
		`{"type":"action","id":"x4","widget":"docker","action":"docs"}`,
	}
	for _, line := range refused {
		if r := send(line); r["ok"] != false || r["message"] != "unknown action" {
			t.Errorf("%s → %v", line, r)
		}
		send2(t, sub)
	}
	if len(c.acted) != 3 {
		t.Fatalf("the widget ran %v", c.acted)
	}
	if r := send(`{"type":"action","id":"a4","widget":"nope","action":"stop"}`); r["ok"] != false || r["id"] != "a4" {
		t.Fatalf("unknown widget result = %v", r)
	}
}

// send2 reads the next message after a result.
func send2(t *testing.T, sub *Subscription) map[string]any {
	t.Helper()
	select {
	case raw := <-sub.C:
		var m map[string]any
		json.Unmarshal(raw, &m)
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
		return nil
	}
}

type worded struct{ scripted }

func (worded) Transition(from, to model.State, _ model.Snapshot) string {
	if from == model.StateBusy && to == model.StateOK {
		return "Generation complete"
	}
	return ""
}

func TestAWidgetMayWordItsOwnTransitions(t *testing.T) {
	c := &worded{scripted{meta: Meta{ID: "comfyui"}, results: []result{
		{snap: snap(model.StateOK, "idle")},
		{snap: snap(model.StateBusy, "generating")}, // worded as nothing: no toast
		{snap: snap(model.StateOK, "idle")},
	}}}
	h := newHub(t, c)
	_, sub := h.Subscribe()
	var got []map[string]any
	for range 3 {
		h.poll(context.Background(), h.entries[0])
		got = append(got, toasts(drain(sub))...)
	}
	if len(got) != 1 || got[0]["text"] != "Generation complete" || got[0]["tone"] != "ok" {
		t.Fatalf("toasts = %v", got)
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
	// A rejected patch re-announces the unchanged config, so a window can revert.
	msgs = drain(sub)
	if len(msgs) != 1 || msgs[0]["config"].(map[string]any)["dock"].(map[string]any)["orientation"] != "horizontal" {
		t.Fatalf("after a rejected patch: %v", msgs)
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
		h.broadcast(model.NewResult(model.Request{ID: "x"}, true, ""))
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
	msg, err := Mocked(nil, false).(Actor).Act(context.Background(), "stop", map[string]string{"container": "api-gateway"})
	if err != nil || msg != "mock: stop api-gateway" {
		t.Fatalf("got %q, %v", msg, err)
	}
}

// slowActor holds every action until released.
type slowActor struct {
	scripted
	release chan struct{}
}

func (s *slowActor) Act(context.Context, string, map[string]string) (string, error) {
	<-s.release
	return "done", nil
}

func TestTheSameActionRunsOnceAtATime(t *testing.T) {
	c := &slowActor{scripted: scripted{meta: Meta{ID: "docker"}, results: []result{{snap: withActions()}}}, release: make(chan struct{})}
	h := newHub(t, c)
	h.poll(context.Background(), h.entries[0])
	req := model.Request{Type: "action", ID: "a1", Widget: "docker", Action: "stop", Args: map[string]string{"container": "api-gateway"}}
	first := make(chan bool)
	go func() {
		ok, _ := h.runAction(context.Background(), req)
		first <- ok
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.Lock()
		n := len(h.running)
		h.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	req.ID = "a2"
	if ok, msg := h.runAction(context.Background(), req); ok || msg != stillRunning {
		t.Fatalf("a second press while running: %v %q", ok, msg)
	}
	// Its answer names nothing a window would take for the running
	// action's result, and raises no toast.
	_, sub := h.Subscribe()
	h.act(context.Background(), req)
	select {
	case raw := <-sub.C:
		var m map[string]any
		json.Unmarshal(raw, &m)
		if m["type"] != "result" || m["id"] != "a2" || m["widget"] != nil || m["action"] != nil {
			t.Fatalf("refusal = %v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no refusal")
	}
	select {
	case raw := <-sub.C:
		t.Fatalf("a refusal raised %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
	close(c.release)
	if !<-first {
		t.Fatal("the first press failed")
	}
	req.ID = "a3"
	if ok, _ := h.runAction(context.Background(), req); !ok {
		t.Fatal("once finished, it can run again")
	}
}

type noticing struct {
	scripted
	forced int
}

func (n *noticing) Force() { n.forced++ }

func TestNoticesToastAfterIssuesAndRefreshForces(t *testing.T) {
	ok := model.Snapshot{State: model.StateOK}
	back := model.Snapshot{State: model.StateOK, Notices: []model.Issue{{Text: "Console operational"}}}
	c := &noticing{scripted: scripted{meta: Meta{ID: "claude"}, results: []result{{snap: ok}, {snap: back}}}}
	h := newHub(t, c)
	if got := h.entries[0].transitions(ok, time.Now()); len(got) != 0 {
		t.Fatalf("nothing before armed: %v", got)
	}
	got := h.entries[0].transitions(back, time.Now())
	if len(got) != 1 || got[0].Text != "Console operational" || got[0].Tone != model.ToneOK {
		t.Fatalf("a notice toasts, ok by default: %+v", got)
	}
	if err := h.Handle(context.Background(), []byte(`{"type":"refresh","widget":"claude"}`)); err != nil || c.forced != 1 {
		t.Fatalf("refresh forces slow sources: %v %d", err, c.forced)
	}
}

// Settings: a patch with an id is answered, with the field the core refused.
func TestSettingsHearWhichFieldWasRefused(t *testing.T) {
	h := newHub(t)
	_, sub := h.Subscribe()
	h.Handle(context.Background(), []byte(`{"type":"config.patch","id":"s1","patch":{"widgets":{"comfyui":{"port":0}}}}`))
	msgs := drain(sub)
	if len(msgs) != 2 || msgs[1]["type"] != "config.result" || msgs[1]["ok"] != false ||
		msgs[1]["field"] != "widgets.comfyui.port" || msgs[1]["message"] == "" || msgs[1]["id"] != "s1" {
		t.Fatalf("got %v", msgs)
	}
	h.Handle(context.Background(), []byte(`{"type":"config.patch","id":"s2","patch":{"widgets":{"comfyui":{"port":8000}}}}`))
	msgs = drain(sub)
	if len(msgs) != 2 || msgs[1]["ok"] != true || msgs[1]["field"] != nil {
		t.Fatalf("got %v", msgs)
	}
}

// A disabled widget is not collected; the config's order is the rail's.
func TestDisabledWidgetsDoNotRunAndOrderFollowsTheConfig(t *testing.T) {
	collected := make(chan struct{}, 16)
	c := &counting{meta: Meta{ID: "docker", Order: 50, PollSecs: 1}, n: collected}
	h := newHub(t, c)
	if err := h.Handle(context.Background(), []byte(`{"type":"config.patch","patch":{"widgets":{"docker":{"enabled":false,"order":5}}}}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	select {
	case <-collected:
		t.Fatal("a disabled widget was collected")
	case <-time.After(300 * time.Millisecond):
	}
	_, sub := h.Subscribe()
	h.Handle(context.Background(), []byte(`{"type":"config.patch","patch":{"widgets":{"docker":{"enabled":true}}}}`))
	select {
	case <-collected: // turned on: polled at once, not at its next tick
	case <-time.After(2 * time.Second):
		t.Fatal("never collected once enabled")
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-sub.C:
			var m struct {
				Type   string
				Widget model.Snapshot
			}
			json.Unmarshal(raw, &m)
			if m.Type == "snapshot" {
				if m.Widget.Order != 5 {
					t.Errorf("order %d, want the config's 5", m.Widget.Order)
				}
				cancel()
				return
			}
		case <-deadline:
			cancel()
			t.Fatal("no snapshot")
		}
	}
}

type counting struct {
	meta Meta
	n    chan struct{}
}

func (c *counting) Meta() Meta { return c.meta }
func (c *counting) Collect(context.Context) (model.Snapshot, error) {
	c.n <- struct{}{}
	return snap(model.StateOK, "fine"), nil
}

type asking struct{ scripted }

func (asking) Ask(_ context.Context, what string, args map[string]string) (string, []string, error) {
	if what != "peers" {
		return "", nil, errors.New("no such question")
	}
	return "2 peers seen", []string{"fileserver", "build-agent"}, nil
}

func TestAsksAreAnswered(t *testing.T) {
	h := newHub(t, &asking{scripted{meta: Meta{ID: "tailscale"}}}, &scripted{meta: Meta{ID: "disk"}})
	_, sub := h.Subscribe()
	h.Handle(context.Background(), []byte(`{"type":"ask","id":"a1","widget":"tailscale","what":"peers"}`))
	if m := send2(t, sub); m["type"] != "answer" || m["id"] != "a1" || m["ok"] != true || len(m["items"].([]any)) != 2 {
		t.Errorf("answer %v", m)
	}
	h.Handle(context.Background(), []byte(`{"type":"ask","id":"a2","widget":"disk","what":"peers"}`))
	if m := send2(t, sub); m["ok"] != false || m["message"] == "" {
		t.Errorf("a widget that cannot answer: %v", m)
	}
	if err := h.Handle(context.Background(), []byte(`{"type":"ask","widget":"disk"}`)); err == nil {
		t.Error("an ask without an id")
	}
}

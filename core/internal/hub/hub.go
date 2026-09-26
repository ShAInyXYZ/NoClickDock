// Package hub runs the collectors and is the single source of truth the
// transports read from: one goroutine per widget on its own poll interval,
// something sent after every poll (the snapshot when it changed, a tick when
// it did not), transitions turned into toasts, and every UI request checked
// and routed to the widget it names. docs/rewrite/specs/dock-core.md §5.
package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

const (
	// collectTimeout bounds a whole collect. Collectors bound their own
	// commands; one that still takes this long is reported as failed.
	collectTimeout = 60 * time.Second
	actionTimeout  = 2 * time.Minute
	// minGap keeps a collect that overran its interval from running back to
	// back with the next one.
	minGap = time.Second
	// subscriberBuffer is how far a transport may fall behind before it is
	// dropped; it then resubscribes and starts again from the full state.
	subscriberBuffer = 256
	// maxAlertToasts caps the toasts one poll may raise.
	maxAlertToasts = 3
)

var errStillCollecting = errors.New("still collecting…")

type Hub struct {
	platform string
	cfg      *config.Store
	entries  []*entry // rail order
	byID     map[string]*entry
	now      func() time.Time
	// stillAfter overrides "two intervals" before a slow collect is reported
	// (tests only; zero means the default).
	stillAfter time.Duration

	mu   sync.Mutex // guards subs and every entry's shown / lastMsg / lastTick
	subs map[*Subscription]struct{}
}

type entry struct {
	c       Collector
	meta    Meta
	refresh chan struct{}

	// Owned by the collector's goroutine.
	last       model.Snapshot
	lastKey    []byte
	armed      bool // one good collect has happened: toasts may follow
	prevState  model.State
	prevIssues map[string]bool
	// transitionFrom is the state a transition is leaving, for Transitioner.
	transitionFrom model.State

	// What the interface was last sent, replayed to every new subscriber and
	// used to check actions against; guarded by Hub.mu.
	shown    model.Snapshot
	lastMsg  []byte
	lastTick []byte

	// inflight is the running collect's answer, nil when none runs. A
	// collect is never started while another is in flight: a collector that
	// ignores its deadline (a statfs stuck in the kernel) stays one stuck
	// goroutine, and stateful collectors never race themselves.
	inflight chan result
}

// Subscription receives every core → UI message, one JSON document each.
// C is closed if the subscriber falls too far behind.
type Subscription struct {
	C  <-chan []byte
	ch chan []byte
}

// Refresher is a collector that finishes slow work in the background and
// wants to be polled again as soon as it has something new.
type Refresher interface {
	SetRefresh(func())
}

func New(platform string, cfg *config.Store, collectors []Collector) *Hub {
	h := &Hub{
		platform: platform,
		cfg:      cfg,
		byID:     map[string]*entry{},
		now:      time.Now,
		subs:     map[*Subscription]struct{}{},
	}
	for _, c := range collectors {
		e := &entry{c: c, meta: c.Meta(), refresh: make(chan struct{}, 1)}
		h.entries = append(h.entries, e)
		h.byID[e.meta.ID] = e
		if r, ok := c.(Refresher); ok {
			id := e.meta.ID
			r.SetRefresh(func() { h.Refresh(id) })
		}
	}
	sort.SliceStable(h.entries, func(i, j int) bool { return h.entries[i].meta.Order < h.entries[j].meta.Order })
	return h
}

// Run polls every collector until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, e := range h.entries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.loop(ctx, e)
		}()
	}
	wg.Wait()
}

// loop polls every PollSecs measured from the start of each collect, so the
// period does not drift by the collect's own duration. A refresh that
// arrives mid-collect is kept (the channel holds one) and runs right after.
// A collect that outlived its poll is applied the moment it lands.
func (h *Hub) loop(ctx context.Context, e *entry) {
	interval := e.interval()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-e.inflight: // nil while nothing is in flight: never chosen
			e.inflight = nil
			h.apply(e, r.snap, r.err)
			continue
		case <-timer.C:
		case <-e.refresh:
		}
		start := time.Now()
		h.poll(ctx, e)
		timer.Reset(max(time.Until(start.Add(interval)), minGap))
	}
}

func (e *entry) interval() time.Duration {
	return time.Duration(max(e.meta.PollSecs, 1)) * time.Second
}

type result struct {
	snap model.Snapshot
	err  error
}

// poll runs one collect, or — when the last one has not returned — says
// that it is still collecting, which the interface hears every poll (a tick
// after the first time), so the widget reads as busy rather than stale.
func (h *Hub) poll(ctx context.Context, e *entry) {
	if e.inflight != nil {
		select {
		case r := <-e.inflight:
			e.inflight = nil
			h.apply(e, r.snap, r.err)
		default:
			h.apply(e, model.Snapshot{}, errStillCollecting)
		}
		return
	}
	e.inflight = h.start(ctx, e)

	slow := time.NewTimer(h.patience(e))
	defer slow.Stop()
	select {
	case r := <-e.inflight:
		e.inflight = nil
		if ctx.Err() == nil {
			h.apply(e, r.snap, r.err)
		}
	case <-slow.C:
		h.apply(e, model.Snapshot{}, errStillCollecting)
	case <-ctx.Done():
	}
}

// start runs one Collect on its own goroutine. A collector that panics is
// reported, never fatal; one that honours its deadline answers with it.
func (h *Hub) start(ctx context.Context, e *entry) chan result {
	done := make(chan result, 1)
	go func() {
		cctx, cancel := context.WithTimeout(ctx, collectTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				done <- result{err: fmt.Errorf("collector crashed: %v", r)}
			}
		}()
		snap, err := e.c.Collect(cctx)
		if err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("gave up after %s", collectTimeout)
		}
		done <- result{snap, err}
	}()
	return done
}

// patience is how long a poll waits before saying "still collecting…":
// two intervals, but at most half the collect timeout, so a 30 s widget
// says so before its collect gives up.
func (h *Hub) patience(e *entry) time.Duration {
	if h.stillAfter > 0 {
		return h.stillAfter
	}
	return min(2*e.interval(), collectTimeout/2)
}

// apply turns one collect's outcome into messages.
func (h *Hub) apply(e *entry, snap model.Snapshot, err error) {
	now := h.now()
	if err != nil {
		snap = e.failed(err)
	}
	e.normalize(&snap)

	// A failed collect neither raises nor clears toasts: the previous state
	// and issues stay as they were until a collect succeeds.
	var toasts []model.Toast
	if err == nil {
		toasts = e.transitions(snap, now)
	}

	key, kerr := contentKey(snap)
	if kerr != nil {
		log.Printf("%s: %v", e.meta.ID, kerr)
		return
	}
	if bytes.Equal(key, e.lastKey) {
		h.tick(e, now)
	} else {
		// updatedAt is when the shown data was measured: a failure keeps the
		// last good time, because that is what the panel still shows.
		if err == nil {
			snap.UpdatedAt = now
		}
		e.last, e.lastKey = snap, key
		h.publish(e, snap)
	}
	for _, t := range toasts {
		h.broadcast(model.NewToast(t))
	}
}

// failed keeps the last known panel, marked with the error: a collector's
// failure is shown, never swallowed, and what was true a poll ago is still
// worth reading next to it.
func (e *entry) failed(err error) model.Snapshot {
	s := e.last
	s.Error = err.Error()
	s.State = model.StateIdle
	s.StateLabel = "unavailable"
	if errors.Is(err, errStillCollecting) {
		s.StateLabel = "collecting"
	}
	s.Glance = model.Glance{}
	s.Progress = nil
	s.Issues = nil
	return s
}

func (e *entry) normalize(s *model.Snapshot) {
	s.ID, s.Title, s.Order, s.PollSecs = e.meta.ID, e.meta.Title, e.meta.Order, e.meta.PollSecs
	if s.State == "" {
		s.State = model.StateIdle
	}
	if s.Pages == nil {
		s.Pages = []model.Page{}
	}
}

// transitions (dock-core.md §5.4): nothing before the first good collect;
// then new issues toast themselves — at most three, most severe first — and
// replace the generic state-change toast, because specific beats generic;
// without new issues, a state change toasts its label in the new state's tone.
func (e *entry) transitions(s model.Snapshot, now time.Time) []model.Toast {
	ids := make(map[string]bool, len(s.Issues))
	var fresh []model.Issue
	for _, is := range s.Issues {
		ids[is.ID] = true
		if !e.prevIssues[is.ID] {
			fresh = append(fresh, is)
		}
	}
	armed, changed := e.armed, s.State != e.prevState
	e.transitionFrom = e.prevState
	e.prevIssues, e.prevState, e.armed = ids, s.State, true
	if !armed {
		return nil
	}

	var out []model.Toast
	if len(fresh) > 0 {
		sort.SliceStable(fresh, func(i, j int) bool { return severity(fresh[i].Tone) > severity(fresh[j].Tone) })
		for _, is := range fresh[:min(len(fresh), maxAlertToasts)] {
			tone := is.Tone
			if tone == model.ToneNone {
				tone = model.ToneCrit
			}
			out = append(out, model.Toast{Widget: s.ID, Tone: tone, Text: is.Text, At: now})
		}
		return out
	}
	if !changed {
		return out
	}
	text := s.StateLabel
	if t, ok := e.c.(Transitioner); ok {
		text = t.Transition(e.transitionFrom, s.State, s)
	}
	if text != "" {
		out = append(out, model.Toast{Widget: s.ID, Tone: s.State.Tone(), Text: text, At: now})
	}
	return out
}

func severity(t model.Tone) int {
	switch t {
	case model.ToneCrit, model.ToneNone:
		return 2
	case model.ToneWarn:
		return 1
	}
	return 0
}

func contentKey(s model.Snapshot) ([]byte, error) {
	s.UpdatedAt = time.Time{}
	return json.Marshal(s)
}

// Refresh polls a widget now instead of at its next tick; "" means all. A
// refresh while one is pending, or while a collect runs, is folded into it.
func (h *Hub) Refresh(widget string) {
	for _, e := range h.entries {
		if widget == "" || widget == e.meta.ID {
			select {
			case e.refresh <- struct{}{}:
			default:
			}
		}
	}
}

// Handle executes one UI → core message.
func (h *Hub) Handle(ctx context.Context, line []byte) error {
	var req model.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return fmt.Errorf("malformed message: %w", err)
	}
	switch req.Type {
	case "refresh":
		if req.Widget != "" && h.byID[req.Widget] == nil {
			return fmt.Errorf("refresh: unknown widget %q", req.Widget)
		}
		h.Refresh(req.Widget)
	case "action":
		if req.ID == "" {
			return fmt.Errorf("action: missing id")
		}
		go h.act(ctx, req)
	case "config.patch":
		cfg, err := h.cfg.Patch(req.Patch)
		// A rejected patch re-announces the config it left unchanged, so a
		// window that changed a control ahead of the answer puts it back.
		h.broadcast(model.NewConfig(cfg))
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown message type %q", req.Type)
	}
	return nil
}

func (h *Hub) act(ctx context.Context, req model.Request) {
	ok, msg := h.runAction(ctx, req)
	h.broadcast(model.NewResult(req, ok, msg))
	// Also a toast: an action chosen from a menu has no panel to answer in.
	// The shell drops it when the widget's panel is open and already says it.
	if msg != "" && h.byID[req.Widget] != nil {
		tone := model.ToneOK
		if !ok {
			tone = model.ToneCrit
		}
		h.broadcast(model.NewToast(model.Toast{Widget: req.Widget, Tone: tone, Text: msg, At: h.now()}))
	}
	if ok {
		h.Refresh(req.Widget) // the world changed; show it now
	}
}

func (h *Hub) runAction(ctx context.Context, req model.Request) (bool, string) {
	e := h.byID[req.Widget]
	if e == nil {
		return false, fmt.Sprintf("unknown widget %q", req.Widget)
	}
	a, ok := e.c.(Actor)
	if !ok {
		return false, fmt.Sprintf("%s has no actions", e.meta.Title)
	}
	// The interface can only ask for what the core itself offered in the
	// snapshot it was shown: it never names a command, a flag, or a
	// container that is not on screen.
	h.mu.Lock()
	allowed := offered(e.shown, req.Action, req.Args)
	h.mu.Unlock()
	if !allowed {
		return false, "unknown action"
	}
	h.broadcast(model.NewRunning(req))
	actx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	msg, err := a.Act(actx, req.Action, req.Args)
	if err != nil {
		return false, err.Error()
	}
	return true, msg
}

// offered reports whether a snapshot carries this action with exactly these
// arguments, as a button on some page or as a menu item.
func offered(s model.Snapshot, action string, args map[string]string) bool {
	for _, m := range s.Menu {
		if m.ID == action && m.URL == "" && m.Copy == "" && len(args) == 0 {
			return true
		}
	}
	for _, p := range s.Pages {
		if offeredIn(p.Blocks, action, args) {
			return true
		}
	}
	return false
}

func offeredIn(blocks model.Blocks, action string, args map[string]string) bool {
	for _, b := range blocks {
		var acts []model.Action
		switch v := b.(type) {
		case model.Row:
			acts = v.Actions
		case model.Alert:
			acts = v.Actions
		case model.Split:
			for _, col := range v.Columns {
				if offeredIn(col, action, args) {
					return true
				}
			}
		}
		for _, a := range acts {
			if a.ID == action && maps.Equal(a.Args, args) {
				return true
			}
		}
	}
	return false
}

// Variants are the mocks' alternative panels, by widget and name, for the
// gallery. Real collectors have none.
func (h *Hub) Variants() map[string]map[string]model.Snapshot {
	out := map[string]map[string]model.Snapshot{}
	now := h.now()
	for _, e := range h.entries {
		v, ok := e.c.(Varianter)
		if !ok {
			continue
		}
		for name, s := range v.Variants(now) {
			e.normalize(&s)
			s.UpdatedAt = now
			if out[e.meta.ID] == nil {
				out[e.meta.ID] = map[string]model.Snapshot{}
			}
			out[e.meta.ID][name] = s
		}
	}
	return out
}

// Hello is the first message of every session.
func (h *Hub) Hello() model.Hello {
	ids := make([]string, len(h.entries))
	for i, e := range h.entries {
		ids[i] = e.meta.ID
	}
	return model.NewHello(h.platform, h.cfg.Get(), ids)
}

// Subscribe returns the current state — hello, then every widget's latest
// snapshot and tick — and a subscription to every message after it, with
// nothing lost or repeated in between.
func (h *Hub) Subscribe() ([][]byte, *Subscription) {
	hello, _ := json.Marshal(h.Hello())
	h.mu.Lock()
	defer h.mu.Unlock()
	state := [][]byte{hello}
	for _, e := range h.entries {
		if e.lastMsg != nil {
			state = append(state, e.lastMsg)
		}
		if e.lastTick != nil {
			state = append(state, e.lastTick)
		}
	}
	ch := make(chan []byte, subscriberBuffer)
	s := &Subscription{C: ch, ch: ch}
	h.subs[s] = struct{}{}
	return state, s
}

func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.ch)
	}
}

// State is the current state alone, for GET /v1/state.
func (h *Hub) State() [][]byte {
	state, s := h.Subscribe()
	h.Unsubscribe(s)
	return state
}

func (h *Hub) publish(e *entry, s model.Snapshot) {
	msg, err := json.Marshal(model.NewSnapshot(s))
	if err != nil {
		log.Printf("%s: %v", e.meta.ID, err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// A snapshot supersedes the previous tick: it carries its own time.
	e.shown, e.lastMsg, e.lastTick = s, msg, nil
	h.sendLocked(msg)
}

// tick tells the interface the widget was polled just now and nothing changed.
func (h *Hub) tick(e *entry, at time.Time) {
	msg, err := json.Marshal(model.NewTick(e.meta.ID, at))
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e.lastTick = msg
	h.sendLocked(msg)
}

func (h *Hub) broadcast(v any) {
	msg, err := json.Marshal(v)
	if err != nil {
		log.Printf("broadcast: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendLocked(msg)
}

func (h *Hub) sendLocked(msg []byte) {
	for s := range h.subs {
		select {
		case s.ch <- msg:
		default:
			delete(h.subs, s)
			close(s.ch)
		}
	}
}

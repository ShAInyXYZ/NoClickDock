// What the interface knows about ncd-core, as runes: every widget's latest
// snapshot, when each was last checked, the config, recent toasts. Each
// window keeps its own copy, fed by its own transport; none of it is ever
// edited here, only replaced.
import type { Action, Config, CoreMessage, Toast, UiMessage, WidgetSnapshot } from "./protocol";
import { createTransport, type Transport } from "./transport";

export interface ActionResult {
  ok: boolean;
  message: string;
}

/** Longer than the core's own action timeout, so the core always answers first. */
const RESULT_TIMEOUT_MS = 150_000;
const TOAST_HISTORY = 20;
/** A widget unheard of for this many poll intervals is stale (DESIGN.md A11). */
const STALE_POLLS = 3;

class Core {
  platform = $state("");
  config = $state<Config | null>(null);
  snapshots = $state<Record<string, WidgetSnapshot>>({});
  /** When each widget was last heard of — a snapshot or a tick (ms since the epoch). */
  heard = $state<Record<string, number>>({});
  toasts = $state<(Toast & { key: number })[]>([]);
  linked = $state(false);
  /** Why the core is not answering, when the shell knows, and since when. */
  linkError = $state<string | null>(null);
  linkDownSince = $state<number | null>(null);
  /** A one-second clock: staleness is a matter of time passing. */
  now = $state(Date.now());

  /** The rail, in order. A widget appears once it has reported. */
  widgets = $derived(Object.values(this.snapshots).sort((a, b) => a.order - b.order));
  /** Widgets the core runs; the gallery waits for all of them. */
  expected = $state<string[]>([]);

  #transport: Transport | null = null;
  #pending = new Map<string, (result: ActionResult) => void>();
  #actionListeners: ((message: Extract<CoreMessage, { type: "running" | "result" }>) => void)[] = [];
  #toastKey = 0;
  #clock: ReturnType<typeof setInterval> | undefined;

  /** Connects this window; call once, from its page. */
  connect(): Promise<() => void> {
    this.#transport ??= createTransport();
    this.#clock ??= setInterval(() => (this.now = Date.now()), 1000);
    return this.#transport.connect(
      (message) => this.#apply(message),
      (up, error, since) => {
        this.linked = up;
        this.linkError = up ? null : (error ?? null);
        this.linkDownSince = up ? null : (since ?? this.linkDownSince ?? Date.now());
      },
    );
  }

  send(message: UiMessage): Promise<void> {
    if (!this.#transport) return Promise.reject(new Error("Not connected to ncd-core."));
    return this.#transport.send(message);
  }

  /** When the widget was last heard of, or 0. */
  lastHeard(id: string): number {
    return this.heard[id] ?? 0;
  }

  /** True when a widget has gone quiet: its dial must not keep an old green. */
  isStale(snapshot: WidgetSnapshot): boolean {
    const last = this.heard[snapshot.id];
    return last !== undefined && this.now - last > STALE_POLLS * snapshot.pollSecs * 1000;
  }

  /** Hears every action start and end, whichever window sent it: both
   *  panel windows show the same widget's buttons (M2-a). */
  onAction(fn: (message: Extract<CoreMessage, { type: "running" | "result" }>) => void) {
    this.#actionListeners.push(fn);
  }

  /** Runs a widget action and resolves with the core's answer. */
  act(widget: string, action: Pick<Action, "id" | "args">): Promise<ActionResult> {
    const id = crypto.randomUUID();
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(new Error("ncd-core did not answer."));
      }, RESULT_TIMEOUT_MS);
      this.#pending.set(id, (result) => {
        clearTimeout(timer);
        resolve(result);
      });
      this.send({ type: "action", id, widget, action: action.id, args: action.args }).catch((error) => {
        clearTimeout(timer);
        this.#pending.delete(id);
        reject(error);
      });
    });
  }

  #apply(message: CoreMessage) {
    switch (message.type) {
      case "hello": {
        // A (re)started core: forget widgets it no longer runs.
        this.platform = message.platform;
        this.config = message.config;
        this.expected = message.widgets;
        const known = new Set(message.widgets);
        for (const id of Object.keys(this.snapshots)) {
          if (!known.has(id)) {
            delete this.snapshots[id];
            delete this.heard[id];
          }
        }
        break;
      }
      case "snapshot":
        // Every poll sends a snapshot or a tick. A snapshot's updatedAt is
        // when its data was measured (a failing widget keeps its last good
        // time), so hearing it is timed here.
        this.snapshots[message.widget.id] = message.widget;
        this.heard[message.widget.id] = Date.now();
        break;
      case "tick":
        this.heard[message.widget] = Date.parse(message.at);
        break;
      case "config":
        this.config = message.config;
        break;
      case "toast":
        this.toasts = [...this.toasts, { ...message.toast, key: ++this.#toastKey }].slice(-TOAST_HISTORY);
        break;
      case "running":
        for (const fn of this.#actionListeners) fn(message);
        break;
      case "result": {
        const settle = this.#pending.get(message.id);
        this.#pending.delete(message.id);
        settle?.({ ok: message.ok, message: message.message });
        for (const fn of this.#actionListeners) fn(message);
        break;
      }
    }
  }
}

export const core = new Core();

/** The id of the rail's own dial, shown while the core is not answering. */
export const CORE_DIAL = "ncd";

/**
 * The shell's own snapshot while ncd-core is not answering: a dial of its
 * own on the rail, so the failure is on screen rather than an empty,
 * invisible app. It says only what is true.
 */
export function coreDown(error: string): WidgetSnapshot {
  return {
    id: CORE_DIAL,
    title: "NoClickDock",
    order: -1,
    state: "idle",
    stateLabel: "core not running",
    glance: { text: "off", tone: "idle" },
    updatedAt: "",
    pollSecs: 0,
    // The sentence says what is wrong; the footer never repeats it — it
    // says what the shell is doing about it, and since when (PanelFooter).
    footer: "retrying every 10 s",
    pages: [
      {
        id: "main",
        label: "",
        blocks: [
          {
            kind: "empty",
            text: `Nothing here can be measured: ${error}. NoClickDock keeps trying to start it.`,
            commands: [],
          },
        ],
      },
    ],
  };
}

/** 12:41:07 — the one time format the interface uses. */
export function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

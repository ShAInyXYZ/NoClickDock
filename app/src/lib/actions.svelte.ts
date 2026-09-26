// The state of the panel's action buttons, keyed by widget, action and
// arguments rather than held by the button: a snapshot rebuilds every row,
// and an arming that vanished with the old row would silently eat the
// user's first click on a destructive action.
import { core } from "./core.svelte";
import type { Action } from "./protocol";

/** A confirm stays armed this long; the second click must come within it. */
const ARM_MS = 4000;
/** A failed action keeps saying so this long, unless another action on the
 *  same widget runs first (ARCHITECTURE M2-a: a failure is never lost). */
const FAILED_MS = 10_000;

class Actions {
  // Armed is this window's own (losing an arm is the safe direction);
  // running and failed follow the core's broadcast `running` and `result`,
  // so both panel windows show them whichever one ran the action.
  armed = $state<Record<string, true>>({});
  running = $state<Record<string, true>>({});
  failed = $state<Record<string, string>>({});
  #timers = new Map<string, ReturnType<typeof setTimeout>>();

  constructor() {
    core.onAction((message) => {
      if (!message.widget || !message.action) return;
      const key = this.#keyOf(message.widget, message.action, message.args);
      if (message.type === "running") {
        this.#started(message.widget, key);
        return;
      }
      delete this.running[key];
      if (!message.ok) this.#fail(key, message.message);
    });
  }

  key(widget: string, action: Action) {
    return this.#keyOf(widget, action.id, action.args);
  }

  #keyOf(widget: string, id: string, args?: Record<string, string>) {
    const sorted = Object.fromEntries(Object.entries(args ?? {}).sort(([a], [b]) => a.localeCompare(b)));
    return `${widget}/${id}/${JSON.stringify(sorted)}`;
  }

  /** First click arms a confirm; the next click (or the only one) runs it. */
  async press(widget: string, action: Action) {
    const key = this.key(widget, action);
    if (this.running[key]) return;
    if (action.confirm && !this.armed[key]) {
      this.#later(key, ARM_MS, () => delete this.armed[key]);
      this.armed[key] = true;
      return;
    }
    this.#clear(key);
    delete this.armed[key];
    this.#started(widget, key);
    try {
      // The broadcast result settles running and failed in every window.
      await core.act(widget, action);
    } catch (error) {
      // No answer at all: only this window knows.
      delete this.running[key];
      this.#fail(key, error instanceof Error ? error.message : String(error));
    }
  }

  /** The failure one of these actions still shows, if any. */
  failureOf(widget: string, list: Action[] | undefined): string | undefined {
    for (const action of list ?? []) {
      const message = this.failed[this.key(widget, action)];
      if (message) return message;
    }
    return undefined;
  }

  // A new action on a widget supersedes any failure it still shows.
  #started(widget: string, key: string) {
    for (const old of Object.keys(this.failed)) {
      if (old.startsWith(`${widget}/`)) delete this.failed[old];
    }
    this.running[key] = true;
  }

  #fail(key: string, message: string) {
    this.failed[key] = message;
    this.#later(key, FAILED_MS, () => delete this.failed[key]);
  }

  #later(key: string, ms: number, fn: () => void) {
    this.#clear(key);
    this.#timers.set(key, setTimeout(() => {
      this.#timers.delete(key);
      fn();
    }, ms));
  }

  #clear(key: string) {
    clearTimeout(this.#timers.get(key));
    this.#timers.delete(key);
  }
}

export const actions = new Actions();

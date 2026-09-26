// The state of the panel's action buttons, keyed by widget, action and
// arguments rather than held by the button: a snapshot rebuilds every row,
// and an arming that vanished with the old row would silently eat the
// user's first click on a destructive action.
import { core } from "./core.svelte";
import type { Action } from "./protocol";

/** A confirm stays armed this long; the second click must come within it. */
const ARM_MS = 4000;
/** A failed action keeps saying so this long. */
const FAILED_MS = 6000;

class Actions {
  armed = $state<Record<string, true>>({});
  running = $state<Record<string, true>>({});
  failed = $state<Record<string, string>>({});
  #timers = new Map<string, ReturnType<typeof setTimeout>>();

  key(widget: string, action: Action) {
    return `${widget}/${action.id}/${JSON.stringify(action.args ?? {})}`;
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
    delete this.failed[key];
    this.running[key] = true;
    try {
      const result = await core.act(widget, action);
      if (!result.ok) this.#fail(key, result.message);
    } catch (error) {
      this.#fail(key, error instanceof Error ? error.message : String(error));
    } finally {
      delete this.running[key];
    }
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

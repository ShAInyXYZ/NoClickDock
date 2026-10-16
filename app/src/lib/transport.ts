// One way to reach ncd-core, two implementations. Inside Tauri the shell
// relays the sidecar's stdout as events and forwards core_send to its stdin;
// in a plain browser the core's development API (--dev / --mock) serves the
// same messages over SSE. Components never know which one is in use.
import { invoke, isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { CORE_MESSAGE_TYPES, type CoreMessage, type UiMessage } from "./protocol";

export interface Transport {
  /** Delivers the current state — hello, then every widget's latest
   *  snapshot — and then every message after it, until the returned
   *  function is called. */
  connect(onMessage: (message: CoreMessage) => void, onLink: (up: boolean, error?: string, since?: number) => void): Promise<() => void>;
  send(message: UiMessage): Promise<void>;
}

interface Link { up: boolean; error: string | null; since?: number | null }

class TauriTransport implements Transport {
  async connect(onMessage: (message: CoreMessage) => void, onLink: (up: boolean, error?: string, since?: number) => void) {
    // Listen first and hold what arrives until the cached state is applied:
    // nothing is lost in between, and nothing older lands on top of newer.
    let held: CoreMessage[] | null = [];
    const unlisten = await listen<string>("core", (event) => {
      const message = JSON.parse(event.payload) as CoreMessage;
      if (held) held.push(message);
      else onMessage(message);
    });
    // Whether the core is answering — and why not — comes from the shell,
    // which supervises it.
    const unlistenLink = await listen<Link>("core-link", (event) =>
      onLink(event.payload.up, event.payload.error ?? undefined, event.payload.since ?? undefined),
    );
    try {
      const state = await invoke<string[]>("core_state");
      for (const line of state) onMessage(JSON.parse(line));
    } finally {
      for (const message of held) onMessage(message);
      held = null;
    }
    const link = await invoke<Link>("core_link");
    onLink(link.up, link.error ?? undefined, link.since ?? undefined);
    return () => {
      unlisten();
      unlistenLink();
    };
  }

  send(message: UiMessage) {
    return invoke<void>("core_send", { line: JSON.stringify(message) });
  }
}

class HttpTransport implements Transport {
  constructor(private readonly base: string) {}

  async connect(onMessage: (message: CoreMessage) => void, onLink: (up: boolean, error?: string, since?: number) => void) {
    // EventSource reconnects by itself, and the core starts every stream
    // with the full state, so a restarted core needs no special handling.
    const source = new EventSource(`${this.base}/v1/events`);
    for (const type of CORE_MESSAGE_TYPES) {
      source.addEventListener(type, (event) => onMessage(JSON.parse((event as MessageEvent<string>).data)));
    }
    let since: number | undefined;
    source.onopen = () => {
      since = undefined;
      onLink(true);
    };
    source.onerror = () => onLink(false, `the development core at ${this.base} is not answering`, (since ??= Date.now()));
    return () => source.close();
  }

  async send(message: UiMessage) {
    const response = await fetch(`${this.base}/v1/send`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(message),
    });
    if (!response.ok) throw new Error((await response.text()).trim() || response.statusText);
  }
}

/** The development core: ?core=<url>, else VITE_NCD_CORE, else the executor's port. */
export function coreURL(): string {
  const fromQuery = new URLSearchParams(location.search).get("core");
  return (fromQuery ?? import.meta.env.VITE_NCD_CORE ?? "http://127.0.0.1:47810").replace(/\/$/, "");
}

export function createTransport(): Transport {
  return isTauri() ? new TauriTransport() : new HttpTransport(coreURL());
}

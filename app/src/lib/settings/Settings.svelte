<script lang="ts">
  import { untrack } from "svelte";
  import { marks } from "$lib/marks";
  import type { Config, DeepPartial, Edge, PowerConfig, WidgetConfig } from "$lib/protocol";
  import Choice from "./Choice.svelte";
  import Field from "./Field.svelte";
  import { presets } from "./presets";
  import Select from "./Select.svelte";
  import Switch from "./Switch.svelte";

  // Settings (DESIGN.md "Settings"): Widgets, Dock, Start, then each widget
  // that has options. Nothing applies until Save: the draft is compared
  // with the config it started from, only what changed is sent, and the core
  // — which owns config.json — validates it and names the field it refuses.
  // The window chrome is ours (a Cerveau title bar); the gallery shows the
  // same component with an invented config and no window.

  interface Saved { ok: boolean; field?: string; message?: string }
  interface Answer { ok: boolean; message?: string; items?: string[] }
  interface WindowControls {
    drag: (event: PointerEvent) => void;
    minimize: () => void;
    close: () => void;
  }

  let {
    config,
    titles,
    save,
    ask,
    controls,
    platform = "linux",
    debugBuild = false,
  }: {
    config: Config;
    titles: Record<string, string>;
    save: (patch: DeepPartial<Config>) => Promise<Saved>;
    ask: (widget: string, what: string, args?: Record<string, string>) => Promise<Answer>;
    controls?: WindowControls;
    platform?: string;
    /** A development build never touches the login items. */
    debugBuild?: boolean;
  } = $props();

  const IDS = ["claude", "codex", "comfyui", "disk", "docker", "power", "tailscale"] as const;
  type Id = (typeof IDS)[number];

  const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v));

  // The config editing started from, and the draft. A config that changes
  // underneath (the rail's menu turned it horizontal) replaces a clean
  // draft; a dirty one keeps its own changes, and only those are sent.
  // Filled from `config` by the effect below, before the first paint.
  let base = $state<Config>({ dock: { orientation: "vertical", density: "normal", readouts: true } });
  let draft = $state<Config>({ dock: { orientation: "vertical", density: "normal", readouts: true } });
  const dirty = $derived(JSON.stringify(draft) !== JSON.stringify(base));
  $effect.pre(() => {
    const incoming = clone(config);
    untrack(() => {
      if (!dirtyNow()) {
        base = incoming;
        draft = clone(incoming);
      }
    });
  });
  function dirtyNow() {
    return JSON.stringify($state.snapshot(draft)) !== JSON.stringify($state.snapshot(base));
  }

  let errors = $state<Record<string, string>>({});
  let notice = $state("");
  let saving = $state(false);
  let closing = $state(false);

  // ---------------------------------------------------------------- widgets
  const widgetsOf = (c: Config) => (c.widgets ??= {}) as Record<Id, WidgetConfig>;
  const ordered = $derived(
    [...IDS].sort((a, b) => (widgetsOf(draft)[a]?.order ?? 0) - (widgetsOf(draft)[b]?.order ?? 0)),
  );
  function move(id: Id, step: number) {
    const list = [...ordered];
    const i = list.indexOf(id);
    const j = i + step;
    if (j < 0 || j >= list.length) return;
    [list[i], list[j]] = [list[j], list[i]];
    list.forEach((w, k) => (widgetsOf(draft)[w].order = (k + 1) * 10));
    changed();
  }

  // ---------------------------------------------------------------- dock
  const edges: { value: Edge | "free"; label: string }[] = [
    { value: "right", label: "Right" },
    { value: "left", label: "Left" },
    { value: "top", label: "Top" },
    { value: "bottom", label: "Bottom" },
    { value: "free", label: "Where I leave it" },
  ];
  const edge = $derived(draft.dock.position?.edge || "free");
  function setEdge(value: string) {
    draft.dock.position = { ...(draft.dock.position ?? { x: 0, y: 0 }), edge: value === "free" ? "" : (value as Edge) };
    changed();
  }

  // ---------------------------------------------------------------- tailscale
  const tailscale = $derived(draft.widgets?.tailscale);
  let peers = $state<string[] | null>(null);
  let peersNote = $state("");
  let typed = $state("");
  function loadPeers() {
    ask("tailscale", "peers")
      .then((answer) => {
        peers = answer.items ?? [];
        peersNote = answer.message ?? "";
      })
      .catch((error) => (peersNote = String(error?.message ?? error)));
  }
  function setWatchMode(mode: string) {
    if (!draft.widgets?.tailscale) return;
    draft.widgets.tailscale.watch = mode === "seen" ? null : (draft.widgets.tailscale.watch ?? []);
    if (mode === "list" && peers === null) loadPeers();
    changed();
  }
  function addPeer(name: string) {
    const host = name.trim();
    const t = draft.widgets?.tailscale;
    if (!host || !t) return;
    t.watch ??= [];
    if (!t.watch.some((h) => h.toLowerCase() === host.toLowerCase())) t.watch.push(host);
    typed = "";
    changed();
  }
  function removePeer(name: string) {
    const t = draft.widgets?.tailscale;
    if (!t?.watch) return;
    t.watch = t.watch.filter((h) => h !== name);
    changed();
  }
  const addable = $derived(
    (peers ?? []).filter((p) => !(tailscale?.watch ?? []).some((h) => h.toLowerCase() === p.toLowerCase())),
  );
  $effect(() => {
    if (tailscale?.watch && peers === null) loadPeers();
  });

  // ---------------------------------------------------------------- power
  const power = $derived(draft.widgets?.power as PowerConfig | undefined);
  function applyPreset(country: string) {
    const p = presets.find((x) => x.country === country);
    const w = draft.widgets?.power;
    if (!p || !w) return;
    w.currency = p.currency;
    w.price_per_kwh = p.flat;
    w.peak_rate = p.peak;
    w.off_peak_rate = p.offPeak;
    w.tou = p.tou;
    notice = `${p.country}'s rates applied — check them against your bill.`;
    changed(false);
  }
  const logIntervals = ["off", "hourly", "daily", "weekly", "monthly"].map((v) => ({ value: v, label: v[0].toUpperCase() + v.slice(1) }));

  // Numbers are typed as text; one that does not read is said at once and
  // blocks Save, and never reaches the core.
  let unreadable = $state<Record<string, string>>({});
  function number(path: string, text: string, set: (v: number | null) => void, { optional = false, integer = false } = {}) {
    const t = text.trim().replace(",", ".");
    if (t === "" && optional) {
      delete unreadable[path];
      set(null);
    } else if (!/^\d+(\.\d+)?$/.test(t) || (integer && !/^\d+$/.test(t))) {
      unreadable[path] = integer ? "Enter a whole number." : "Enter a number, like 0.30.";
      return changed();
    } else {
      delete unreadable[path];
      set(Number(t));
    }
    changed();
  }

  // ---------------------------------------------------------------- comfyui
  const comfy = $derived(draft.widgets?.comfyui);
  let probing = $state(false);
  let probe = $state<{ ok: boolean; text: string } | null>(null);
  let probeSeq = 0;
  async function test() {
    if (!comfy || probing) return;
    const seq = ++probeSeq;
    probing = true;
    probe = null;
    try {
      const answer = await ask("comfyui", "probe", { host: comfy.host.trim(), port: String(comfy.port) });
      if (seq === probeSeq) probe = { ok: answer.ok, text: answer.message ?? "" };
    } catch (error) {
      if (seq === probeSeq) probe = { ok: false, text: error instanceof Error ? error.message : String(error) };
    } finally {
      if (seq === probeSeq) probing = false;
    }
  }

  // ---------------------------------------------------------------- saving
  function changed(clearNotice = true) {
    if (clearNotice) notice = "";
    closing = false;
  }

  /** Only what differs from where editing started. */
  function diff(next: unknown, prev: unknown): unknown {
    if (Array.isArray(next) || Array.isArray(prev) || next === null || prev === null || typeof next !== "object" || typeof prev !== "object") {
      return JSON.stringify(next) === JSON.stringify(prev) ? undefined : next;
    }
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(next as object)) {
      const d = diff((next as Record<string, unknown>)[key], (prev as Record<string, unknown>)[key]);
      if (d !== undefined) out[key] = d;
    }
    return Object.keys(out).length ? out : undefined;
  }

  async function onsave(event?: SubmitEvent) {
    event?.preventDefault();
    if (saving || !dirty || Object.keys(unreadable).length) return;
    const patch = diff($state.snapshot(draft), $state.snapshot(base)) as DeepPartial<Config> | undefined;
    if (!patch) return;
    saving = true;
    errors = {};
    notice = "";
    try {
      const result = await save(patch);
      if (result.ok) {
        base = clone($state.snapshot(draft));
        notice = "Saved.";
      } else if (result.field) {
        errors = { [result.field]: result.message ?? "Refused." };
        notice = "Not saved: one value was refused.";
      } else {
        notice = `Not saved: ${result.message ?? "the core refused it."}`;
      }
    } catch (error) {
      notice = `Not saved: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      saving = false;
    }
  }

  function discard() {
    draft = clone($state.snapshot(base));
    errors = {};
    unreadable = {};
    probe = null;
    notice = "Changes discarded.";
    closing = false;
  }

  function requestClose() {
    if (dirty) closing = true;
    else controls?.close();
  }

  const err = (path: string) => unreadable[path] ?? errors[path];
  const titleOf = (id: string) => titles[id] ?? id;
  const markOf = (id: string) => marks[id];
</script>

<div class="Settings">
  <header class="Settings-bar" onpointerdown={(event) => controls?.drag(event)} role="presentation">
    <svg class="Settings-glyph" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h10M18 7h2M4 17h4M12 17h8" /><circle cx="16" cy="7" r="2" /><circle cx="10" cy="17" r="2" /></svg>
    <h1>Settings</h1>
    {#if controls}
      <div class="Settings-controls" onpointerdown={(event) => event.stopPropagation()} role="presentation">
        <button type="button" class="Settings-ctl" aria-label="Minimise" title="Minimise" onclick={() => controls.minimize()}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 10.5h9" /></svg>
        </button>
        <button type="button" class="Settings-ctl close" aria-label="Close" title="Close" onclick={requestClose}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 4 8 8m0-8-8 8" /></svg>
        </button>
      </div>
    {/if}
  </header>

  <form class="Settings-form" onsubmit={onsave}>
    <div class="Settings-body">
      <section aria-labelledby="s-widgets">
        <h2 id="s-widgets">Widgets</h2>
        <ul class="Widgets">
          {#each ordered as id, i (id)}
            {@const w = widgetsOf(draft)[id]}
            {@const mark = markOf(id)}
            <li class="Widget" class:is-off={!w?.enabled}>
              <span class="Widget-mark" aria-hidden="true">
                {#if mark}<svg viewBox={mark.viewBox}><path d={mark.d} fill-rule={mark.rule} transform-origin="center" style:transform="scale({mark.scale})" /></svg>{/if}
              </span>
              <span class="Widget-name">{titleOf(id)}</span>
              <span class="Widget-order">
                <button type="button" class="Icon" aria-label="Move {titleOf(id)} up" disabled={i === 0} onclick={() => move(id, -1)}>
                  <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 10 4-4 4 4" /></svg>
                </button>
                <button type="button" class="Icon" aria-label="Move {titleOf(id)} down" disabled={i === ordered.length - 1} onclick={() => move(id, 1)}>
                  <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg>
                </button>
              </span>
              {#if w}<Switch bind:checked={w.enabled} label="Show {titleOf(id)}" onchange={() => changed()} />{/if}
            </li>
          {/each}
        </ul>
      </section>

      <section aria-labelledby="s-dock">
        <h2 id="s-dock">Dock</h2>
        <div class="Line">
          <span class="Line-label">Orientation</span>
          <Choice label="Orientation" bind:value={draft.dock.orientation} onchange={() => changed()}
            options={[{ value: "vertical", label: "Vertical" }, { value: "horizontal", label: "Horizontal" }]} />
        </div>
        <div class="Line">
          <span class="Line-label">Size</span>
          <Choice label="Size" bind:value={draft.dock.density} onchange={() => changed()}
            options={[{ value: "compact", label: "Compact" }, { value: "normal", label: "Normal" }, { value: "large", label: "Large" }]} />
        </div>
        <div class="Line">
          <span class="Line-label">Readouts under the dials{#if draft.dock.density === "compact"}<span class="Line-hint">Compact has none</span>{/if}</span>
          <Switch bind:checked={draft.dock.readouts} label="Readouts under the dials" disabled={draft.dock.density === "compact"} onchange={() => changed()} />
        </div>
        <div class="Grid">
          <Field label="Edge" hint="where the rail sits" error={err("dock.position.edge")}>
            <Select label="Edge" value={edge} options={edges} onchange={setEdge} />
          </Field>
        </div>
      </section>

      <section aria-labelledby="s-start">
        <h2 id="s-start">Start</h2>
        <div class="Line">
          <span class="Line-label">Launch NoClickDock at login{#if debugBuild}<span class="Line-hint">a development build leaves your login items alone</span>{/if}</span>
          {#if draft.start}<Switch bind:checked={draft.start.launchAtLogin} label="Launch NoClickDock at login" onchange={() => changed()} />{/if}
        </div>
      </section>

      {#if tailscale}
        <section aria-labelledby="s-tailscale">
          <h2 id="s-tailscale">Tailscale</h2>
          <div class="Line">
            <span class="Line-label">Watch<span class="Line-hint">a watched peer going offline is a warning</span></span>
            <Choice label="Watch" value={tailscale.watch === null ? "seen" : "list"} onchange={setWatchMode}
              options={[{ value: "seen", label: "Peers seen online" }, { value: "list", label: "Only these" }]} />
          </div>
          {#if tailscale.watch !== null}
            <ul class="Peers" aria-label="Watched peers">
              {#each tailscale.watch as host (host)}
                <li class="Peer">
                  <span>{host}</span>
                  <button type="button" class="Icon small" aria-label="Stop watching {host}" onclick={() => removePeer(host)}>
                    <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m5 5 6 6m0-6-6 6" /></svg>
                  </button>
                </li>
              {:else}
                <li class="Peers-empty">No peer is watched: no peer going offline is a warning.</li>
              {/each}
            </ul>
            <div class="Grid">
              <Field label="Add a peer seen" hint={peersNote} error={err("widgets.tailscale.watch")}>
                <Select label="Add a peer seen" value="" placeholder={peers === null ? "Asking Tailscale…" : addable.length ? "Choose a peer…" : "Every peer is watched"}
                  options={addable.map((p) => ({ value: p, label: p }))} onchange={(p) => addPeer(p)} />
              </Field>
              <Field label="Or by name" hint="its MagicDNS name or host name" id="peer-name">
                <div class="Inline">
                  <input id="peer-name" bind:value={typed} autocomplete="off" spellcheck="false" placeholder="office-pc"
                    onkeydown={(event) => { if (event.key === "Enter") { event.preventDefault(); addPeer(typed); } }} />
                  <button type="button" class="Pill" disabled={!typed.trim()} onclick={() => addPeer(typed)}>Add</button>
                </div>
              </Field>
            </div>
          {/if}
        </section>
      {/if}

      {#if power}
        <section aria-labelledby="s-power">
          <h2 id="s-power">Power</h2>
          <div class="Line">
            <span class="Line-label">Tariff</span>
            <Choice label="Tariff" value={power.tou ? "tou" : "flat"} onchange={(v) => { power.tou = v === "tou"; changed(); }}
              options={[{ value: "flat", label: "Flat" }, { value: "tou", label: "Time of use" }]} />
          </div>
          <div class="Grid">
            <Field label="Country preset" hint="all-in household rates, 2026">
              <Select label="Country preset" value="" placeholder="Apply a country's rates…"
                options={presets.map((p) => ({ value: p.country, label: `${p.country} · ${p.flat} ${p.currency}` }))} onchange={applyPreset} />
            </Field>
            <Field label="Currency" id="currency" error={err("widgets.power.currency")}>
              <input id="currency" bind:value={power.currency} maxlength="5" oninput={() => changed()} />
            </Field>
            <Field label={power.tou ? "Flat rate" : "Price per kWh"} hint={power.tou ? "if a rate below is empty" : undefined} id="flat" error={err("widgets.power.price_per_kwh")}>
              <input id="flat" class="mono" inputmode="decimal" value={power.price_per_kwh}
                oninput={(e) => number("widgets.power.price_per_kwh", e.currentTarget.value, (v) => (power.price_per_kwh = v ?? 0))} />
            </Field>
            {#if power.tou}
              <Field label="Peak rate" hint="per kWh" id="peak" error={err("widgets.power.peak_rate")}>
                <input id="peak" class="mono" inputmode="decimal" value={power.peak_rate ?? ""} placeholder={String(power.price_per_kwh)}
                  oninput={(e) => number("widgets.power.peak_rate", e.currentTarget.value, (v) => (power.peak_rate = v), { optional: true })} />
              </Field>
              <Field label="Off-peak rate" hint="per kWh" id="offpeak" error={err("widgets.power.off_peak_rate")}>
                <input id="offpeak" class="mono" inputmode="decimal" value={power.off_peak_rate ?? ""} placeholder={String(power.price_per_kwh)}
                  oninput={(e) => number("widgets.power.off_peak_rate", e.currentTarget.value, (v) => (power.off_peak_rate = v), { optional: true })} />
              </Field>
              <Field label="Peak hours" hint="from–to, like 6-13,15-22" id="hours" error={err("widgets.power.peak_hours")}>
                <input id="hours" class="mono" bind:value={power.peak_hours} spellcheck="false" oninput={() => changed()} />
              </Field>
            {/if}
          </div>
          {#if power.tou}
            <div class="Line">
              <span class="Line-label">Weekends are off-peak</span>
              <Switch bind:checked={power.tou_weekend_offpeak} label="Weekends are off-peak" onchange={() => changed()} />
            </div>
          {/if}
          <h3>Estimates</h3>
          <div class="Grid">
            <Field label="Board, fans and PSU" hint="W, added to every total" id="overhead" error={err("widgets.power.overhead_watts")}>
              <input id="overhead" class="mono" inputmode="decimal" value={power.overhead_watts}
                oninput={(e) => number("widgets.power.overhead_watts", e.currentTarget.value, (v) => (power.overhead_watts = v ?? 0))} />
            </Field>
            <Field label="USB peripherals" hint="W, once for all" id="periph" error={err("widgets.power.peripheral_watts")}>
              <input id="periph" class="mono" inputmode="decimal" value={power.peripheral_watts}
                oninput={(e) => number("widgets.power.peripheral_watts", e.currentTarget.value, (v) => (power.peripheral_watts = v ?? 0))} />
            </Field>
            <Field label="Read every" hint="seconds" id="poll" error={err("widgets.power.poll_interval")}>
              <input id="poll" class="mono" inputmode="numeric" value={power.poll_interval}
                oninput={(e) => number("widgets.power.poll_interval", e.currentTarget.value, (v) => (power.poll_interval = v ?? 0), { integer: true })} />
            </Field>
            <Field label="Log" hint="to Documents/PM-Log" error={err("widgets.power.log_interval")}>
              <Select label="Log" bind:value={power.log_interval} options={logIntervals} onchange={() => changed()} />
            </Field>
          </div>
        </section>
      {/if}

      {#if comfy}
        <section aria-labelledby="s-comfyui">
          <div class="Section-head">
            <h2 id="s-comfyui">ComfyUI</h2>
            <button type="button" class="Pill" disabled={probing || !comfy.host.trim()} onclick={test}>{probing ? "Testing…" : "Test"}</button>
          </div>
          <div class="Grid endpoint">
            <Field label="Host" hint="this machine: 127.0.0.1" id="host" error={err("widgets.comfyui.host")}>
              <input id="host" class="mono" bind:value={comfy.host} spellcheck="false" autocomplete="off" oninput={() => { probe = null; changed(); }} />
            </Field>
            <Field label="Port" hint={platform === "windows" ? "Desktop: 8000" : undefined} id="port" error={err("widgets.comfyui.port")}>
              <input id="port" class="mono" inputmode="numeric" value={comfy.port}
                oninput={(e) => { probe = null; number("widgets.comfyui.port", e.currentTarget.value, (v) => (comfy.port = v ?? 0), { integer: true }); }} />
            </Field>
          </div>
          {#if probe}
            <p class="Probe" class:is-bad={!probe.ok} role={probe.ok ? "status" : "alert"}>{probe.text}</p>
          {/if}
        </section>
      {/if}
    </div>

    <footer class="Settings-foot">
      <p class="Settings-note" aria-live="polite">
        {#if closing}Discard unsaved changes and close?
        {:else if Object.keys(unreadable).length}Fix the highlighted value to save.
        {:else if notice}{notice}
        {:else if dirty}Unsaved changes{/if}
      </p>
      {#if closing}
        <button type="button" class="Pill" onclick={() => (closing = false)}>Keep editing</button>
        <button type="button" class="Pill danger" onclick={() => controls?.close()}>Discard and close</button>
      {:else}
        <button type="button" class="Pill" disabled={!dirty || saving} onclick={discard}>Discard</button>
        <button type="submit" class="Pill primary" disabled={!dirty || saving || Object.keys(unreadable).length > 0}>{saving ? "Saving…" : "Save"}</button>
      {/if}
    </footer>
  </form>
</div>

<style>
  .Settings {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    background: var(--s1);
    color: var(--text);
    font: 500 var(--fs-body)/1.5 var(--font-sans);
  }
  svg { display: block; fill: none; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }

  /* Cerveau's title bar: the drag region, the name, minimise and close. */
  .Settings-bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 40px;
    padding: 0 6px 0 16px;
    border-bottom: 1px solid var(--line);
    background: var(--bg);
    user-select: none;
    -webkit-user-select: none;
  }
  .Settings-glyph { width: 16px; height: 16px; color: var(--dim); }
  h1 { flex: 1; margin: 0; font: 600 var(--fs-body)/20px var(--font-sans); }
  .Settings-controls { display: flex; gap: 2px; }
  .Settings-ctl {
    width: 30px;
    height: 28px;
    padding: 6px 7px;
    border: 1px solid transparent;
    border-radius: var(--r);
    background: transparent;
    color: var(--dim);
    cursor: pointer;
  }
  .Settings-ctl:hover { background: var(--s3); border-color: var(--line2); color: var(--text); }
  .Settings-ctl.close:hover { background: var(--accent); border-color: var(--accent); color: var(--on-accent); }

  .Settings-form { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .Settings-body { flex: 1; min-height: 0; overflow-y: auto; padding: 4px 24px 24px; scrollbar-gutter: stable; }
  section { padding: 20px 0 4px; border-top: 1px solid var(--line); }
  section:first-child { border-top: 0; }
  h2 { margin: 0 0 12px; font: 600 var(--fs-title)/20px var(--font-sans); }
  h3 { margin: 16px 0 10px; font: 600 var(--fs-body)/20px var(--font-sans); color: var(--muted); }
  .Section-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
  .Section-head h2 { margin: 0; }

  .Widgets { margin: 0; padding: 0; list-style: none; }
  .Widget {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 40px;
    border-bottom: 1px solid var(--line);
  }
  .Widget:last-child { border-bottom: 0; }
  .Widget-mark { width: 18px; height: 18px; flex: none; color: var(--text); }
  .Widget-mark svg { width: 18px; height: 18px; fill: currentColor; stroke: none; }
  .Widget-name { flex: 1; min-width: 0; line-height: 20px; }
  .Widget.is-off .Widget-name, .Widget.is-off .Widget-mark { color: var(--dim); }
  .Widget-order { display: flex; gap: 2px; }

  .Line { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 40px; }
  .Line-label { display: flex; flex-direction: column; line-height: 20px; }
  .Line-hint { font: 400 var(--fs-small)/16px var(--font-sans); color: var(--dim); }

  .Grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px 16px; margin: 8px 0 12px; }
  .Grid.endpoint { grid-template-columns: 2fr 1fr; }
  .Inline { display: flex; gap: 8px; }

  .Peers { display: flex; flex-wrap: wrap; gap: 6px; margin: 4px 0 8px; padding: 0; list-style: none; }
  .Peer {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 28px;
    padding: 0 4px 0 10px;
    border: 1px solid var(--line2);
    border-radius: 999px;
    background: var(--s2);
    line-height: 20px;
  }
  .Peers-empty { color: var(--dim); line-height: 20px; }
  .Probe { margin: 0 0 12px; font: 400 var(--fs-small)/16px var(--font-mono); color: var(--muted); overflow-wrap: anywhere; }
  .Probe.is-bad { color: var(--err); }

  .Icon {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--r);
    background: transparent;
    color: var(--dim);
    cursor: pointer;
  }
  .Icon svg { width: 14px; height: 14px; }
  .Icon.small { width: 20px; height: 20px; border-radius: 999px; }
  .Icon:hover:not(:disabled) { background: var(--s3); border-color: var(--line2); color: var(--text); }
  .Icon:disabled { color: var(--line2); cursor: default; }

  /* Pill buttons (DESIGN: Save / Discard footer). */
  .Pill {
    flex: none;
    height: 32px;
    padding: 0 16px;
    border: 1px solid var(--line2);
    border-radius: 999px;
    background: var(--s2);
    color: var(--text);
    font: 600 var(--fs-small)/1 var(--font-sans);
    cursor: pointer;
    transition: background var(--t-fast), border-color var(--t-fast);
  }
  .Inline .Pill { height: 38px; }
  .Pill:hover:not(:disabled) { background: var(--s3); border-color: var(--faint); }
  .Pill:disabled { color: var(--faint); border-color: var(--line); cursor: default; }
  .Pill.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); }
  .Pill.primary:hover:not(:disabled) { background: color-mix(in srgb, var(--accent) 85%, white); }
  .Pill.primary:disabled { background: var(--accent-soft); border-color: var(--accent-line); color: var(--dim); }
  .Pill.danger { color: var(--err); }

  .Settings-foot {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 24px;
    border-top: 1px solid var(--line);
    background: var(--bg);
  }
  .Settings-note { flex: 1; min-width: 0; margin: 0; font: 400 var(--fs-small)/16px var(--font-sans); color: var(--muted); }
</style>

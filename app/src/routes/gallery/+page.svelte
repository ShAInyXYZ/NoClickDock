<script lang="ts">
  import { onMount, tick } from "svelte";
  import { core } from "$lib/core.svelte";
  import Dial from "$lib/dock/Dial.svelte";
  import Rail from "$lib/dock/Rail.svelte";
  import type { Side } from "$lib/dock/side";
  import Panel from "$lib/panel/Panel.svelte";
  import type { State, Tone, WidgetSnapshot } from "$lib/protocol";
  import ToastStack from "$lib/toast/ToastStack.svelte";
  import { coreURL } from "$lib/transport";

  // Every surface at once, from a development core (`ncd-core --mock` for
  // invented data): the rail in each orientation and density, every dial
  // state, every page of every panel, the footer's error and stale states,
  // and a toast stack. This is what the design review shoots
  // (docs/rewrite/design/shoot.mjs) and what README images are made from.
  //
  // Contract: each capturable surface carries data-surface="<kind>/<id>",
  // <html> gets data-gallery-ready once fonts and every widget have drawn,
  // and ?surface=<id> renders that surface alone.
  //
  // Dial specimens, the error panel, the stale panel and the toast stack are
  // the mock's snapshots with one thing forced (a state, an error, staleness)
  // so every state can be judged without waiting for it to happen.

  const only = new URLSearchParams(location.search).get("surface");
  const show = (id: string) => !only || only === id;

  let open = $state<string | null>(null);
  const pages = $state<Record<string, string>>({});

  const byId = $derived(Object.fromEntries(core.widgets.map((w) => [w.id, w])));
  const pick = (state: State) => core.widgets.find((w) => w.state === state);

  interface Specimen { name: string; snapshot: WidgetSnapshot; open?: boolean; side?: Side; hover?: boolean; focus?: boolean; stale?: boolean }
  const specimens = $derived.by(() => {
    const ok = pick("ok");
    const busy = pick("busy");
    const out: (Specimen | undefined)[] = [
      ok && { name: "ok", snapshot: ok },
      pick("warn") && { name: "warn", snapshot: pick("warn")! },
      pick("crit") && { name: "crit", snapshot: pick("crit")! },
      busy && { name: "busy", snapshot: busy },
      busy && { name: "spin", snapshot: { ...busy, progress: undefined } },
      ok && { name: "idle", snapshot: { ...ok, state: "idle", stateLabel: "not running", glance: { text: "off", tone: "idle" as Tone } } },
      ok && { name: "hover", snapshot: ok, hover: true },
      ok && { name: "open", snapshot: ok, open: true, side: "top" },
      ok && { name: "focus", snapshot: ok, focus: true },
      pick("crit") && { name: "stale", snapshot: pick("crit")!, stale: true },
    ];
    return out.filter((s): s is Specimen => !!s);
  });

  // One panel surface per page; a single-page panel is just panel/<widget>.
  const panels = $derived(
    core.widgets.flatMap((w) =>
      w.pages.length > 1
        ? w.pages.map((p) => ({ id: `panel/${w.id}/${p.id}`, snapshot: w, page: p.id }))
        : [{ id: `panel/${w.id}`, snapshot: w, page: w.pages[0]?.id }],
    ),
  );
  const failing = $derived(
    byId.disk && { ...byId.disk, error: "statfs /mnt/offsite: gave up after 60s", state: "idle" as State, stateLabel: "unavailable" },
  );
  const quiet = $derived(pick("crit"));
  const toasts = $derived(
    core.toasts.length
      ? core.toasts.slice(-3).reverse().map((t) => ({ key: t.key, tone: t.tone, text: t.text }))
      : core.widgets
          .filter((w) => w.state !== "ok")
          .slice(0, 3)
          .map((w) => ({ key: w.id, tone: (w.state === "busy" ? "info" : w.state) as Tone, text: `${w.title}: ${w.stateLabel}` })),
  );

  onMount(() => {
    const disconnect = core.connect();
    return () => {
      disconnect.then((stop) => stop());
    };
  });

  // Ready once the fonts are in and every widget the core runs has drawn.
  let ready = false;
  $effect(() => {
    if (ready || !core.expected.length || core.expected.some((id) => !core.snapshots[id])) return;
    ready = true;
    document.fonts.ready.then(tick).then(() => document.documentElement.setAttribute("data-gallery-ready", ""));
  });
</script>

<svelte:head><title>NoClickDock · gallery</title></svelte:head>

<main class:alone={!!only}>
  {#if !only}
    <header class="Sheet-head">
      <h1>NoClickDock</h1>
      <span class="Sheet-note">gallery · {coreURL()} · {core.linked ? "linked" : "not linked"} · {core.widgets.length} widgets</span>
    </header>
  {/if}

  {#if core.config && core.widgets.length}
    <section class="Sheet-row">
      {#if show("dock/v-normal")}
        <figure data-surface="dock/v-normal">
          <Rail widgets={core.widgets} {open} side="right" onhover={(w) => (open = w)} />
        </figure>
      {/if}
      {#if show("dock/v-compact")}
        <figure data-surface="dock/v-compact"><Rail widgets={core.widgets} density="compact" /></figure>
      {/if}
      {#if show("dock/v-large")}
        <figure data-surface="dock/v-large"><Rail widgets={core.widgets} density="large" /></figure>
      {/if}
      <div class="Sheet-col">
        {#if show("dock/h-normal")}
          <figure data-surface="dock/h-normal"><Rail widgets={core.widgets} orientation="horizontal" side="top" /></figure>
        {/if}
        {#if show("dock/h-compact")}
          <figure data-surface="dock/h-compact">
            <Rail widgets={core.widgets} orientation="horizontal" density="compact" side="top" />
          </figure>
        {/if}
        {#if !only || only.startsWith("dial/")}
          <figure data-surface="dial/states">
            <Rail widgets={[]} orientation="horizontal">
              {#each specimens as s (s.name)}
                <div class="Specimen" data-surface="dial/{s.name}">
                  <Dial snapshot={s.snapshot} open={s.open} side={s.side} hover={s.hover} focus={s.focus} stale={s.stale} />
                  <span class="Specimen-name">{s.name}</span>
                </div>
              {/each}
            </Rail>
          </figure>
        {/if}
        {#if show("toast/stack") && toasts.length}
          <figure data-surface="toast/stack"><ToastStack {toasts} /></figure>
        {/if}
      </div>
    </section>

    <section class="Sheet-panels">
      {#each panels as p (p.id)}
        {#if show(p.id)}
          <div class="Sheet-panel" class:is-open={open === p.snapshot.id} data-surface={p.id}>
            <Panel snapshot={p.snapshot} page={p.page} />
          </div>
        {/if}
      {/each}
      {#if failing && show("panel/disk/collector-error")}
        <div class="Sheet-panel" data-surface="panel/disk/collector-error"><Panel snapshot={failing} /></div>
      {/if}
      {#if quiet && show(`panel/${quiet.id}/stale`)}
        <div class="Sheet-panel" data-surface="panel/{quiet.id}/stale"><Panel snapshot={quiet} stale /></div>
      {/if}
    </section>
  {/if}
</main>

<style>
  /* Sheet chrome, not product: plates on the app's own background. */
  main {
    display: flex;
    flex-direction: column;
    gap: 32px;
    padding: 24px 32px 64px;
  }
  main.alone { padding: 16px; }
  .Sheet-head {
    display: flex;
    align-items: baseline;
    gap: 12px;
    height: 48px;
    border-bottom: 1px solid var(--line);
  }
  h1 { font: 600 var(--fs-title)/1 var(--font-sans); }
  .Sheet-note { font: 400 var(--fs-micro)/1 var(--font-mono); letter-spacing: .04em; color: var(--dim); }
  .Sheet-row { display: flex; align-items: flex-start; gap: 40px; }
  .Sheet-col { display: flex; flex-direction: column; align-items: flex-start; gap: 32px; }
  figure { margin: 0; display: inline-flex; padding: 1px; }
  .Specimen { display: flex; flex-direction: column; align-items: center; gap: 6px; min-width: 52px; }
  .Specimen-name {
    font: 500 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .12em;
    text-transform: uppercase;
    color: var(--dim);
  }
  .Sheet-panels { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 24px; }
  .Sheet-panel { padding: 1px; border-radius: var(--r-panel); }
  .Sheet-panel.is-open { box-shadow: 0 0 0 2px var(--accent-line); }
</style>

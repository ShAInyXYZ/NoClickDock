<script lang="ts">
  import Led from "$lib/kit/Led.svelte";
  import { marks } from "$lib/marks";
  import type { State, Tone, WidgetSnapshot } from "$lib/protocol";
  import Readout from "./Readout.svelte";
  import type { Side } from "./side";

  // One widget on the rail, after DeadWhispr's microphone: a machined rim, a
  // recess cut into it holding the widget's mark in monochrome, and a square
  // lamp set into the bezel at 45°. Only a problem (crit) or work in
  // progress (busy) draws a ring; a healthy dial never moves. A widget the
  // core has not heard from for three polls is stale: no ring, lamp idle.
  let {
    snapshot,
    open = false,
    side = "right",
    readout = true,
    stale = false,
    hover = false,
    focus = false,
    onenter,
    onleave,
  }: {
    snapshot: WidgetSnapshot;
    open?: boolean;
    side?: Side;
    readout?: boolean;
    stale?: boolean;
    /** Drawn as if pointed at or focused: the gallery's specimens only. */
    hover?: boolean;
    focus?: boolean;
    onenter?: () => void;
    onleave?: () => void;
  } = $props();

  const LAMP: Record<State, Tone> = { ok: "ok", warn: "warn", crit: "crit", idle: "idle", busy: "busy" };

  const mark = $derived(marks[snapshot.id]);
  const ring = $derived(!stale && (snapshot.state === "crit" || snapshot.state === "busy"));
  const progress = $derived(
    snapshot.state === "busy" && snapshot.progress != null ? Math.round(Math.min(1, Math.max(0, snapshot.progress)) * 100) : null,
  );
  const readoutTone = $derived<Tone | "stale" | undefined>(
    stale
      ? "stale"
      : snapshot.glance.tone === "warn" || snapshot.glance.tone === "crit"
        ? snapshot.glance.tone
        : snapshot.state === "idle"
          ? "idle"
          : undefined,
  );
</script>

<div class="Slot" class:is-stale={stale} data-widget={snapshot.id}>
  <button
    class="Dial"
    class:is-open={open}
    class:is-stale={stale}
    class:is-hover={hover}
    class:is-focus={focus}
    data-state={snapshot.state}
    data-side={side}
    data-progress={snapshot.state === "busy" && progress === null ? "none" : undefined}
    style:--progress={progress ?? undefined}
    aria-label="{snapshot.title}: {stale ? 'stale' : snapshot.stateLabel}"
    aria-expanded={open}
    onpointerenter={onenter}
    onpointerleave={onleave}
    onfocus={onenter}
    onblur={onleave}
  >
    <span class="Dial-recess"></span>
    {#if mark}
      <svg class="Dial-mark" viewBox={mark.viewBox} style:--mark-scale={mark.scale} aria-hidden="true">
        <path d={mark.d} />
      </svg>
    {/if}
    {#if ring}
      <svg class="Dial-ring" viewBox="0 0 40 40" aria-hidden="true">
        <circle cx="20" cy="20" r="19" pathLength="100" />
      </svg>
    {/if}
    <Led tone={stale ? "idle" : LAMP[snapshot.state]} />
    <span class="Dial-index"></span>
  </button>
  {#if readout}<Readout text={snapshot.glance.text} tone={readoutTone} />{/if}
</div>

<style>
  .Slot {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
  }

  .Dial {
    position: relative;
    display: block;
    flex: none;
    width: var(--dial);
    height: var(--dial);
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: var(--s2);
    box-shadow: var(--elev-1);
    color: var(--muted);
    cursor: default;
    font: inherit;
  }
  /* A cherry ring around a 40px dial reads as the crit ring: focus is ink. */
  .Dial:focus-visible, .Dial.is-focus { outline: 2px solid var(--text); outline-offset: 2px; }
  .Dial:hover, .Dial.is-hover { color: var(--text); }
  /* Not running: the mark steps back. */
  .Dial[data-state="idle"], .Dial.is-stale { color: var(--dim); }
  .Dial[data-state="idle"]:hover { color: var(--muted); }

  .Dial-recess {
    position: absolute;
    inset: var(--rim);
    border-radius: 50%;
    background: var(--bg);
    box-shadow: var(--recess-shade);
  }

  .Dial-mark {
    position: absolute;
    left: 50%;
    top: 50%;
    width: calc(var(--mark) * var(--mark-scale, 1));
    height: calc(var(--mark) * var(--mark-scale, 1));
    transform: translate(-50%, -50%);
    fill: currentColor;
    transition: color var(--t-fast) var(--ease-mech);
  }

  /* The lamp sits on the 45° radius, pulled in until its outer corner is 1px
     inside the outline; the dark gasket sets it into the bezel. */
  .Dial > :global(.Led) {
    position: absolute;
    top: calc(var(--dial) / 2 - (var(--dial) / 2 - 1px - var(--led) * .7071) * .7071 - var(--led) / 2);
    right: calc(var(--dial) / 2 - (var(--dial) / 2 - 1px - var(--led) * .7071) * .7071 - var(--led) / 2);
    box-shadow: 0 0 0 1px rgba(0,0,0,.55);
  }

  /* Rings: 2px on the outer edge of the rim. Arcs start at 12 o'clock. */
  .Dial-ring {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    overflow: visible;
    pointer-events: none;
    transform: rotate(-90deg);
  }
  .Dial-ring circle { fill: none; stroke-width: 2; }
  .Dial[data-state="crit"] .Dial-ring circle {
    stroke: var(--err);
    animation: breathe 2.4s var(--ease-mech) infinite;
  }
  .Dial[data-state="busy"] .Dial-ring circle {
    stroke: var(--info);
    stroke-linecap: butt;
    stroke-dasharray: var(--progress, 25) 100;
    transition: stroke-dasharray var(--t-slow) var(--ease-out);
  }
  .Dial[data-state="busy"][data-progress="none"] .Dial-ring { animation: spin 1.2s linear infinite; }
  @keyframes breathe { 0%, 100% { opacity: 1; } 50% { opacity: .3; } }
  @keyframes spin { from { transform: rotate(-90deg); } to { transform: rotate(270deg); } }

  /* Open: the recess one step darker, the mark lit, and the dock's only
     accent — an index tick on the rail wall facing the panel, off the rim. */
  .Dial.is-open { color: var(--text); }
  .Dial.is-open .Dial-recess { background: var(--recess-open); }
  .Dial-index {
    position: absolute;
    display: none;
    background: var(--accent);
    border-radius: 1px;
  }
  .Dial.is-open .Dial-index { display: block; }
  .Dial[data-side="right"]  .Dial-index { width: 2px; height: 10px; top: calc(50% - 5px); right: -6px; }
  .Dial[data-side="left"]   .Dial-index { width: 2px; height: 10px; top: calc(50% - 5px); left: -6px; }
  .Dial[data-side="top"]    .Dial-index { width: 10px; height: 2px; left: calc(50% - 5px); top: -6px; }
  .Dial[data-side="bottom"] .Dial-index { width: 10px; height: 2px; left: calc(50% - 5px); bottom: -6px; }

  /* Reduced motion: the crit ring stays, static; the arc stops. */
  @media (prefers-reduced-motion: reduce) {
    .Dial[data-state="crit"] .Dial-ring circle { animation: none; opacity: 1; }
    .Dial[data-state="busy"][data-progress="none"] .Dial-ring { animation: none; }
  }
</style>

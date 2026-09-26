<script lang="ts">
  import Led from "$lib/kit/Led.svelte";
  import { marks } from "$lib/marks";
  import type { Tone, WidgetSnapshot } from "$lib/protocol";

  // One 48px row: the mark, the title, and the state in words beside its
  // lamp. Nothing else — freshness lives in the footer. Healthy is quiet:
  // an ok state reads in ink, not in green.
  let { snapshot, stale = false }: { snapshot: WidgetSnapshot; stale?: boolean } = $props();

  const mark = $derived(marks[snapshot.id]);
  const tone = $derived<Tone>(stale ? "idle" : snapshot.state === "busy" ? "info" : snapshot.state);
</script>

<div class="PanelHeader">
  {#if mark}
    <svg class="PanelHeader-mark" viewBox={mark.viewBox} aria-hidden="true"><path d={mark.d} fill-rule={mark.rule} /></svg>
  {/if}
  <span class="PanelHeader-title">{snapshot.title}</span>
  <span class="PanelHeader-state" data-tone={tone}>
    <Led {tone} /><span class="PanelHeader-label" title={snapshot.stateLabel}>{stale ? "stale" : snapshot.stateLabel}</span>
  </span>
</div>

<style>
  .PanelHeader {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 48px;
    padding: 0 var(--inset);
  }
  .PanelHeader-mark { width: 18px; height: 18px; flex: none; fill: var(--text); }
  .PanelHeader-title {
    font: 600 var(--fs-title)/1 var(--font-sans);
    letter-spacing: -.005em;
    color: var(--text);
    white-space: nowrap;
  }
  .PanelHeader-state {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    font: 600 var(--fs-body)/1 var(--font-sans);
    color: var(--tone);
  }
  /* A 1× line box clips descenders under overflow:hidden ("runninq"). */
  .PanelHeader-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 20px; }
  .PanelHeader-state[data-tone="ok"], .PanelHeader-state[data-tone="idle"] { color: var(--muted); }
</style>

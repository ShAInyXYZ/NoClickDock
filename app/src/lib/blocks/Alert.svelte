<script lang="ts">
  import { actions } from "$lib/actions.svelte";
  import Led from "$lib/kit/Led.svelte";
  import PillButton from "$lib/kit/PillButton.svelte";
  import type { AlertBlock } from "$lib/protocol";

  // A problem in full: what and since when on one 24px line with what to do
  // at its right, the evidence under it in at most two lines. A 6 % wash of
  // its tone, never a coloured bar. It bleeds 8px so lamp and text stay on
  // the panel's inset; its height is always 8 + 24 + 16·n + 8.
  let { block, widget }: { block: AlertBlock; widget: string } = $props();
  const failure = $derived(actions.failureOf(widget, block.actions));
</script>

<div class="Alert" data-tone={block.tone}>
  <Led tone={block.tone} />
  <div class="Alert-line">
    <span class="Alert-title">{block.title}</span>
    {#if block.meta}<span class="Alert-meta">{block.meta}</span>{/if}
  </div>
  <div class="Alert-actions">
    {#each block.actions ?? [] as action, i (`${i}:${action.id}`)}<PillButton {widget} {action} />{/each}
  </div>
  {#if block.body}<div class="Alert-body" class:is-mono={block.mono}>{block.body}</div>{/if}
  {#if failure}<div class="Alert-error" title={failure}>{failure}</div>{/if}
</div>

<style>
  .Alert {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    column-gap: 10px;
    align-items: center;
    margin: 0 -8px 4px;
    padding: 8px;
    border-radius: var(--r);
    background: color-mix(in srgb, var(--tone) 6%, var(--s1));
  }
  .Alert > :global(.Led) { grid-row: 1; }
  .Alert-line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    height: 24px;
    line-height: 24px;
    overflow: hidden;
  }
  .Alert-title {
    flex: none;
    max-width: 60%;
    font: 600 var(--fs-body)/24px var(--font-sans);
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Alert-meta {
    min-width: 0;
    font: 400 var(--fs-small)/24px var(--font-mono);
    color: var(--tone);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Alert-actions { grid-column: 3; grid-row: 1; display: flex; gap: 4px; }
  .Alert-body {
    grid-column: 2 / 4;
    font: 400 var(--fs-small)/16px var(--font-sans);
    color: var(--muted);
    /* A second line (what else is known) shows when the first leaves room. */
    white-space: pre-line;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .Alert-body.is-mono { font-family: var(--font-mono); }
  .Alert-error {
    grid-column: 2 / 4;
    font: 400 var(--fs-small)/16px var(--font-mono);
    color: var(--err);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>

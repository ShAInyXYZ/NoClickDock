<script lang="ts">
  import Led from "$lib/kit/Led.svelte";
  import PillButton from "$lib/kit/PillButton.svelte";
  import type { RowBlock } from "$lib/protocol";

  // name ............ value: an optional lamp on the name line, a quiet line
  // under the name, and the row's actions as ghost pills at the right.
  let { block, widget }: { block: RowBlock; widget: string } = $props();
</script>

<div class="Row">
  {#if block.led}<Led tone={block.led} />{/if}
  <span class="Row-text">
    <span class="Row-name" class:is-mono={block.mono}>{block.name}</span>
    {#if block.sub}<span class="Row-sub">{block.sub}</span>{/if}
  </span>
  {#if block.value}<span class="Row-value" data-tone={block.tone}>{block.value}</span>{/if}
  {#if block.actions?.length}
    <span class="Row-actions">
      {#each block.actions as action (action.id)}<PillButton {widget} {action} />{/each}
    </span>
  {/if}
</div>

<style>
  .Row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: var(--row-h);
    padding: 4px 0;
  }
  /* The lamp belongs to the name line, not to the middle of two lines. */
  .Row > :global(.Led) { align-self: flex-start; margin-top: 10px; }
  /* A hover wash that bleeds 8px, so the text stays on the inset. */
  .Row::before {
    content: '';
    position: absolute;
    inset: 0 -8px;
    border-radius: var(--r);
    background: transparent;
    transition: background var(--t-fast) var(--ease-mech);
    pointer-events: none;
  }
  .Row:hover::before { background: color-mix(in srgb, var(--text) 2.5%, transparent); }
  .Row > * { position: relative; }
  .Row-text { flex: 1; min-width: 0; }
  .Row-name {
    display: block;
    font: 500 var(--fs-body)/18px var(--font-sans);
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Row-name.is-mono { font: 500 var(--fs-small)/18px var(--font-mono); }
  .Row-sub {
    display: block;
    font: 400 var(--fs-small)/14px var(--font-sans);
    color: var(--dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Row-value {
    font: 400 var(--fs-small)/18px var(--font-mono);
    color: var(--muted);
    text-align: right;
    white-space: nowrap;
  }
  .Row-value[data-tone="warn"] { color: var(--warn); }
  .Row-value[data-tone="crit"] { color: var(--err); }
  .Row-actions { display: flex; gap: 4px; }
</style>

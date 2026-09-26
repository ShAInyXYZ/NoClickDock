<script lang="ts">
  import { actions } from "$lib/actions.svelte";
  import Led from "$lib/kit/Led.svelte";
  import PillButton from "$lib/kit/PillButton.svelte";
  import type { RowBlock } from "$lib/protocol";

  // name ............ value: an optional lamp on the name line, a quiet line
  // under the name, and the row's actions as ghost pills at the right. One
  // line, always: a long value ellipsizes, the pills never wrap under it.
  // `mono` means the name is a path (A18).
  let { block, widget }: { block: RowBlock; widget: string } = $props();
  // A failed action says why under the row that ran it (M2-a).
  const failure = $derived(actions.failureOf(widget, block.actions));
</script>

<div class="Row">
  <div class="Row-line">
    <span class="Row-text" class:has-led={!!block.led}>
      {#if block.led}<Led tone={block.led} />{/if}
      <span class="Row-name" class:is-mono={block.mono}>{block.name}</span>
      {#if block.sub}<span class="Row-sub">{block.sub}</span>{/if}
    </span>
    {#if block.value}<span class="Row-value" data-tone={block.tone} title={block.value}>{block.value}</span>{/if}
    {#if block.actions?.length}
      <span class="Row-actions">
        {#each block.actions as action, i (`${i}:${action.id}`)}<PillButton {widget} {action} />{/each}
      </span>
    {/if}
  </div>
  {#if failure}<span class="Row-error" title={failure}>{failure}</span>{/if}
</div>

<style>
  .Row {
    position: relative;
    display: flex;
    flex-direction: column;
    justify-content: center;
    min-height: var(--row-h);
    padding: 4px 0;
  }
  .Row-line {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    column-gap: 10px;
    min-width: 0;
  }
  /* The lamp lives with the name, centred on its 18px line — the same in a
     single-line row and a row with a sub line. */
  .Row-text.has-led { padding-left: 16px; }
  .Row-text > :global(.Led) { position: absolute; left: 0; top: calc((18px - var(--led)) / 2); }
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
  /* The name is the row's identity: it keeps a floor while a long value
     gives way first. */
  .Row-text { position: relative; flex: 1; min-width: 30%; }
  .Row-name {
    display: block;
    font: 500 var(--fs-body)/18px var(--font-sans);
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Row-name.is-mono { font: 400 var(--fs-small)/18px var(--font-mono); }
  .Row-sub {
    display: block;
    font: 400 var(--fs-small)/14px var(--font-sans);
    color: var(--dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Row-value {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font: 400 var(--fs-small)/18px var(--font-mono);
    color: var(--muted);
    text-align: right;
    white-space: nowrap;
  }
  .Row-value[data-tone="warn"] { color: var(--warn); }
  .Row-value[data-tone="crit"] { color: var(--err); }
  .Row-actions { flex: none; display: flex; gap: 4px; }
  .Row-error {
    display: block;
    min-width: 0;
    font: 400 var(--fs-small)/16px var(--font-mono);
    color: var(--err);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>

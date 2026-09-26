<script lang="ts">
  import type { StatsBlock } from "$lib/protocol";

  // One mono line of key value pairs: the numbers a panel used to put in
  // tiles, read in a single glance instead. There are no KPI tiles.
  let { block }: { block: StatsBlock } = $props();
</script>

<p class="Stats">
  {#each block.items as item, i (i)}
    <span class="Stats-item"><span class="Stats-key">{item.key}</span><span class="Stats-value" data-tone={item.tone}>{item.value}</span></span>
  {/each}
</p>

<style>
  .Stats {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    row-gap: 2px;
    min-height: 28px;
    padding: 5px 0;
    font: 400 var(--fs-small)/18px var(--font-mono);
    /* Every item carries its 16px separator in front. The box is pulled
       left by one separator, into the panel's inset, and its leftmost 16px
       are clipped: the separator that starts a line — the first, or one
       that wrapped — is never seen. */
    margin-left: -16px;
    clip-path: inset(0 0 0 16px);
  }
  .Stats-item { white-space: nowrap; }
  .Stats-item::before { content: '·'; display: inline-block; width: 16px; text-align: center; color: var(--faint); }
  .Stats-key { color: var(--dim); margin-right: 6px; }
  .Stats-value { color: var(--text); }
  .Stats-value[data-tone="warn"] { color: var(--warn); }
  .Stats-value[data-tone="crit"] { color: var(--err); }
</style>

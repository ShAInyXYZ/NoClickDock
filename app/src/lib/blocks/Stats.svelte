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
  }
  .Stats-item { white-space: nowrap; }
  .Stats-item + .Stats-item::before { content: '·'; color: var(--faint); margin: 0 8px; }
  .Stats-key { color: var(--dim); margin-right: 6px; }
  .Stats-value { color: var(--text); }
  .Stats-value[data-tone="warn"] { color: var(--warn); }
  .Stats-value[data-tone="crit"] { color: var(--err); }
</style>

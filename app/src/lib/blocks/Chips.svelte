<script lang="ts">
  import Led from "$lib/kit/Led.svelte";
  import type { ChipsBlock, Tone } from "$lib/protocol";
  import Row from "./Row.svelte";

  // Every item of a set at a glance: a lamp and a label, no box. Healthy
  // items stay quiet in the grid; one that is not leaves it and becomes a
  // full row with words, above the grid, where it cannot be missed.
  let { block, widget }: { block: ChipsBlock; widget: string } = $props();

  const problem = (tone?: Tone) => tone === "warn" || tone === "crit";
  const pulled = $derived(block.items.filter((item) => problem(item.tone)));
  const quiet = $derived(block.items.filter((item) => !problem(item.tone)));
</script>

{#each pulled as item (item.label)}
  <Row
    block={{ kind: "row", name: item.label, value: item.value ?? (item.tone === "crit" ? "failing" : "degraded"), tone: item.tone, led: item.tone }}
    {widget}
  />
{/each}
{#if quiet.length}
  <div class="Chips" style:--cols={Math.max(1, block.columns || 3)}>
    {#each quiet as item (item.label)}
      <span class="Chip">
        {#if item.tone}<Led tone={item.tone} />{/if}
        <span class="Chip-label" title={item.label}>{item.label}</span>
        {#if item.value}<span class="Chip-value">{item.value}</span>{/if}
      </span>
    {/each}
  </div>
{/if}

<style>
  .Chips {
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    column-gap: 12px;
    row-gap: 2px;
    padding: 4px 0 6px;
  }
  .Chip {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    height: 24px;
    font: 500 var(--fs-small)/1 var(--font-sans);
    color: var(--muted);
  }
  .Chip-label { min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; line-height: 16px; }
  .Chip-value {
    margin-left: auto;
    font: 400 var(--fs-small)/1 var(--font-mono);
    color: var(--text);
    white-space: nowrap;
  }
</style>

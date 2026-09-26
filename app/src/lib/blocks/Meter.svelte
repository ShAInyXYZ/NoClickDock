<script lang="ts">
  import Seg from "$lib/kit/Seg.svelte";
  import type { MeterBlock } from "$lib/protocol";

  // label · level · value, with the arithmetic spelled out under the level
  // when the core gives it. A meter never stands alone: it is a row of its
  // MeterGroup's grid, so every bar in a run starts on the same x.
  let { block }: { block: MeterBlock } = $props();
</script>

<div class="Meter">
  <span class="Meter-label" class:is-mono={block.mono} title={block.label}>{block.label}</span>
  <Seg frac={block.frac} tone={block.tone} />
  <span class="Meter-value" data-tone={block.tone}>{block.value ?? ""}</span>
  {#if block.note}<span class="Meter-note">{block.note}</span>{/if}
</div>

<style>
  .Meter { display: contents; }
  .Meter-label, .Meter-value {
    display: flex;
    align-items: center;
    height: 28px;
    white-space: nowrap;
  }
  .Meter-label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font: 500 var(--fs-body)/1 var(--font-sans);
    color: var(--muted);
  }
  .Meter-label.is-mono { font: 400 var(--fs-small)/1 var(--font-mono); }
  .Meter-value {
    justify-content: flex-end;
    font: 400 var(--fs-small)/1 var(--font-mono);
    color: var(--text);
  }
  .Meter-value[data-tone="warn"] { color: var(--warn); }
  .Meter-value[data-tone="crit"] { color: var(--err); }
  .Meter-note {
    grid-column: 2 / -1;
    margin-top: -4px;
    padding-bottom: 4px;
    font: 400 var(--fs-micro)/16px var(--font-mono);
    color: var(--dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>

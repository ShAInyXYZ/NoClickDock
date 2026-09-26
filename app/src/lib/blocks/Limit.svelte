<script lang="ts">
  import Seg from "$lib/kit/Seg.svelte";
  import type { LimitBlock } from "$lib/protocol";

  // One plan window: how much is gone and when it comes back. A row of its
  // LimitGroup's grid, so every bar in a run starts on the same x.
  let { block }: { block: LimitBlock } = $props();
  const pct = $derived(Math.min(100, Math.max(0, block.pct)));
</script>

<div class="Limit">
  <span class="Limit-label" title={block.label}>{block.label}</span>
  <Seg frac={pct / 100} tone={block.tone} />
  <span class="Limit-pct" data-tone={block.tone}>{Math.round(pct)}%</span>
  <span class="Limit-resets">{block.resets ?? ""}</span>
</div>

<style>
  .Limit { display: contents; }
  .Limit-label, .Limit-pct, .Limit-resets {
    display: flex;
    align-items: center;
    height: 28px;
    white-space: nowrap;
  }
  .Limit-label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font: 500 var(--fs-body)/1 var(--font-sans);
    color: var(--muted);
  }
  .Limit-pct {
    justify-content: flex-end;
    font: 400 var(--fs-small)/1 var(--font-mono);
    color: var(--text);
  }
  .Limit-pct[data-tone="warn"] { color: var(--warn); }
  .Limit-pct[data-tone="crit"] { color: var(--err); }
  .Limit-resets { font: 400 var(--fs-small)/1 var(--font-mono); color: var(--dim); }
</style>

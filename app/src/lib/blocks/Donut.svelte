<script lang="ts">
  import type { DonutBlock } from "$lib/protocol";

  // One total split by component, on a monochrome ramp — largest first, so
  // brightness ranks the parts and colour stays free for state. The legend
  // is rows.
  let { block }: { block: DonutBlock } = $props();

  const RAMP = ["var(--text)", "var(--muted)", "var(--dim)", "var(--faint)", "var(--line2)"];
  const R = 51;
  const C = 2 * Math.PI * R;
  const GAP = 1.5;

  const parts = $derived(
    block.segments
      .filter((s) => s.value > 0)
      .sort((a, b) => b.value - a.value)
      .map((s, i) => ({ ...s, colour: RAMP[Math.min(i, RAMP.length - 1)] })),
  );
  const arcs = $derived.by(() => {
    const total = parts.reduce((sum, p) => sum + p.value, 0) || 1;
    let offset = 0;
    return parts.map((p) => {
      const length = (p.value / total) * C;
      const arc = { colour: p.colour, dash: `${Math.max(length - GAP, 0.5)} ${C}`, offset: -offset };
      offset += length;
      return arc;
    });
  });
</script>

<div class="Donut">
  <div class="Donut-chart">
    <svg viewBox="0 0 112 112" aria-hidden="true">
      {#each arcs as arc, i (i)}
        <circle cx="56" cy="56" r={R} stroke={arc.colour} stroke-dasharray={arc.dash} stroke-dashoffset={arc.offset} />
      {/each}
    </svg>
    <div class="Donut-center">
      <span class="Donut-value">{block.center}</span>
      {#if block.unit}<span class="Donut-unit">{block.unit}</span>{/if}
    </div>
  </div>
  <div class="Donut-legend">
    {#each parts as part, i (part.label)}
      <div class="Donut-row" class:is-minor={i > 1}>
        <span class="Donut-swatch" style:background={part.colour}></span>
        <span class="Donut-label">{part.label}</span>
        <span class="Donut-amt">{part.display}</span>
      </div>
    {/each}
  </div>
</div>

<style>
  .Donut {
    display: grid;
    grid-template-columns: 112px minmax(0, 1fr);
    column-gap: 20px;
    align-items: center;
    padding: 8px 0 4px;
  }
  .Donut-chart { position: relative; width: 112px; height: 112px; }
  svg { width: 112px; height: 112px; transform: rotate(-90deg); }
  circle { fill: none; stroke-width: 10; }
  .Donut-center {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
  }
  .Donut-value { font: 500 var(--fs-display)/1 var(--font-mono); color: var(--text); letter-spacing: -.01em; }
  .Donut-unit {
    font: 500 var(--fs-micro)/1 var(--font-mono);
    color: var(--dim);
    letter-spacing: .12em;
    text-transform: uppercase;
  }
  .Donut-legend { display: flex; flex-direction: column; min-width: 0; }
  .Donut-row {
    display: grid;
    grid-template-columns: 8px minmax(0, 1fr) auto;
    align-items: center;
    column-gap: 10px;
    height: 24px;
  }
  .Donut-swatch { width: 6px; height: 6px; border-radius: 1px; }
  .Donut-label { font: 500 var(--fs-small)/1 var(--font-sans); color: var(--muted); }
  .Donut-amt { font: 400 var(--fs-small)/1 var(--font-mono); color: var(--text); }
  .Donut-row.is-minor .Donut-amt { color: var(--muted); }
</style>

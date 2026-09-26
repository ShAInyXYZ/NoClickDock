<script lang="ts">
  import type { BarsBlock } from "$lib/protocol";

  // A day by the hour: bars fill the column, past hours in faint ink, the
  // hour in progress lit, hours to come a stub on the floor. Integer rects
  // with crisp edges, like the meters.
  let { block }: { block: BarsBlock } = $props();

  const GAP = 2;
  const HEIGHT = 40;
  let width = $state(0);
  const slots = $derived(Math.max(24, block.values.length));
  const now = $derived(block.highlight ?? block.values.length - 1);
  const bar = $derived(Math.max(1, Math.floor((width - (slots - 1) * GAP) / slots)));
  const drawn = $derived(slots * bar + (slots - 1) * GAP);
  const peak = $derived(Math.max(...block.values, 1));
  const ticks = $derived(slots === 24 ? [0, 6, 12, 18] : []);
</script>

<div class="Bars" bind:clientWidth={width}>
  {#if width > 0}
    <svg
      width={drawn}
      height={HEIGHT}
      viewBox="0 0 {drawn} {HEIGHT}"
      shape-rendering="crispEdges"
      role="img"
      aria-label="{block.values.length} readings{block.unit ? ` in ${block.unit}` : ''}"
    >
      {#each { length: slots }, i}
        {@const x = i * (bar + GAP)}
        {#if i > now || i >= block.values.length}
          <rect class="future" {x} y={HEIGHT - 1} width={bar} height="1" />
        {:else}
          {@const h = Math.max(2, Math.round((block.values[i] / peak) * HEIGHT))}
          <rect class:now={i === now} {x} y={HEIGHT - h} width={bar} height={h} />
        {/if}
      {/each}
    </svg>
    {#if ticks.length}
      <div class="Bars-axis" style:width="{drawn}px">
        {#each ticks as hour (hour)}
          <span style:left="{hour * (bar + GAP)}px">{String(hour).padStart(2, "0")}</span>
        {/each}
        <span class="end">24</span>
      </div>
    {/if}
  {/if}
</div>

<style>
  .Bars { display: flex; flex-direction: column; gap: 4px; padding: 4px 0 8px; min-height: 60px; }
  svg { display: block; }
  rect { fill: var(--faint); }
  rect.now { fill: var(--text); }
  rect.future { fill: var(--line2); }
  .Bars-axis { position: relative; height: 10px; font: 400 var(--fs-micro)/10px var(--font-mono); color: var(--dim); }
  .Bars-axis span { position: absolute; top: 0; transform: translateX(-1px); }
  .Bars-axis .end { right: 0; transform: none; }
</style>

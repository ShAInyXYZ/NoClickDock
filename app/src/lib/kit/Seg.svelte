<script lang="ts">
  import type { Tone } from "$lib/protocol";

  // A segmented level meter, filling its column: 3px segments 2px
  // apart, drawn as integer SVG rects with crisp edges — spans at fractional
  // x snap to 3 or 4 device pixels and the meter looks uneven. Lit segments
  // are ink; only a warn or crit tone from the core colours them.
  let { frac, tone }: { frac: number; tone?: Tone } = $props();

  const SEGMENT = 3;
  const PITCH = 5;
  const HEIGHT = 10;
  let width = $state(0);
  const count = $derived(Math.max(0, Math.floor((Math.floor(width) + PITCH - SEGMENT) / PITCH)));
  const clamped = $derived(Math.min(1, Math.max(0, frac)));
  const lit = $derived(Math.round(clamped * count));
  const drawn = $derived(Math.max(0, count * PITCH - (PITCH - SEGMENT)));
</script>

<div
  class="Seg"
  data-tone={tone}
  bind:clientWidth={width}
  role="meter"
  aria-valuemin={0}
  aria-valuemax={100}
  aria-valuenow={Math.round(clamped * 100)}
>
  {#if count > 0}
    <svg width={drawn} height={HEIGHT} viewBox="0 0 {drawn} {HEIGHT}" shape-rendering="crispEdges" aria-hidden="true">
      {#each { length: count }, i}
        <rect x={i * PITCH} y="0" width={SEGMENT} height={HEIGHT} class:on={i < lit} />
      {/each}
    </svg>
  {/if}
</div>

<style>
  .Seg { height: 10px; min-width: 0; overflow: hidden; }
  svg { display: block; }
  rect { fill: var(--line2); }
  rect.on { fill: var(--muted); }
  .Seg[data-tone="warn"] rect.on { fill: var(--warn); }
  .Seg[data-tone="crit"] rect.on { fill: var(--err); }
</style>

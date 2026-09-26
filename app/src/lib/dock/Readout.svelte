<script lang="ts">
  import type { Tone } from "$lib/protocol";

  // glance.text under a dial: the number the dock exists to show, in ink
  // bright enough to read without leaning in. Coloured only for a problem.
  let { text, tone }: { text: string; tone?: Tone | "stale" } = $props();
</script>

<span class="Readout" data-tone={tone}>{text}</span>

<style>
  .Readout {
    display: block;
    min-width: var(--dial);
    /* Five characters is the core's budget; a longer glance never runs
       into its neighbour. */
    max-width: calc(var(--dial) + var(--slot-gap));
    overflow: hidden;
    font: 500 var(--fs-micro)/var(--readout-h) var(--font-mono);
    letter-spacing: .02em;
    text-align: center;
    white-space: nowrap;
    color: var(--muted);
  }
  .Readout[data-tone="warn"] { color: var(--warn); }
  .Readout[data-tone="crit"] { color: var(--err); }
  .Readout[data-tone="idle"] { color: var(--dim); }
  .Readout[data-tone="stale"] { color: var(--faint); }
</style>

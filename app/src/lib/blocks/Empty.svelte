<script lang="ts">
  import type { EmptyBlock } from "$lib/protocol";

  // What is true when there is nothing to list, in one sentence; a command
  // to run is set apart as a mono chip.
  let { block }: { block: EmptyBlock } = $props();
  const parts = $derived(block.text.split(/`([^`]+)`/));
</script>

<p class="Empty">
  {#each parts as part, i (i)}{#if i % 2}<code>{part}</code>{:else}{part}{/if}{/each}
</p>

<style>
  .Empty { padding: 20px 0 16px; font: 500 var(--fs-body)/20px var(--font-sans); color: var(--muted); }
  code {
    font: 400 var(--fs-small)/20px var(--font-mono);
    color: var(--text);
    padding: 1px 6px;
    background: var(--s2);
    border-radius: var(--r);
    box-shadow: inset 0 0 0 1px var(--line2);
    white-space: nowrap;
  }
</style>

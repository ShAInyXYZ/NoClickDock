<script lang="ts">
  import type { NoteBlock } from "$lib/protocol";

  // A quiet line of supporting text; a command to copy is set in mono.
  let { block }: { block: NoteBlock } = $props();
</script>

<p class="Note" class:is-mono={block.mono} data-tone={block.tone}>{block.text}</p>

<style>
  .Note {
    padding: 6px 0;
    font: 400 var(--fs-small)/16px var(--font-sans);
    color: var(--dim);
    overflow-wrap: anywhere;
  }
  .Note.is-mono { font-family: var(--font-mono); }
  .Note[data-tone="warn"] { color: var(--warn); }
  .Note[data-tone="crit"] { color: var(--err); }
</style>

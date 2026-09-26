<script lang="ts">
  import Led from "$lib/kit/Led.svelte";
  import type { Tone } from "$lib/protocol";

  // One transition worth knowing while you can still act on it: a lamp and
  // one plain sentence, at most two lines.
  let { tone, text, leaving = false }: { tone: Tone; text: string; leaving?: boolean } = $props();
</script>

<div class="Toast" class:is-leaving={leaving} role="status">
  <Led {tone} />
  <span class="Toast-text">{text}</span>
</div>

<style>
  .Toast {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    width: max-content;
    max-width: 280px;
    padding: 9px 14px 9px 12px;
    background: var(--s2);
    border-radius: var(--r-toast);
    box-shadow: var(--edge-2);
    font: 500 var(--fs-body)/18px var(--font-sans);
    color: var(--text);
    transition: opacity 160ms var(--ease-out);
  }
  .Toast > :global(.Led) { margin-top: 6px; }
  .Toast-text {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .Toast.is-leaving { opacity: 0; }
</style>

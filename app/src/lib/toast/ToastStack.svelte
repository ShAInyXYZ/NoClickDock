<script lang="ts">
  import type { Tone } from "$lib/protocol";
  import Toast from "./Toast.svelte";

  // Newest nearest the dial, at most three, 6px apart. The pointer passes
  // through: a toast never blocks the desktop under it.
  let { toasts }: { toasts: { key: number | string; tone: Tone; text: string; leaving?: boolean }[] } = $props();
</script>

<div class="ToastStack" aria-live="polite">
  {#each toasts as toast (toast.key)}
    <Toast tone={toast.tone} text={toast.text} leaving={toast.leaving} />
  {/each}
</div>

<style>
  .ToastStack { display: flex; flex-direction: column; gap: 6px; pointer-events: none; }
</style>

<script lang="ts">
  import { onMount } from "svelte";
  import { core } from "$lib/core.svelte";
  import ToastStack from "$lib/toast/ToastStack.svelte";

  // The toast stack from the core's toasts: each shown five seconds, then a
  // 160 ms fade. The toast window that places it beside its dial, and holds
  // toasts while a panel is open, arrives in M2; this route draws the stack.
  const VISIBLE_MS = 5000;
  const FADE_MS = 160;
  const MAX = 3;
  const shown = $derived(
    core.toasts
      .map((toast) => ({ ...toast, age: core.now - Date.parse(toast.at) }))
      .filter((toast) => toast.age < VISIBLE_MS + FADE_MS)
      .slice(-MAX)
      .reverse()
      .map((toast) => ({ ...toast, leaving: toast.age >= VISIBLE_MS })),
  );

  onMount(() => {
    document.documentElement.classList.add("overlay");
    const disconnect = core.connect();
    return () => {
      disconnect.then((stop) => stop());
    };
  });
</script>

<div class="window"><ToastStack toasts={shown} /></div>

<style>
  .window { display: inline-block; padding: 1px; }
</style>

<script lang="ts">
  import { isTauri } from "@tauri-apps/api/core";
  import { onMount, tick } from "svelte";
  import { core } from "$lib/core.svelte";
  import type { Side } from "$lib/dock/side";
  import { onToasts, report, reportUncaught, toastDismiss, toastLayout, type ToastStackMessage } from "$lib/shell";
  import ToastStack from "$lib/toast/ToastStack.svelte";

  // The toast window. The shell decides what shows and when — five seconds
  // each, waiting while a panel is open — and sends the stack; this page
  // draws it, newest nearest the dial, and answers with its size. In a plain
  // browser (development) it draws the core's latest toasts itself.
  let stack = $state<ToastStackMessage | null>(null);
  let frame = $state<HTMLElement>();
  const side = $derived<Side>(stack?.side ?? "right");

  const VISIBLE_MS = 5000;
  const browserToasts = $derived(
    core.toasts
      .filter((toast) => core.now - Date.parse(toast.at) < VISIBLE_MS)
      .slice(-3)
      .reverse()
      .map((toast) => ({ key: toast.key, tone: toast.tone, text: toast.text })),
  );
  const toasts = $derived(stack ? stack.items : browserToasts);

  async function measure(seq: number) {
    await tick();
    if (!frame || !stack?.items.length) return;
    const box = frame.getBoundingClientRect();
    // The newest toast is the one level with the dial.
    const newest = frame.querySelector<HTMLElement>(".Toast");
    const anchor = newest ? newest.getBoundingClientRect().top - box.top + newest.offsetHeight / 2 : box.height / 2;
    toastLayout(seq, box.width, box.height, anchor).catch(report);
  }

  onMount(() => {
    document.documentElement.classList.add("overlay");
    if (!isTauri()) {
      const disconnect = core.connect();
      return () => {
        disconnect.then((stop) => stop());
      };
    }
    reportUncaught();
    const unlisten = onToasts((next) => {
      stack = next;
      measure(next.seq);
    });
    return () => {
      unlisten.then((stop) => stop());
    };
  });

  // Windows cannot pass the pointer through a webview reliably: there,
  // pointing at a toast dismisses it (on Linux the pointer never arrives).
  const windows = typeof navigator !== "undefined" && navigator.userAgent.includes("Windows");
  function dismiss() {
    if (isTauri() && windows) toastDismiss().catch(report);
  }
</script>

<div class="window" data-side={side} bind:this={frame} onmouseenter={dismiss} role="presentation">
  <ToastStack {toasts} />
</div>

<style>
  .window { display: inline-flex; padding: 1px; }
  /* Above a horizontal rail the newest toast is the lowest one. */
  .window[data-side="top"] :global(.ToastStack) { flex-direction: column-reverse; }
</style>

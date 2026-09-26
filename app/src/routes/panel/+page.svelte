<script lang="ts">
  import { onMount, tick } from "svelte";
  import { core } from "$lib/core.svelte";
  import type { Side } from "$lib/dock/side";
  import Panel from "$lib/panel/Panel.svelte";
  import { hoverPanel, onPanelClose, onPanelOpen, panelLayout, report, reportUncaught } from "$lib/shell";

  // The panel window. The shell says which widget to draw; the page answers
  // with the size that takes, and the shell places and reveals the window.
  let widget = $state<string | null>(null);
  let side = $state<Side>("right");
  let maxHeight = $state<number>();
  let reveals = $state(0);
  let seq = 0;
  let frame = $state<HTMLElement>();
  const pages = $state<Record<string, string>>({});
  const snapshot = $derived(widget ? core.snapshots[widget] : undefined);

  function measure() {
    if (!frame || !widget) return;
    const { width, height } = frame.getBoundingClientRect();
    panelLayout(seq, width, height).catch(report);
  }

  onMount(() => {
    document.documentElement.classList.add("overlay");
    reportUncaught();
    const disconnect = core.connect();
    disconnect.catch(report);
    const unlistenOpen = onPanelOpen(async (open) => {
      widget = open.widget;
      side = open.side;
      // The window keeps 1px of margin on every side for the ring.
      maxHeight = open.maxHeight - 2;
      seq = open.seq;
      if (open.reveal) reveals++;
      await tick();
      measure();
    });
    const unlistenClose = onPanelClose(() => (widget = null));

    const root = document.documentElement;
    const enter = () => hoverPanel(true).catch(report);
    const leave = () => hoverPanel(false).catch(report);
    root.addEventListener("mouseenter", enter);
    root.addEventListener("mouseleave", leave);
    return () => {
      disconnect.then((stop) => stop());
      unlistenOpen.then((stop) => stop());
      unlistenClose.then((stop) => stop());
      root.removeEventListener("mouseenter", enter);
      root.removeEventListener("mouseleave", leave);
    };
  });

  // A snapshot that grows, or a page switch, changes the panel's height.
  $effect(() => {
    if (!frame) return;
    const observer = new ResizeObserver(measure);
    observer.observe(frame);
    return () => observer.disconnect();
  });
</script>

{#if snapshot}
  {#key reveals}
    <div class="window" data-side={side} bind:this={frame}>
      <Panel {snapshot} bind:page={pages[snapshot.id]} {maxHeight} />
    </div>
  {/key}
{/if}

<style>
  .window {
    display: inline-flex;
    padding: 1px;
    animation: reveal 140ms var(--ease-out);
  }
  /* The panel arrives from the rail: a 3px slide away from it. */
  .window[data-side="right"]  { --from: translateX(-3px); }
  .window[data-side="left"]   { --from: translateX(3px); }
  .window[data-side="bottom"] { --from: translateY(-3px); }
  .window[data-side="top"]    { --from: translateY(3px); }
  @keyframes reveal { from { opacity: 0; transform: var(--from); } }
  @media (prefers-reduced-motion: reduce) { .window { animation: none; } }
</style>

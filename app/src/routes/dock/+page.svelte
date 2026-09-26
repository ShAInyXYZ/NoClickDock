<script lang="ts">
  import { onMount, tick } from "svelte";
  import { core, coreDown } from "$lib/core.svelte";
  import Rail from "$lib/dock/Rail.svelte";
  import type { Side } from "$lib/dock/side";
  import {
    dialMenu,
    hoverApproach,
    hoverDial,
    onPanelState,
    railLayout,
    railMenu,
    report,
    reportUncaught,
    startDrag,
  } from "$lib/shell";

  // The rail window. The shell sizes the window to whatever this page
  // measures and opens panels against the dial rectangles reported here.
  // The page keeps 1px of transparent margin so the rail's ring is never
  // clipped by the window edge.
  let frame = $state<HTMLElement>();
  let open = $state<string | null>(null);
  let side = $state<Side>("right");

  // While the core is not answering, the rail still stands, with a dial of
  // its own that says so — never an invisible app.
  const down = $derived(core.linkError ? coreDown(core.linkError) : null);
  const dials = $derived(down ? [down, ...core.widgets] : core.widgets);
  const dock = $derived(core.config?.dock);

  function measure() {
    if (!frame) return;
    const { width, height } = frame.getBoundingClientRect();
    const rects = [...frame.querySelectorAll<HTMLElement>(".Slot[data-widget]")].map((slot) => {
      const dial = slot.querySelector(".Dial")!.getBoundingClientRect();
      return { widget: slot.dataset.widget!, x: dial.x, y: dial.y, width: dial.width, height: dial.height };
    });
    railLayout({ width, height, viewport: window.innerWidth, dials: rects }).catch(report);
  }

  function menu(widget: string | null) {
    (widget ? dialMenu(widget) : railMenu()).catch(report);
  }

  onMount(() => {
    document.documentElement.classList.add("overlay");
    reportUncaught();
    const disconnect = core.connect();
    disconnect.catch(report);
    const unlisten = onPanelState((state) => {
      open = state.widget;
      side = state.side;
    });
    return () => {
      disconnect.then((stop) => stop());
      unlisten.then((stop) => stop());
    };
  });

  // Every change — a widget joining, a readout, density, orientation — may
  // be a new window size and new dial positions for the shell. Measured
  // after each update rather than on ResizeObserver alone: WebKit runs no
  // rendering updates for a window that has never been shown, and the rail
  // is shown only once it has been measured.
  $effect(() => {
    void [dials, dock];
    tick().then(measure);
  });
  // Fonts arriving after the first measurement change the size again.
  $effect(() => {
    if (!frame) return;
    const observer = new ResizeObserver(measure);
    observer.observe(frame);
    return () => observer.disconnect();
  });
</script>

{#if dials.length}
  <div class="window" bind:this={frame}>
    <Rail
      widgets={dials}
      orientation={dock?.orientation}
      density={dock?.density}
      readouts={dock?.readouts ?? true}
      {open}
      {side}
      onhover={(widget) => hoverDial(widget).catch(report)}
      onapproach={(widget) => hoverApproach(widget).catch(report)}
      ongrab={() => startDrag().catch(report)}
      onmenu={menu}
    />
  </div>
{/if}

<style>
  .window { display: inline-flex; padding: 1px; }
</style>

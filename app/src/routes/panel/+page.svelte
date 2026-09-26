<script lang="ts">
  import { onMount, tick } from "svelte";
  import { CORE_DIAL, core, coreDown } from "$lib/core.svelte";
  import type { Side } from "$lib/dock/side";
  import Panel from "$lib/panel/Panel.svelte";
  import {
    hoverPanel,
    onPanelClose,
    onPanelOpen,
    onPanelReveal,
    panelLayout,
    panelPainted,
    report,
    reportUncaught,
  } from "$lib/shell";

  // One of the two panel windows. The shell names a widget in the one not
  // showing as soon as the pointer reaches its dial; the page draws it,
  // answers with its size, waits for the window to take that size and for
  // two frames to paint it, and says so — only then is it shown, so no frame
  // shows an empty panel or a widget in another's geometry. Arrival and the
  // 100 ms close fade are animated here.
  let widget = $state<string | null>(null);
  let side = $state<Side>("right");
  let maxHeight = $state<number>();
  let closing = $state(false);
  let seq = 0;
  let frame = $state<HTMLElement>();
  let shadow = $state<HTMLElement>();
  let tallest = $state<number>();
  const pages = $state<Record<string, string>>({});
  const snapshot = $derived(
    widget === CORE_DIAL
      ? coreDown(core.linkError ?? "ncd-core is not answering")
      : widget
        ? core.snapshots[widget]
        : undefined,
  );
  // Above a horizontal rail the panel's bottom is held against the rail, so
  // it takes the height of its tallest page: switching tabs never moves the
  // header under the pointer.
  const steady = $derived(side === "top" && (snapshot?.pages.length ?? 0) > 1);

  async function measure() {
    if (!frame || !widget || closing) return;
    let { width, height } = frame.getBoundingClientRect();
    if (steady && shadow) {
      tallest = Math.max(...[...shadow.children].map((c) => c.getBoundingClientRect().height)) || undefined;
      height = Math.max(height, (tallest ?? 0) + 2);
    }
    const asked = seq;
    try {
      await panelLayout(asked, width, height);
      await sized(width, height);
      await painted();
      if (asked === seq) await panelPainted(asked);
    } catch (error) {
      report(error);
    }
  }

  // The window has taken the size it was given (or 200 ms have passed).
  function sized(width: number, height: number) {
    const fits = () => Math.abs(innerWidth - width) < 2 && Math.abs(innerHeight - height) < 2;
    return new Promise<void>((done) => {
      if (fits()) return done();
      const finish = () => {
        clearTimeout(timer);
        removeEventListener("resize", check);
        done();
      };
      const check = () => fits() && finish();
      const timer = setTimeout(finish, 200);
      addEventListener("resize", check);
    });
  }

  // Two frames: the one that paints, and the next. A view that runs no
  // frames gives up after 100 ms rather than never.
  const nextFrame = () => new Promise<void>((done) => requestAnimationFrame(() => done()));
  const painted = () =>
    Promise.race([nextFrame().then(nextFrame), new Promise<void>((done) => setTimeout(done, 100))]);

  const SLIDE: Record<Side, string> = {
    right: "translateX(-3px)",
    left: "translateX(3px)",
    bottom: "translateY(-3px)",
    top: "translateY(3px)",
  };
  function arrive(from: Side) {
    if (!frame || matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    frame.animate([{ opacity: 0, transform: SLIDE[from] }, { opacity: 1, transform: "none" }], {
      duration: 140,
      easing: getComputedStyle(document.documentElement).getPropertyValue("--ease-out").trim() || "ease-out",
    });
  }

  onMount(() => {
    document.documentElement.classList.add("overlay");
    reportUncaught();
    const disconnect = core.connect();
    disconnect.catch(report);
    const unlistenOpen = onPanelOpen(async (open) => {
      closing = false;
      widget = open.widget;
      side = open.side;
      // The window keeps 1px of margin on every side for the ring.
      maxHeight = open.maxHeight - 2;
      seq = open.seq;
      await tick();
      measure();
    });
    const unlistenReveal = onPanelReveal((from) => {
      closing = false;
      arrive(from);
    });
    const unlistenClose = onPanelClose(() => (closing = true));

    const root = document.documentElement;
    const enter = () => hoverPanel(true).catch(report);
    const leave = () => hoverPanel(false).catch(report);
    root.addEventListener("mouseenter", enter);
    root.addEventListener("mouseleave", leave);
    return () => {
      disconnect.then((stop) => stop());
      unlistenOpen.then((stop) => stop());
      unlistenReveal.then((stop) => stop());
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
  <div class="window" class:closing bind:this={frame}>
    <Panel
      {snapshot}
      bind:page={pages[snapshot.id]}
      {maxHeight}
      minHeight={steady ? tallest : undefined}
      stale={widget === CORE_DIAL ? false : undefined}
      since={widget === CORE_DIAL ? core.linkDownSince : undefined}
    />
  </div>
  {#if steady}
    <!-- Every page, drawn unseen, to find the tallest. -->
    <div class="shadow" aria-hidden="true" bind:this={shadow}>
      {#each snapshot.pages as p (p.id)}
        <Panel {snapshot} page={p.id} {maxHeight} />
      {/each}
    </div>
  {/if}
{/if}

<style>
  .window {
    display: inline-flex;
    padding: 1px;
    transition: opacity 100ms linear;
  }
  .window.closing { opacity: 0; }
  .shadow {
    position: absolute;
    left: -10000px;
    top: 0;
    visibility: hidden;
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .window { transition: none; }
  }
</style>

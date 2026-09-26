<script lang="ts">
  import type { Snippet } from "svelte";
  import { core } from "$lib/core.svelte";
  import type { Config, WidgetSnapshot } from "$lib/protocol";
  import Dial from "./Dial.svelte";
  import type { Side } from "./side";

  type Dock = Config["dock"];

  // The rail: a floating capsule with one dial per widget, in order. No
  // title, no wordmark; the grip at its leading end is the only thing on it
  // that is not a widget. Anywhere that is not a dial moves it. The gallery
  // passes its own dials (children) to show every state on one rail.
  let {
    widgets,
    orientation = "vertical",
    density = "normal",
    readouts = true,
    open = null,
    side = "right",
    element = $bindable(),
    onhover,
    ongrab,
    onmenu,
    children,
  }: {
    widgets: WidgetSnapshot[];
    orientation?: Dock["orientation"];
    density?: Dock["density"];
    readouts?: boolean;
    open?: string | null;
    side?: Side;
    element?: HTMLElement;
    onhover?: (widget: string | null) => void;
    ongrab?: (event: PointerEvent) => void;
    /** Right-click: on a dial with its widget, on the rail itself with null. */
    onmenu?: (widget: string | null) => void;
    children?: Snippet;
  } = $props();

  function grab(event: PointerEvent) {
    if (event.button !== 0 || (event.target as Element).closest(".Dial")) return;
    ongrab?.(event);
  }
</script>

<nav
  class="Rail"
  data-orient={orientation === "horizontal" ? "h" : "v"}
  data-density={density}
  data-readouts={readouts && density !== "compact" ? "" : undefined}
  aria-label="NoClickDock"
  bind:this={element}
  onpointerdown={grab}
  oncontextmenu={(event) => {
    if (!onmenu) return;
    event.preventDefault();
    onmenu(null);
  }}
>
  <div class="Rail-grip" aria-hidden="true"><i></i><i></i><i></i></div>
  {#if children}
    {@render children()}
  {:else}
    {#each widgets as snapshot (snapshot.id)}
      <Dial
        {snapshot}
        {side}
        open={open === snapshot.id}
        stale={core.isStale(snapshot)}
        readout={readouts && density !== "compact"}
        onenter={() => onhover?.(snapshot.id)}
        onleave={() => onhover?.(null)}
        oncontext={onmenu && (() => onmenu(snapshot.id))}
      />
    {/each}
  {/if}
</nav>

<style>
  .Rail {
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    gap: var(--slot-gap);
    padding: 6px;
    background: var(--s1);
    border-radius: var(--r-control);
    box-shadow: var(--edge-2);
    user-select: none;
    -webkit-user-select: none;
  }
  /* A vertical rail's readouts need room at its trailing end. */
  .Rail[data-orient="v"][data-readouts] { padding: 6px 6px 10px; }
  .Rail[data-orient="h"] { flex-direction: row; align-items: flex-start; padding: 6px 12px 6px 6px; }
  .Rail[data-orient="h"]:not([data-readouts]) { padding: 6px 8px 6px 6px; }

  /* The grip: the leading 14px, three engraved 2px dots. */
  .Rail-grip {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 3px;
    width: var(--dial);
    height: 14px;
    margin-bottom: -2px;
    cursor: grab;
  }
  .Rail[data-orient="h"] .Rail-grip {
    flex-direction: column;
    width: 14px;
    height: var(--dial);
    margin: 0 -2px 0 0;
    align-self: flex-start;
  }
  .Rail-grip i {
    width: 2px;
    height: 2px;
    border-radius: 1px;
    background: var(--faint);
    box-shadow: 0 1px 0 rgba(255,255,255,.05);
  }
</style>

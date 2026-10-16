<script lang="ts">
  import { tick } from "svelte";
  import { core } from "$lib/core.svelte";
  import Segmented from "$lib/kit/Segmented.svelte";
  import type { WidgetSnapshot } from "$lib/protocol";
  import Blocks from "./Blocks.svelte";
  import PanelFooter from "./PanelFooter.svelte";
  import PanelHeader from "./PanelHeader.svelte";

  // The hover panel: one surface, no nested cards, its elevation ring for an
  // edge. Header, tabs and footer stay put; only the body scrolls, and only
  // when there is more to say than the monitor has room for.
  let {
    snapshot,
    page = $bindable(),
    maxHeight,
    minHeight,
    stale,
    since,
  }: {
    snapshot: WidgetSnapshot;
    page?: string;
    maxHeight?: number;
    minHeight?: number;
    stale?: boolean;
    /** Down since then (the core-down panel): the footer says so. */
    since?: number | null;
  } = $props();

  const current = $derived(snapshot.pages.find((p) => p.id === page) ?? snapshot.pages[0]);
  const tabs = $derived(snapshot.pages.map((p) => ({ value: p.id, label: p.label, tone: p.tone, badge: p.badge })));
  const quiet = $derived(stale ?? core.isStale(snapshot));

  // Keep the selected tab valid when a widget's pages change under it.
  $effect(() => {
    if (current && current.id !== page) page = current.id;
  });

  // Every meter on a page shares one label and one value column, and every
  // limit likewise, so bars start on the same x across sections (A6). The
  // columns are the widest label and value text on the page.
  let body = $state<HTMLElement>();
  $effect(() => {
    void [current, snapshot];
    const el = body;
    if (!el) return;
    tick().then(() => align(el));
    document.fonts.ready.then(() => align(el));
  });

  function align(el: HTMLElement) {
    const range = document.createRange();
    const widest = (selector: string) =>
      Math.ceil(
        Math.max(
          0,
          ...[...el.querySelectorAll<HTMLElement>(selector)].map((node) => {
            range.selectNodeContents(node);
            return range.getBoundingClientRect().width;
          }),
        ),
      );
    const label = widest(".Meter-label");
    el.style.setProperty("--meter-cols", label ? `min(${label}px, 45%) minmax(0, 1fr) ${widest(".Meter-value")}px` : "");
    const limit = widest(".Limit-label");
    el.style.setProperty(
      "--limit-cols",
      limit ? `min(${limit}px, 40%) minmax(0, 1fr) ${widest(".Limit-pct")}px ${widest(".Limit-resets")}px` : "",
    );
  }
</script>

<section
  class="Panel"
  style:--panel-w={snapshot.width ? `${snapshot.width}px` : undefined}
  style:--panel-max-h={maxHeight ? `${maxHeight}px` : undefined}
  style:min-height={minHeight ? `${minHeight}px` : undefined}
  aria-label="{snapshot.title}: {quiet ? 'stale' : snapshot.stateLabel}"
>
  <div class="Panel-head">
    <PanelHeader {snapshot} stale={quiet} />
    {#if snapshot.pages.length > 1}
      <div class="Panel-tabs"><Segmented options={tabs} bind:value={page} /></div>
    {/if}
  </div>
  <div class="Panel-body" bind:this={body}>
    {#if current}<Blocks blocks={current.blocks} widget={snapshot.id} />{/if}
  </div>
  <PanelFooter {snapshot} heard={core.lastHeard(snapshot.id)} stale={quiet} {since} />
</section>

<style>
  .Panel {
    display: flex;
    flex-direction: column;
    width: var(--panel-w);
    max-height: var(--panel-max-h, none);
    background: var(--s1);
    border-radius: var(--r-panel);
    box-shadow: var(--edge-2);
    color: var(--text);
    overflow: hidden;
  }
  .Panel-head { flex: none; border-bottom: 1px solid var(--line); }
  .Panel-tabs { padding: 0 var(--inset) 12px; }
  .Panel-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px var(--inset) 12px;
  }
</style>

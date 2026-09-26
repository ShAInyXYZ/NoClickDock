<script lang="ts">
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
    stale,
  }: { snapshot: WidgetSnapshot; page?: string; maxHeight?: number; stale?: boolean } = $props();

  const current = $derived(snapshot.pages.find((p) => p.id === page) ?? snapshot.pages[0]);
  const tabs = $derived(snapshot.pages.map((p) => ({ value: p.id, label: p.label, tone: p.tone, badge: p.badge })));
  const quiet = $derived(stale ?? core.isStale(snapshot));

  // Keep the selected tab valid when a widget's pages change under it.
  $effect(() => {
    if (current && current.id !== page) page = current.id;
  });
</script>

<section
  class="Panel"
  style:--panel-w={snapshot.width ? `${snapshot.width}px` : undefined}
  style:--panel-max-h={maxHeight ? `${maxHeight}px` : undefined}
  aria-label="{snapshot.title}: {quiet ? 'stale' : snapshot.stateLabel}"
>
  <div class="Panel-head">
    <PanelHeader {snapshot} stale={quiet} />
    {#if snapshot.pages.length > 1}
      <div class="Panel-tabs"><Segmented options={tabs} bind:value={page} /></div>
    {/if}
  </div>
  <div class="Panel-body">
    {#if current}<Blocks blocks={current.blocks} widget={snapshot.id} />{/if}
  </div>
  <PanelFooter {snapshot} heard={core.lastHeard(snapshot.id)} stale={quiet} />
</section>

<style>
  .Panel {
    display: flex;
    flex-direction: column;
    width: var(--panel-w);
    max-height: var(--panel-max-h, none);
    background: var(--s1);
    border-radius: var(--r-panel);
    box-shadow: var(--elev-2);
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

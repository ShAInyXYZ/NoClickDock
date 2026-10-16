<script lang="ts">
  import { clock } from "$lib/core.svelte";
  import type { WidgetSnapshot } from "$lib/protocol";

  // 28px under a hairline: what the widget adds about itself on the left,
  // and when it was last read on the right. A collector error takes the line
  // in red and says which reading is still shown; a widget gone quiet takes
  // it in amber and says since when. Something down whose panel already
  // says why (the core itself) says what is being done about it, in amber,
  // and since when — never the error twice.
  let {
    snapshot,
    heard,
    stale = false,
    since,
  }: { snapshot: WidgetSnapshot; heard: number; stale?: boolean; since?: number | null } = $props();

  const measured = $derived(Date.parse(snapshot.updatedAt));
</script>

{#if since}
  <div class="PanelFooter" data-tone="warn">
    <span class="PanelFooter-about">{snapshot.footer ?? ""}</span>
    <span class="fresh">since {clock(since)}</span>
  </div>
{:else if snapshot.error}
  <div class="PanelFooter" data-tone="crit">
    <span class="PanelFooter-about" title={snapshot.error}>{snapshot.error}</span>
    {#if measured > 0}<span class="fresh">showing {clock(measured)}</span>{/if}
  </div>
{:else if stale}
  <div class="PanelFooter" data-tone="warn">
    <span class="PanelFooter-about">{snapshot.footer ?? ""}</span>
    <span class="fresh">no reading since {clock(heard)}</span>
  </div>
{:else}
  <div class="PanelFooter">
    <span class="PanelFooter-about">{snapshot.footer ?? ""}</span>
    <span class="fresh">{clock(heard || measured)} · {snapshot.pollSecs}s</span>
  </div>
{/if}

<style>
  .PanelFooter {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    height: 28px;
    padding: 0 var(--inset);
    border-top: 1px solid var(--line);
    font: 400 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .04em;
    color: var(--dim);
  }
  .PanelFooter-about { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 16px; }
  .fresh { flex: none; color: var(--faint); white-space: nowrap; }
  .PanelFooter[data-tone="crit"], .PanelFooter[data-tone="crit"] .fresh { color: var(--err); }
  .PanelFooter[data-tone="warn"], .PanelFooter[data-tone="warn"] .fresh { color: var(--warn); }
</style>

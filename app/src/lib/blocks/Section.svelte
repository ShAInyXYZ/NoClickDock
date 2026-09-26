<script lang="ts">
  import type { SectionBlock } from "$lib/protocol";

  // A printed strip that opens a group: the title engraved on the left, its
  // count or note on the right, a hairline above. Always one line.
  let { block }: { block: SectionBlock } = $props();
</script>

<div class="Section" data-tone={block.tone === "warn" || block.tone === "crit" ? block.tone : undefined}>
  <span class="Section-title">{block.title}</span>
  {#if block.meta}<span class="Section-meta">{block.meta}</span>{/if}
</div>

<style>
  .Section {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    height: 32px;
    margin-top: 8px;
    border-top: 1px solid var(--line);
  }
  .Section:first-child { margin-top: 0; border-top: 0; }
  .Section-title {
    font: 500 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .22em;
    text-transform: uppercase;
    color: var(--dim);
    white-space: nowrap;
  }
  .Section[data-tone] .Section-title { color: var(--tone); }
  .Section-meta {
    font: 400 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .04em;
    color: var(--dim);
    white-space: nowrap;
  }
</style>

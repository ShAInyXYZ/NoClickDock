<script lang="ts">
  import type { SectionBlock } from "$lib/protocol";

  // A printed strip that opens a group: the title engraved on the left, its
  // count or note on the right, a hairline above. Always one line.
  let { block }: { block: SectionBlock } = $props();
  let failedIcon = $state<string | undefined>();
  const icon = $derived(block.icon && /^data:image\/(png|jpeg|gif|x-icon|svg\+xml);base64,/.test(block.icon) ? block.icon : undefined);
</script>

<div class="Section" data-tone={block.tone === "warn" || block.tone === "crit" ? block.tone : undefined}>
  <span class="Section-heading">
    {#if block.initials}
      <span class="Section-icon" aria-hidden="true">
        {#if icon && failedIcon !== icon}<img src={icon} alt="" width="18" height="18" onerror={() => failedIcon = icon} />
        {:else}<span>{block.initials}</span>{/if}
      </span>
    {/if}
    <span class="Section-title">{block.title}</span>
  </span>
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
  .Section-heading { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .Section-icon { flex: none; display: grid; place-items: center; width: 18px; height: 18px; color: var(--muted); background: var(--s2); border-radius: var(--r); font: 500 9px/1 var(--font-mono); letter-spacing: 0; }
  .Section-icon img { display: block; width: 18px; height: 18px; object-fit: contain; }
  .Section-title {
    /* Uppercase has no descenders, but a 1× line box under overflow:hidden
       would clip any that came (a "·", a lowercase unit). */
    font: 500 var(--fs-micro)/16px var(--font-mono);
    letter-spacing: .22em;
    text-transform: uppercase;
    color: var(--dim);
    white-space: nowrap;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .Section[data-tone] .Section-title { color: var(--tone); }
  .Section-meta {
    flex: none;
    font: 400 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .04em;
    color: var(--dim);
    white-space: nowrap;
  }
</style>

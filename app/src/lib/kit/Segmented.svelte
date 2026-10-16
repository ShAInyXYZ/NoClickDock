<script lang="ts">
  import type { Tone } from "$lib/protocol";
  import Led from "./Led.svelte";

  // A toggle bank as the panel's page tabs. Hovering a segment for
  // 180 ms switches to it — reading never needs a click — and a click works
  // too. The selected segment is a raised key, not a cherry block: a tab
  // carries a problem's lamp and count, which a cherry fill would drown.
  interface Option { value: string; label: string; tone?: Tone; badge?: string }
  let { options, value = $bindable() }: { options: Option[]; value?: string } = $props();

  const HOVER_SWITCH_MS = 180;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function dwell(next: string) {
    clearTimeout(timer);
    timer = setTimeout(() => (value = next), HOVER_SWITCH_MS);
  }
  function cancel() {
    clearTimeout(timer);
  }
  $effect(() => cancel);

  const problem = (tone?: Tone) => tone === "warn" || tone === "crit";
</script>

<div class="Segmented" role="tablist">
  {#each options as option (option.value)}
    <button
      class="Segmented-btn"
      role="tab"
      aria-selected={value === option.value}
      onclick={() => {
        cancel();
        value = option.value;
      }}
      onpointerenter={() => dwell(option.value)}
      onpointerleave={cancel}
    >
      <span class="Segmented-label">{option.label}</span>
      {#if problem(option.tone)}<Led tone={option.tone} />{/if}
      {#if option.badge}<span class="Segmented-badge" data-tone={problem(option.tone) ? option.tone : undefined}>{option.badge}</span>{/if}
    </button>
  {/each}
</div>

<style>
  .Segmented {
    display: inline-flex;
    max-width: 100%;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--line2);
    border-radius: var(--r);
    background: var(--s2);
  }
  .Segmented-btn {
    --led: 5px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 10px;
    border: 0;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--dim);
    font: 500 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .1em;
    text-transform: uppercase;
    cursor: default;
    white-space: nowrap;
    min-width: 0;
    transition: color .1s, background .1s;
  }
  .Segmented-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; line-height: 14px; }
  .Segmented-btn:hover { color: var(--text); }
  .Segmented-btn[aria-selected="true"] {
    color: var(--text);
    background: var(--s3);
    box-shadow: 0 1px 0 0 var(--lift) inset, 0 -1px 0 0 var(--shade) inset, 0 0 0 1px var(--ring);
  }
  .Segmented-badge { letter-spacing: 0; color: var(--tone, var(--muted)); }
</style>

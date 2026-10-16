<script lang="ts">
  // One of a few, side by side: the panel's segmented strip as a radio
  // group — a click chooses (no hover switching here: this changes a
  // setting), the chosen one is a raised key.
  interface Option { value: string; label: string }
  let {
    options,
    value = $bindable(),
    label,
    onchange,
  }: { options: Option[]; value?: string; label: string; onchange?: (value: string) => void } = $props();

  function pick(next: string) {
    value = next;
    onchange?.(next);
  }

  function key(event: KeyboardEvent, i: number) {
    const step = event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = options[(i + step + options.length) % options.length];
    pick(next.value);
    ((event.currentTarget as HTMLElement).parentElement?.children[options.indexOf(next)] as HTMLElement | undefined)?.focus();
  }
</script>

<div class="Choice" role="radiogroup" aria-label={label}>
  {#each options as option, i (option.value)}
    <button
      type="button"
      class="Choice-btn"
      role="radio"
      aria-checked={value === option.value}
      tabindex={value === option.value || (!options.some((o) => o.value === value) && i === 0) ? 0 : -1}
      onclick={() => pick(option.value)}
      onkeydown={(event) => key(event, i)}
    >
      {option.label}
    </button>
  {/each}
</div>

<style>
  .Choice {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--line2);
    border-radius: var(--r);
    background: var(--s2);
  }
  .Choice-btn {
    height: 28px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--dim);
    font: 500 var(--fs-small)/28px var(--font-sans);
    white-space: nowrap;
    cursor: pointer;
    transition: color var(--t-fast), background var(--t-fast);
  }
  .Choice-btn:hover { color: var(--text); }
  .Choice-btn[aria-checked="true"] {
    color: var(--text);
    background: var(--s3);
    box-shadow: 0 1px 0 0 var(--lift) inset, 0 -1px 0 0 var(--shade) inset, 0 0 0 1px var(--ring);
  }
</style>

<script lang="ts">
  import { Select } from "bits-ui";

  // Cerveau's select on bits-ui: a 38px trigger in the field style, a menu
  // on the floating surface — never the browser's own <select>.
  interface Option { value: string; label: string }
  let {
    value = $bindable(""),
    options,
    label,
    placeholder = "Choose…",
    onchange,
  }: { value?: string; options: Option[]; label: string; placeholder?: string; onchange?: (value: string) => void } = $props();

  const current = $derived(options.find((o) => o.value === value));
</script>

<Select.Root
  type="single"
  bind:value
  items={options}
  onValueChange={(next) => onchange?.(next)}
>
  <Select.Trigger class="Sel-trigger" aria-label={label}>
    <span class="Sel-value" class:is-placeholder={!current}>{current?.label ?? placeholder}</span>
    <svg class="Sel-chev" viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg>
  </Select.Trigger>
  <Select.Portal>
    <Select.Content class="Sel-menu" sideOffset={4} align="start">
      <Select.Viewport>
        {#each options as option (option.value)}
          <Select.Item class="Sel-item" value={option.value} label={option.label}>
            {#snippet children({ selected })}
              <span class="Sel-item-label">{option.label}</span>
              {#if selected}<svg class="Sel-check" viewBox="0 0 16 16" aria-hidden="true"><path d="m3.5 8.5 3 3 6-7" /></svg>{/if}
            {/snippet}
          </Select.Item>
        {/each}
      </Select.Viewport>
    </Select.Content>
  </Select.Portal>
</Select.Root>

<style>
  :global(.Sel-trigger) {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    height: 38px;
    padding: 0 10px 0 12px;
    border: 1px solid var(--line2);
    border-radius: var(--r);
    background: var(--s2);
    color: var(--text);
    font: 500 var(--fs-body)/1 var(--font-sans);
    text-align: left;
    cursor: pointer;
    transition: border-color var(--t-fast), background var(--t-fast);
  }
  :global(.Sel-trigger:hover) { border-color: var(--faint); background: var(--s3); }
  :global(.Sel-trigger[data-state="open"]) { border-color: var(--accent-line); }
  .Sel-value { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 20px; }
  .Sel-value.is-placeholder { color: var(--dim); }
  .Sel-chev { width: 14px; height: 14px; flex: none; fill: none; stroke: var(--dim); stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }
  :global(.Sel-trigger[data-state="open"]) .Sel-chev { transform: rotate(180deg); }
  :global(.Sel-menu) {
    z-index: var(--z-popover);
    min-width: var(--bits-select-anchor-width);
    max-height: min(320px, var(--bits-select-content-available-height));
    overflow-y: auto;
    padding: 4px;
    border-radius: var(--r-lg);
    background: var(--s2);
    box-shadow: var(--elev-2);
  }
  :global(.Sel-item) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    height: 32px;
    padding: 0 10px;
    border-radius: var(--r);
    color: var(--muted);
    font: 500 var(--fs-body)/1 var(--font-sans);
    cursor: pointer;
    outline: none;
  }
  :global(.Sel-item[data-highlighted]) { background: var(--s3); color: var(--text); }
  :global(.Sel-item[data-selected]) { color: var(--text); }
  .Sel-item-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 20px; }
  .Sel-check { width: 14px; height: 14px; flex: none; fill: none; stroke: var(--accent); stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
</style>

<script lang="ts">
  import type { Snippet } from "svelte";

  // A labelled setting: the label (and an optional hint) above its control,
  // the core's refusal under it, in words.
  let {
    label,
    hint,
    error,
    id,
    children,
  }: { label: string; hint?: string; error?: string; id?: string; children: Snippet } = $props();
</script>

<div class="Field" class:has-error={!!error}>
  <label class="Field-label" for={id}>{label}{#if hint}<span class="Field-hint">{hint}</span>{/if}</label>
  {@render children()}
  {#if error}<p class="Field-error" role="alert">{error}</p>{/if}
</div>

<style>
  .Field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .Field-label { font: 500 var(--fs-small)/16px var(--font-sans); color: var(--muted); }
  .Field-hint { margin-left: 6px; color: var(--dim); font-weight: 400; }
  .Field-error { margin: 0; font: 500 var(--fs-small)/16px var(--font-sans); color: var(--err); }
  .Field :global(input) {
    width: 100%;
    height: 38px;
    padding: 0 12px;
    border: 1px solid var(--line2);
    border-radius: var(--r);
    background: var(--s2);
    color: var(--text);
    font: 500 var(--fs-body)/1 var(--font-sans);
    caret-color: var(--accent);
    transition: border-color var(--t-fast);
  }
  .Field :global(input.mono) { font: 400 var(--fs-small)/1 var(--font-mono); }
  .Field :global(input::placeholder) { color: var(--faint); }
  .Field :global(input:hover) { border-color: var(--faint); }
  .Field :global(input:focus-visible) { outline: 2px solid var(--accent); outline-offset: 2px; }
  .Field.has-error :global(input) { border-color: color-mix(in srgb, var(--err) 60%, var(--line2)); }
</style>

<script lang="ts">
  // An on/off switch: a 32 × 18 track with a raised thumb. The accent is
  // for "on" only — a control's state, never a status colour.
  let {
    checked = $bindable(false),
    label,
    disabled = false,
    onchange,
  }: { checked?: boolean; label: string; disabled?: boolean; onchange?: (checked: boolean) => void } = $props();
</script>

<button
  type="button"
  class="Switch"
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {disabled}
  onclick={() => {
    checked = !checked;
    onchange?.(checked);
  }}
>
  <span class="Switch-thumb"></span>
</button>

<style>
  .Switch {
    position: relative;
    flex: none;
    width: 32px;
    height: 18px;
    padding: 0;
    border: 1px solid var(--line2);
    border-radius: 999px;
    background: var(--s2);
    cursor: pointer;
    transition: background var(--t-fast), border-color var(--t-fast);
  }
  .Switch-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--dim);
    box-shadow: 0 1px 0 var(--lift) inset, 0 1px 2px var(--shade);
    transition: transform var(--t-fast) var(--ease-out), background var(--t-fast);
  }
  .Switch[aria-checked="true"] { background: var(--accent-soft); border-color: var(--accent-line); }
  .Switch[aria-checked="true"] .Switch-thumb { transform: translateX(14px); background: var(--accent); }
  .Switch:hover:not(:disabled) { border-color: var(--faint); }
  .Switch:disabled { opacity: .45; cursor: default; }
  @media (prefers-reduced-motion: reduce) { .Switch, .Switch-thumb { transition: none; } }
</style>

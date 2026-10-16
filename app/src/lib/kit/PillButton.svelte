<script lang="ts">
  import { actions } from "$lib/actions.svelte";
  import type { Action } from "$lib/protocol";

  // A pill that changes the world. With a confirm, the first click
  // only arms it: the confirm text replaces the label, in the accent (or the
  // error colour for what cannot be undone). Every label shares one grid
  // cell, so the pill is as wide as the longest and arming never moves the
  // row. In a row or alert it is a ghost until the row is pointed at.
  let { widget, action }: { widget: string; action: Action } = $props();

  const key = $derived(actions.key(widget, action));
  const armed = $derived(!!actions.armed[key]);
  const running = $derived(!!actions.running[key]);
  const failure = $derived(actions.failed[key]);
</script>

<button
  class="PillButton"
  class:is-armed={armed}
  class:is-danger={action.danger}
  class:is-failed={!!failure}
  aria-busy={running}
  title={failure}
  onclick={() => actions.press(widget, action)}
>
  <span class="PillButton-text" class:shown={!armed && !failure}>{action.label}</span>
  {#if action.confirm}<span class="PillButton-text" class:shown={armed}>{action.confirm}</span>{/if}
  <span class="PillButton-text" class:shown={!!failure && !armed}>failed</span>
</button>

<style>
  .PillButton {
    display: inline-grid;
    place-items: center;
    height: 24px;
    padding: 0 10px;
    border: 1px solid var(--line2);
    border-radius: var(--r-control);
    background: var(--s2);
    color: var(--muted);
    font: 500 var(--fs-small)/1 var(--font-mono);
    letter-spacing: 0;
    white-space: nowrap;
    cursor: pointer;
    user-select: none;
    transition: background .1s, border-color .1s, color .1s, opacity var(--t-fast), transform .04s;
  }
  .PillButton-text { grid-area: 1 / 1; visibility: hidden; }
  .PillButton-text.shown { visibility: visible; }
  .PillButton:hover { color: var(--text); border-color: var(--faint); background: var(--s3); }
  .PillButton:active { transform: translateY(1px); }
  .PillButton[aria-busy="true"] { cursor: progress; opacity: .6; }

  /* At rest inside a row or an alert a pill is a ghost; pointing at the row
     or focusing into it gives the pill its body. Layout never shifts. */
  :global(:is(.Row, .Alert)) .PillButton:not(.is-armed) {
    background: transparent;
    border-color: var(--line);
    color: var(--dim);
  }
  :global(:is(.Row, .Alert):is(:hover, .is-hover, :focus-within)) .PillButton:not(.is-armed) {
    background: var(--s2);
    border-color: var(--line2);
    color: var(--muted);
  }
  :global(:is(.Row, .Alert)) .PillButton:not(.is-armed):hover {
    color: var(--text);
    border-color: var(--faint);
    background: var(--s3);
  }

  .PillButton.is-armed {
    background: var(--accent-soft);
    border-color: var(--accent-line);
    color: var(--accent);
  }
  .PillButton.is-armed.is-danger {
    background: color-mix(in srgb, var(--err) 12%, transparent);
    border-color: color-mix(in srgb, var(--err) 42%, transparent);
    color: var(--err);
  }
  :global(:is(.Row, .Alert)) .PillButton.is-failed:not(.is-armed),
  .PillButton.is-failed { color: var(--err); }
</style>

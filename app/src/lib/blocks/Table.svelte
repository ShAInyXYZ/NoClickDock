<script lang="ts">
  import Led from "$lib/kit/Led.svelte";
  import type { TableBlock, TableCell, Tone } from "$lib/protocol";

  // Aligned columns of readings (Power's draw). A left mono column is an
  // identifier (CPU, GPU0), a left prose column a name that takes the slack
  // and ellipsises; everything else is a number, right-aligned. A cell may
  // carry its own tone: one hot sensor in an otherwise quiet row. Groups of
  // rows share the one set of columns, each opened by a strip of its own;
  // the header row is the first group's strip.
  let { block }: { block: TableBlock } = $props();

  const kind = (i: number) => {
    const c = block.columns[i];
    if (!c || c.align !== "left") return "num";
    return c.mono ? "id" : "name";
  };
  const text = (cell: TableCell) => (typeof cell === "string" ? cell : cell.text);
  const tone = (cell: TableCell): Tone | undefined => (typeof cell === "string" ? undefined : cell.tone);
  const headed = $derived(block.columns.some((c) => c.label));
  const nameColumn = $derived(block.columns.findIndex((_, i) => kind(i) === "name"));
</script>

<table class="Table">
  {#if headed}
    <thead>
      <tr>
        {#each block.columns as column, i (i)}
          <th class={kind(i)}>{column.label}</th>
        {/each}
      </tr>
    </thead>
  {/if}
  <tbody>
    {#each block.rows as row, r (r)}
      {#if row.group}
        <tr class="Table-group">
          <td colspan={block.columns.length}>
            <div class="Table-strip">
              <span class="Table-title">{row.group}</span>
              {#if row.meta}<span class="Table-meta">{row.meta}</span>{/if}
            </div>
          </td>
        </tr>
      {:else}
      <tr>
        {#each row.cells as cell, i (i)}
          <td class={kind(i)} data-tone={tone(cell) ?? row.tone} title={kind(i) === "name" ? text(cell) : undefined}>
            {#if i === 0 && row.led}<Led tone={row.led} />{/if}
            {text(cell)}
            {#if i === nameColumn && row.sub}<span class="Table-sub">{row.sub}</span>{/if}
          </td>
        {/each}
      </tr>
      {/if}
    {/each}
  </tbody>
</table>

<style>
  .Table { width: 100%; border-collapse: collapse; }
  th {
    height: 32px;
    padding: 0 0 0 12px;
    text-align: right;
    vertical-align: middle;
    font: 500 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .22em;
    text-transform: uppercase;
    color: var(--dim);
    white-space: nowrap;
  }
  td {
    height: 28px;
    padding: 0 0 0 12px;
    vertical-align: middle;
    text-align: right;
    font: 400 var(--fs-small)/1 var(--font-mono);
    color: var(--muted);
    white-space: nowrap;
  }
  th:first-child, td:first-child { padding-left: 0; }
  th.id, th.name, td.id, td.name { text-align: left; }
  /* One width for identifiers, so the tables of one panel line up. */
  th.id, td.id { width: 48px; }
  td.id {
    font: 500 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .08em;
    text-transform: uppercase;
    color: var(--dim);
  }
  /* The name takes whatever the numbers leave, and ellipsises in it. */
  td.name {
    width: 100%;
    max-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font: 500 var(--fs-body)/1 var(--font-sans);
    color: var(--text);
  }
  .Table-sub { color: var(--dim); font-weight: 400; }
  td[data-tone="warn"] { color: var(--warn); }
  td[data-tone="crit"] { color: var(--err); }
  td > :global(.Led) { margin-right: 8px; vertical-align: middle; }
  /* A group's strip is a Section inside the table. */
  .Table-group td { height: auto; padding: 0; }
  .Table-strip {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    height: 32px;
    margin-top: 8px;
    border-top: 1px solid var(--line);
  }
  .Table-title {
    font: 500 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .22em;
    text-transform: uppercase;
    color: var(--dim);
    white-space: nowrap;
  }
  .Table-meta {
    font: 400 var(--fs-micro)/1 var(--font-mono);
    letter-spacing: .04em;
    color: var(--dim);
    white-space: nowrap;
  }
</style>

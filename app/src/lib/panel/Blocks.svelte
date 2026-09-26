<script lang="ts">
  import Alert from "$lib/blocks/Alert.svelte";
  import Bars from "$lib/blocks/Bars.svelte";
  import Chips from "$lib/blocks/Chips.svelte";
  import Donut from "$lib/blocks/Donut.svelte";
  import Empty from "$lib/blocks/Empty.svelte";
  import Limit from "$lib/blocks/Limit.svelte";
  import Meter from "$lib/blocks/Meter.svelte";
  import Note from "$lib/blocks/Note.svelte";
  import Row from "$lib/blocks/Row.svelte";
  import Section from "$lib/blocks/Section.svelte";
  import Stats from "$lib/blocks/Stats.svelte";
  import Table from "$lib/blocks/Table.svelte";
  import type { Block, LimitBlock, MeterBlock } from "$lib/protocol";
  import Blocks from "./Blocks.svelte";

  // A page's blocks, top to bottom. A run of consecutive meters (or limits)
  // becomes one grid, so its label column is as wide as its widest label and
  // every bar starts on the same x. A split lays two such lists side by side.
  let { blocks, widget }: { blocks: Block[]; widget: string } = $props();

  type Item =
    | { kind: "meters"; meters: MeterBlock[] }
    | { kind: "limits"; limits: LimitBlock[] }
    | { kind: "one"; block: Block };
  const items = $derived.by(() => {
    const out: Item[] = [];
    for (const block of blocks) {
      const last = out.at(-1);
      if (block.kind === "meter") {
        if (last?.kind === "meters") last.meters.push(block);
        else out.push({ kind: "meters", meters: [block] });
      } else if (block.kind === "limit") {
        if (last?.kind === "limits") last.limits.push(block);
        else out.push({ kind: "limits", limits: [block] });
      } else out.push({ kind: "one", block });
    }
    return out;
  });
</script>

{#each items as item, i (i)}
  {#if item.kind === "meters"}
    <div class="MeterGroup">
      {#each item.meters as block, j (j)}<Meter {block} />{/each}
    </div>
  {:else if item.kind === "limits"}
    <div class="LimitGroup">
      {#each item.limits as block, j (j)}<Limit {block} />{/each}
    </div>
  {:else if item.block.kind === "section"}
    <Section block={item.block} />
  {:else if item.block.kind === "row"}
    <Row block={item.block} {widget} />
  {:else if item.block.kind === "note"}
    <Note block={item.block} />
  {:else if item.block.kind === "stats"}
    <Stats block={item.block} />
  {:else if item.block.kind === "alert"}
    <Alert block={item.block} {widget} />
  {:else if item.block.kind === "chips"}
    <Chips block={item.block} {widget} />
  {:else if item.block.kind === "table"}
    <Table block={item.block} />
  {:else if item.block.kind === "donut"}
    <Donut block={item.block} />
  {:else if item.block.kind === "bars"}
    <Bars block={item.block} />
  {:else if item.block.kind === "empty"}
    <Empty block={item.block} />
  {:else if item.block.kind === "split"}
    <div class="Split">
      {#each item.block.columns as column, c (c)}
        <div class="Split-col"><Blocks blocks={column} {widget} /></div>
      {/each}
    </div>
  {/if}
{/each}

<style>
  .MeterGroup, .LimitGroup {
    display: grid;
    align-items: center;
    column-gap: 12px;
  }
  /* The panel sets the page's shared columns (--meter-cols, --limit-cols);
     until it has measured, each group sizes itself. A long label never
     squeezes the bar away. */
  .MeterGroup { grid-template-columns: var(--meter-cols, fit-content(45%) minmax(0, 1fr) max-content); }
  .LimitGroup { grid-template-columns: var(--limit-cols, fit-content(40%) minmax(0, 1fr) max-content max-content); }

  .Split {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    column-gap: 24px;
    margin-top: 4px;
  }
  .Split-col { min-width: 0; }
  /* The one structural divider in a split. */
  .Split-col + .Split-col { position: relative; }
  .Split-col + .Split-col::before {
    content: '';
    position: absolute;
    left: -12px;
    top: 8px;
    bottom: 0;
    width: 1px;
    background: var(--line);
  }
</style>

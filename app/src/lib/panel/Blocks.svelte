<script lang="ts">
  import Alert from "$lib/blocks/Alert.svelte";
  import Meter from "$lib/blocks/Meter.svelte";
  import Note from "$lib/blocks/Note.svelte";
  import Row from "$lib/blocks/Row.svelte";
  import Section from "$lib/blocks/Section.svelte";
  import Stats from "$lib/blocks/Stats.svelte";
  import type { Block, MeterBlock } from "$lib/protocol";

  // A page's blocks, top to bottom. A run of consecutive meters becomes one
  // grid, so its label column is as wide as its widest label and every bar
  // starts on the same x. A kind without a renderer yet says so rather than
  // vanishing: a panel never silently drops what the core sent.
  let { blocks, widget }: { blocks: Block[]; widget: string } = $props();

  type Item = { kind: "meters"; meters: MeterBlock[] } | { kind: "one"; block: Block };
  const items = $derived.by(() => {
    const out: Item[] = [];
    for (const block of blocks) {
      const last = out.at(-1);
      if (block.kind === "meter" && last?.kind === "meters") last.meters.push(block);
      else if (block.kind === "meter") out.push({ kind: "meters", meters: [block] });
      else out.push({ kind: "one", block });
    }
    return out;
  });
</script>

{#each items as item, i (i)}
  {#if item.kind === "meters"}
    <div class="MeterGroup">
      {#each item.meters as block, j (j)}<Meter {block} />{/each}
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
  {:else}
    <p class="Missing">{item.block.kind} · not drawn yet</p>
  {/if}
{/each}

<style>
  .MeterGroup {
    display: grid;
    /* Labels size the column, but a very long mount path cannot squeeze the bar away. */
    grid-template-columns: fit-content(45%) minmax(0, 1fr) max-content;
    align-items: center;
    column-gap: 12px;
  }
  .Missing {
    padding: 6px 0;
    font: 400 var(--fs-micro)/16px var(--font-mono);
    color: var(--dim);
  }
</style>

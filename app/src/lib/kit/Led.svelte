<script lang="ts">
  import type { Tone } from "$lib/protocol";

  // A square TP-7 lamp. Colour alone never carries meaning:
  // every lamp has its words somewhere on the same surface. Size is --led.
  let { tone }: { tone?: Tone } = $props();
</script>

<span class="Led" data-tone={tone ?? "idle"} aria-hidden="true"></span>

<style>
  .Led {
    display: inline-block;
    flex: none;
    width: var(--led);
    height: var(--led);
    border-radius: 1px;
    background: var(--tone, var(--faint));
  }
  .Led[data-tone="ok"] { background: var(--ok-led); }
</style>

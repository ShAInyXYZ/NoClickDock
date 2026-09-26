<script lang="ts">
  import { isTauri } from "@tauri-apps/api/core";
  import { getCurrentWindow } from "@tauri-apps/api/window";
  import { onMount } from "svelte";
  import { core } from "$lib/core.svelte";
  import Settings from "$lib/settings/Settings.svelte";
  import { titles } from "$lib/settings/titles";
  import { report, reportUncaught } from "$lib/shell";

  // The Settings window: created by the shell on demand and destroyed on
  // close (it costs nothing while shut). It reads and writes config.json
  // through the core like every window.
  // In a plain browser (development) there is no window to move or close.
  const win = isTauri() ? getCurrentWindow() : null;
  const controls = win
    ? {
        drag: (event: PointerEvent) => {
          if (event.button === 0) win.startDragging().catch(report);
        },
        minimize: () => win.minimize().catch(report),
        close: () => win.close().catch(report),
      }
    : undefined;

  onMount(() => {
    reportUncaught();
    const disconnect = core.connect();
    disconnect.catch(report);
    return () => {
      disconnect.then((stop) => stop());
    };
  });
</script>

<svelte:head><title>NoClickDock Settings</title></svelte:head>

<main>
  {#if core.config}
    <Settings
      config={core.config}
      {titles}
      platform={core.platform}
      debugBuild={import.meta.env.DEV}
      save={(patch) => core.patchConfig(patch)}
      ask={(widget, what, args) => core.ask(widget, what, args)}
      {controls}
    />
  {:else}
    <p class="waiting">{core.linkError ?? "Waiting for ncd-core…"}</p>
  {/if}
</main>

<style>
  :global(html), :global(body) { height: 100%; overflow: hidden; background: var(--s1); }
  main { height: 100dvh; box-shadow: inset 0 0 0 1px var(--line2); }
  main :global(.Settings) { box-shadow: inset 0 0 0 1px var(--line2); }
  .waiting { margin: 0; padding: 56px 24px; color: var(--muted); font: 500 var(--fs-body)/20px var(--font-sans); }
</style>

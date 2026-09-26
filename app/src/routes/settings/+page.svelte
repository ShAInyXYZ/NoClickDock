<script lang="ts">
  // Settings arrive in M6 (DESIGN.md: Widgets, Dock, Start, per-widget
  // options). Until then the dock is configured through config.json.
</script>

<main>
  <h1>Settings</h1>
  <p>Arrives in milestone M6. Until then, <code>~/.config/noclickdock/config.json</code> holds the dock's options.</p>
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    padding: var(--sp-8);
  }
  h1 { font-size: var(--fs-title); font-weight: 600; }
  p { color: var(--muted); }
  code { font-family: var(--font-mono); font-size: var(--fs-small); color: var(--text); }
</style>

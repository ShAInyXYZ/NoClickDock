import { defineConfig } from "vite";
import { sveltekit } from "@sveltejs/kit/vite";

// Tauri expects a fixed port and must not have Rust errors cleared away.
// The design and research reviewers run their own copy with --port 1430/1440.
export default defineConfig({
  plugins: [sveltekit()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
    host: "127.0.0.1",
    watch: { ignored: ["**/src-tauri/**"] },
  },
});

# M8 — secret scan of the branch

Scope: `git log main..HEAD -p` at `9e61461`, 83 commits, covering every
**added line and every commit message** (51,564 lines). The images added
or changed on the branch were checked too.

The scan matched against this machine's real values, collected at scan
time and never written anywhere:
- hostname and username (as `/home/<user>`, `<user>@`, `\Users\<user>`);
- LAN addresses;
- the tailnet's name, and every node's HostName, MagicDNS label and
  Tailscale IP;
- every Docker container name;
- GPU UUIDs, serials and bus ids;
- board serial and product UUID;
- the 30 project names under `~/.claude/projects` and in Codex rollout
  working directories;
- e-mail addresses.

Token patterns: private keys, `sk-`/`sk-ant-`, GitHub, AWS, Slack and
Tailscale keys, JWTs, bearer tokens.

## Result

| category | hits |
|---|---|
| hostname, username paths, LAN IPs | 0 |
| tailnet name, node names, Tailscale IPs | 0 |
| GPU UUIDs / serials, board serial / UUID | 0 |
| tokens, keys, JWTs | 0 |
| e-mail | only `noreply@anthropic.com` (co-author lines) and `example.org`. Author/committer metadata is the user's public address, as on main. |
| container names | 40 lines, all product names or ordinary words that happen to be container names here (e.g. `portainer`). None is a private name. |
| images (104 blobs: icons, fonts, screenshots, evidence) | No PNG `tEXt`/`iTXt`/`zTXt`/`eXIf` chunks at all. SVGs contain no paths or names. Every screenshot I opened shows the mock data ("invented data"; RTX 4090 / Ryzen 9 7950X mock hardware, not this machine's). The power report shots use the invented 25-day test log. |
| **the user's other project names** | **HubVoice** (24 lines), **DeadWhispr** (10), **Cerveau-Hub** (8) — see must-fix |

Also clean: the **HEAD tree** (413 files) contains none of the tailnet,
host or IP values above.

## MUST-FIX (if those projects are private)

1. **Names of the user's non-public projects appear in the tree and in
   branch history.**
   - `gh repo list ShAInyXYZ` shows `Cerveau` public, but no HubVoice,
     DeadWhispr or Cerveau-Hub.
   - In the HEAD tree (12 files):
     - `DESIGN.md` lines 7–8, 55, 78, 262 (`../DeadWhispr2`,
       `../Cerveau-Hub/HubVoice` paths);
     - `docs/rewrite/ARCHITECTURE.md:3`;
     - `docs/rewrite/design/README.md`;
     - `docs/rewrite/design/reference.css:14` and `docs/rewrite/design/shoot.mjs` (relative paths);
     - `app/src-tauri/src/dock.rs:477`;
     - `app/src-tauri/src/overlay.rs:152`;
     - `app/src/lib/dock/Dial.svelte:8`;
     - `docs/rewrite/reviews/M1-exec.md`.
   - My own files (`research/tauri-windows.md`, `reviews/M2-research-r1.md`,
     `-r2.md`) are scrubbed in the working tree (now "the sibling app");
     main commits them.
   - Fix: replace them with neutral words in the tree.
   - History: the branch has not been pushed (there is no
     `origin/rewrite/tauri-go-svelte`). A **squash merge** into main
     publishes only the final tree. Pushing the branch as is would publish
     the old lines. Rewriting it before a push is main's call.
   - If these names are meant to be public, drop this item.

## Pre-existing in main's history (report only, nothing rewritten)

- `git log main -p` (3 commits) contains **real tailnet node names** in 4
  added lines of `dcfc0c0bd`:
  - `README.md`;
  - `widgets/tailscale/README.md`;
  - `widgets/tailscale/tailscale-status-checker.py` (a config example
    `{"watch": [...]}`).
- The files were removed or rewritten on this branch (`508a809`), so the
  merged tree will not have them. Main's published history keeps them. If
  that matters, only a history rewrite of main (with a force push)
  removes them. That is the user's decision.

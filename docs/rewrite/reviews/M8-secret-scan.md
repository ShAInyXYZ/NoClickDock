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
| the user's other project names | 42 lines in the tree and in branch history — **fixed**, see below |

Also clean: the **HEAD tree** (413 files) contains none of the tailnet,
host or IP values above.

## Fixed before the push

1. **Names of the user's other projects appeared in the tree and in branch
   history** (code comments, DESIGN.md, the design workbench, review notes,
   and relative paths outside the repository in `reference.css` and
   `shoot.mjs`).
   - The tree now names none of them: the comments and docs describe the
     design on its own terms, `reference.css` loads Manrope from
     `app/static/fonts`, and `shoot.mjs` finds Playwright in the project or
     `$NCD_PLAYWRIGHT`.
   - History: the branch was never pushed. It lands on main as one squashed
     commit, so only the clean tree is published.

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

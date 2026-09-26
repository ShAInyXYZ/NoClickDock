# M8 — design review, round 1 (README as the front page)

Reviewer: design supervisor, 2026-09-26. **Tree:** a clean `git archive` of
`508a809`.

**How the README was rendered:** `pandoc -f gfm` with GitHub-like
markdown CSS, dark and light, in a 896 px repository column and at 390 px
phone width. Every link and image was checked against the tree. Shots are
in `shots/M8/`.

| check | result |
|---|---|
| Says what it is in the first screen | **yes.** The seven marks, the name, "Every status you need, with no click.", the rail beside an open Docker panel, and a caption saying what the picture shows. The paragraph right below the hero explains the rest. `readme-dark-first-screen.png` |
| Broken links or images | **none.** 19 local references all resolve (7 marks, 11 screenshots, LICENSE); 3 external links; `#screenshots` has its heading. Every image loads in both themes. |
| Every image from invented data | **yes.** The 12 files in `assets/screenshots` are byte-identical to the `--mock` gallery captures (`design/readme-shots`). The only other images in the tree are app icons, design evidence and review shots from `--mock`, Xvfb or the reference. The Tailscale shot's `tail0c4fe` / `100.101.1.7` are invented (research's M3 leak scan). |
| Copy matches what was built | **yes:** checked against the tree. <ul><li>Poll cadences (30/30/2/30/20/2/30 s).</li><li>Menu items per widget.</li><li>Disk thresholds 85/92.</li><li>A20-a funnel wording.</li><li>`install.sh` flags (`--build --from --replace-legacy --restore-legacy --status --uninstall --no-start`) and the prefix `~/.local/share/noclickdock2`.</li><li>Config at `~/.config/noclickdock/config.json` and `%APPDATA%\NoClickDock`.</li><li>The dev ports 47810 / 1420.</li><li>`build-windows.sh` shipping `install.ps1`, and what `install.ps1` does.</li><li>Windows stated honestly: "compiles and its installer parses, but it has not yet been run on Windows".</li></ul> |
| Stale references | **none.** "Python" appears only as "NoClickDock 1 (the Python widgets)" in the migration line, and GTK only as the WebKitGTK/GTK 3 runtime requirement. No MSYS2, `shinywidget` or Emberdeck. |

Image placement and one imprecise line are in `FOLLOWUPS.md`:
- the hero and the Settings/toast pair show different scales side by side;
- Power at half scale;
- the snapping claim;
- the duplicated `readme-shots` folder.

VERDICT: PASS

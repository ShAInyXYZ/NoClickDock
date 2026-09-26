# Spec — disk

Parity reference: `widgets/disk/disk-status-checker.py` 1.1.0 (uses the shared
runtime, see [dock-core.md](dock-core.md)). Legend: **P** Python, **N** NCD 2,
**BUG** do not carry, **Q** decision for main.

## Identity

id `disk` · title `Disk` (P `DISK`) · order 40 · poll **30 s** · width: P
fitted to content; N 400 CSS px · mark: P Material "storage" (three stacked
drives, `M2 20h20v-4H2v4zm2-3h2v2H4v-2zM2 4v4h20V4H2zm4 3H4V5h2v2zm-4 7h20v-4H2v4zm2-3h2v2H4v-2z`,
viewBox 24) — monochrome in N.

## Data sources

### Filesystems — Linux

1. Read `/proc/mounts`; per line `dev mnt fstype …` (need ≥ 3 fields).
2. Skip `fstype` in: `tmpfs devtmpfs squashfs overlay proc sysfs cgroup
   cgroup2 devpts efivarfs autofs fuse.gvfsd-fuse fuse.portal ramfs mqueue
   hugetlbfs debugfs tracefs securityfs pstore bpf configfs fusectl
   binfmt_misc nsfs`.
3. Skip mount points starting with `/snap /var/snap /run /sys /proc /dev`
   (string prefix — note `/snapshots` is also skipped by P; N: match whole
   path components, `mnt == p || HasPrefix(mnt, p+"/")`).
4. Skip devices not starting with `/` (keeps `/dev/…`, drops `rpool/…` ZFS
   datasets and network `server:/share` — **Q**: P drops NFS/CIFS/ZFS; keep
   parity unless main wants network mounts).
5. De-duplicate by `stat(mnt).st_dev` (bind mounts, btrfs subvolumes of one
   fs), first wins; unstat-able mounts are dropped.
6. Sort: `/` first, then by mount path.
7. Usage per mount = `statfs`: `total = f_blocks·f_frsize`, `free =
   f_bavail·f_frsize` (space available to the user), `used = (f_blocks −
   f_bfree)·f_frsize` — exactly `shutil.disk_usage`. Note `used + free <
   total` on ext4 (root-reserved blocks): **frac = used / total**, as P.
8. Drop mounts with `total <= 0`.
9. Mount point strings in `/proc/mounts` escape spaces as `\040` — decode
   `\040 \011 \012 \134` (P shows them raw — **BUG**, minor).

### Filesystems — Windows

P: `GetLogicalDrives` bitmask → `A:\`…`Z:\`; keep `GetDriveTypeW` ∈ {3
DRIVE_FIXED, 4 DRIVE_REMOTE}; fstype = `GetVolumeInformationW` file-system
name lower-cased (`ntfs`, `refs`, `exfat`), else `network` for remote, `disk`
otherwise. Usage via `shutil.disk_usage` (= `GetDiskFreeSpaceExW`: total,
free-to-caller, used = total − total-free). Order: drive-letter order (P does
not sort Windows; `/` does not exist).

N: same calls from Go (`golang.org/x/sys/windows`). Label mounts `C:` (no
trailing backslash) and add the volume label when present (`C: System`).
Remote drives can block for seconds when the server is gone — query each
drive with its own 3 s timeout (goroutine) and drop drives that time out
(P could hang the whole poll).

### Docker reclaimable (both OSes)

Every **300 s** (and on first poll; cached between):

- `docker system df --format "{{.Type}}\t{{.Reclaimable}}"`, timeout 25 s.
  Empty output (docker missing, daemon down, permission) → no docker section
  at all (`None`), no error.
- Per line: type (`Images`, `Containers`, `Local Volumes`, `Build Cache`),
  reclaimable like `83.54GB (18%)` → take the size before `(`.
- `docker info --format "{{.DockerRootDir}}"`, timeout 10 s → where it would be
  freed (default `/`).
- **BUG**: P parses sizes with **binary** multipliers (`GB = 1<<30`) but Docker
  prints **decimal** units (`kB`, `MB`, `GB` = 10³ⁿ; go-units `HumanSize`) →
  reclaimable overstated by ~7 % at GB scale. N: decimal, as the docker widget
  already does (`_sizes` in docker). Parse `B`, `kB`/`KB`, `MB`, `GB`, `TB`.
- Windows (Docker Desktop): `DockerRootDir` is a path inside the VM
  (`/var/lib/docker`), meaningless on the host → say "inside Docker Desktop's
  disk image" instead of a path.

## Derived values and state

Per disk: `mount, fstype, total, used, free, frac`.

| state | condition | stateLabel (N; P upper-cased in a badge) |
|---|---|---|
| `idle` | no filesystems | `no filesystems` (P `NO FILESYSTEMS`) |
| `crit` | worst `frac ≥ 0.92` | `<mount> <pct>% full` of the worst disk, e.g. `/ 93% full` |
| `warn` | worst `frac ≥ 0.85` | same wording |
| `ok` | otherwise | `<fmt_bytes(min free over all disks)> free` — the **smallest** free space of any disk, not root's |

Percentages are rounded (`%.0f`).

## Glance

`<worst pct>%` (e.g. `86%`); tone `crit` ≥ 92, `warn` ≥ 85, else neutral.

## Toasts

- Transition: default = stateLabel of the new state (`/ 93% full`,
  `41G free`).
- Alert ids: every mount with `frac ≥ 0.85` → id = mount path.
- Alert message: `<mount>: <pct>% full · <fmt_bytes(free)> left`, tone warn
  (≥ 85) or crit (≥ 92) in N (P: always CRIT colour).

## Panel (single page)

P, top to bottom → N blocks:

1. `Section("FILESYSTEMS")` → `section {title: "Filesystems"}`.
2. No disks → `Note("No filesystems found", IDLE)` → `note {tone: idle}`.
3. Per disk `Meter(label=mount, frac, value="<free> free",
   color=ratio_color(frac, 0.85, 0.92), note="<used> used of <total>   ·
   <pct>%   ·   <fstype>")` → `meter {label, frac, value, note, tone}` with
   tone `ok`/`warn`/`crit` by 0.85/0.92 (DESIGN: the meter lights in the tone
   colour; ok meters are quiet).
4. If docker data: `Section("DOCKER · RECLAIMABLE")` → `section {title:
   "Docker · reclaimable"}`.
5. `Note("<total> could be freed on <root>", AMBER if total > 20 GiB else
   FG_DIM)` → `note`, tone `warn` above 20 GB (decimal in N) else none.
6. `Grid([(kind.lower(), AMBER if size > 10 GiB else IDLE, fmt_bytes(size))],
   columns=2)` → `chips {items:[{label:"images", tone, value:"41G"}],
   columns: 2}`, tone `warn` above 10 GB else `idle`. Order = docker's.
7. If total > 20 GB: `Note("docker system prune -a   ·   docker builder
   prune", AMBER)` → `note {mono: true, tone: warn}`.

No actions on this panel (prune lives on the docker widget's System page).

## Menu

P: `Open Disk Usage Analyzer` only when `baobab` is on PATH → runs `baobab`
(Popen, no args). N: menu item `open-analyzer` → core runs `baobab` detached
(Linux only, only when found at snapshot time). Windows: nothing
(`cleanmgr.exe` is not an analyzer; do not invent).

## Config

None. (No legacy file.)

## Windows summary

Drive letters as above; docker reclaimable works with Docker Desktop; no
analyzer menu item. Everything else identical.

## Subtle behaviours to keep

- `frac` = used/total including root-reserved blocks (a disk can read 95 %
  "full" while `df` shows less `Avail`) — matches P and `df`'s Use%.
- The OK label reports the **smallest** free space, which is the one that
  bites.
- `docker system df` is slow; never more than once per 300 s, never on the
  poll path's critical timing (run it in its own goroutine, publish when done).
- Bind-mount de-duplication by device id.

## Bugs not to carry

- Binary multipliers for Docker's decimal sizes.
- Raw `\040` escapes in mount names.
- Hung network drive blocks the poll (Windows).

## Tests (M3)

Fixture `/proc/mounts` with tmpfs, snap, bind mount, a `\040` path, ZFS
dataset; fixture `docker system df` output with `0B`, `1.2kB`, `83.54GB (18%)`,
`1.5TB`; state/label table tests at 0.849/0.85/0.919/0.92.

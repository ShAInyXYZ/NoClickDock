# Local Servers

Local Servers uses the same Go `hub.Collector` / `hub.Actor`, snapshot blocks,
440px panel, segmented pages, row LEDs, confirmation pills, action results and
Settings registration as Docker. It is enabled by default after Docker.

## Discovery

Linux only. Every 10 seconds (or Refresh), read `/proc/net/tcp{,6}`, match
listening socket inodes to `/proc/<pid>/fd`, and inspect the owning process.
No connections, HTTP requests, port sweeps, environment replay, or changes to
existing servers occur during scanning. Loopback and wildcard TCP listeners are
included; remote-interface-only listeners, UDP and container network namespaces
are outside this widget. Unreadable owners appear under Show other services.
The URL uses the bound loopback address, or a loopback address for wildcard
listeners; HTTP is assumed, so TLS-only and non-HTTP services may need another URL.

Group by the nearest package/project directory, using package.json's name when
available. Recognize Node.js, Bun, Deno, Python, Ruby, PHP, Java and .NET runtimes;
Vite, Next.js, Astro, Nuxt, Webpack, React Scripts, Uvicorn, Gunicorn, Django,
Flask, Python HTTP, Rails and Laravel command signatures; Go and Rust project
manifests. Other processes with Python/Ruby/PHP/Go/Rust manifests can also be
classified as development projects. Classification is heuristic; unrelated and
protected services stay behind Show other services.

Each process appears once with its ports, PID, runtime, status, directory and
URL shortcuts. Servers that disappear are shown on Stopped for this NCD session
(up to 128 history entries). A new owner of an old port has a new identity.

## Controls and safety

* Production controls use the existing Tauri → stdio → Go path. The development
  HTTP transport refuses Local Servers action/ask requests, including requests
  without an Origin. Browser previews are read-only. There is no command RPC.
* The hub accepts only actions and exact arguments offered in its last snapshot.
  The actor checks them again. A target contains PID, start ticks, UID,
  executable, working directory, argv, cgroup and socket inode identity.
* Stop opens a Linux pidfd, verifies the identity and current listener ownership,
  then signals that process with SIGTERM. It waits up to eight seconds and never
  escalates to SIGKILL. Kernels without pidfd support fail closed.
* No parent terminal or shared process group is signalled. Shared listening
  sockets, other users, NCD/ancestors, and known infrastructure (including Docker,
  Tailscale, databases and BookMarkeep) are read-only. PM2, Supervisor, Forever and
  nodemon children are left to their supervisors.
* A systemd **user** service can be controlled only when its MainPID is the
  listener, its verified cgroup contains exactly that process, and its invocation
  still matches. Stop/restart is delegated to that service's existing policy.
  Shared units, wrappers and worker pools are read-only. There is no system-wide
  systemctl or privilege escalation. External stopped units must be started with
  their manager; NCD does not guess their stopped instance identity.
* Open/copy row shortcuts reuse native menu items. The shell resolves their IDs
  against the latest snapshot and permits only HTTP(S) URLs without credentials.

## Registering a launch

For an unmanaged process, Restart is absent until an explicit launch file is
valid. **Register** creates and opens a private JSON template in
`<NCD config directory>/local-servers/<project-and-port-hash>.json`. Fill `argv`
with an **absolute executable** and separate arguments, save, then refresh:

```json
{
  "directory": "/home/dev/projects/example",
  "argv": ["/usr/bin/node", "/home/dev/projects/example/node_modules/vite/bin/vite.js", "--port", "5173"]
}
```

The directory must equal the observed process working directory. Files must be
owned by the current user, mode 0600, and regular files, not symlinks. Shells,
terminals, package runners (`npm`, `npx`, `pnpm`, `yarn`), infrastructure and process
managers are refused: register the actual server executable. No shell syntax is
interpreted. Arguments or paths containing `$`, `%` or newlines are refused to
avoid environment/specifier expansion on older systemd versions. NCD does not prefill or display the original arguments or environment.
A bad registration is explained in the panel and cannot stop the existing server.

Registered launches require a working user systemd session and `systemd-run`.
They run in isolated `ncd-dev-<hash>.service` units with a clean environment:
only HOME and a fixed PATH are supplied. Use an absolute virtualenv interpreter
when needed. Terminal-specific variables, secrets, shell initialization, input
streams and reload/worker trees are not reproduced. Use a dedicated service
configured outside NCD for applications requiring those features.

Restart validates the launch before stopping, checks that the old ports are
free, starts the dedicated unit, and waits up to 12 seconds for that unit's new
process to listen on a previous port. A timeout says launch was requested rather
than claiming success. A newly occupied port is never terminated. Registered
commands persist; stopped process history is in memory for this NCD session.

## Logs and states

Verified user-service and registered-launch logs come from the journal, limited
to 20 lines for the exact invocation. Lines containing command argument or
observed environment values, or credential-like patterns, are hidden. Values
used to redact stopped-service logs remain backend-only in bounded session
history. Unmanaged terminal logs cannot be recovered; NCD does not read terminal
file descriptors. No raw command, environment, or CLI error output is serialized.

The shared panel handles loading/staleness; explicit empty/unavailable snapshots,
per-server progress and persistent action errors cover failures. No new CSS theme
or widget-specific frontend renderer is required.

## Verification

`go test -race ./internal/widgets/localservers ./internal/transport` exercises
real disposable listeners and isolated user services: grouping, graceful stop,
sibling preservation, exit races, stale identity/socket refusal, port reuse,
registered handoff and start-after-stop, manager restart, stopped journal access,
registration validation, credential exclusion and HTTP action denial. Systemd
tests skip if the host has no user manager. No existing server is controlled.

## Compact preview and launch settings

Each running endpoint has one address row, with runtime/PID, status, copy and
available process controls. The address opens its full local URL through the
same snapshot-owned native menu shortcut. Multiple ports share process controls
on the first endpoint only. No separate port-only row is drawn.

Launch registration and missing-launch explanations live in Settings → Local
Servers, including stopped servers. The private-file editor workflow and
identity checks are unchanged. Registration requires the existing second click;
commands are never copied into the snapshot or settings window.

## Project icons

The trusted backend fetches a running development server's HTML, resolves up to
three `<link rel="icon">` candidates, then tries `/favicon.ico`. Only the same
loopback host/port is allowed (including a localhost alias); requests use no
proxy, cookies, credentials, or external redirects. Listener identity is
verified before each connection and after downloading. Shared supervisors may
block process controls while still allowing these read-only requests.

Two workers fetch in the background, with at most sixteen pending jobs, a
three-second job deadline, and bounded page/image sizes (512 KiB / 64 KiB).
Scans never wait for network requests. Completed icons appear on the next normal
poll, without extra process scans. The in-memory cache holds at most 128 entries:
successes for ten minutes, misses for two. Keys include process identity,
project directory, and endpoint; NCD restart clears the cache.

Project headings use a fixed 18px icon slot, falling back to two initials while
loading, on failure, or if the webview cannot decode the image. PNG, JPEG, GIF,
and ICO are accepted with size checks. SVG is reduced to inert geometry:
scripts, CSS, text, embedded images, and external references are excluded.
The webview receives only validated image data URLs, never fetched HTML or a
remote image URL. Projects with multiple servers share the first available icon.

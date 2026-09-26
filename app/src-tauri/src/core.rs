//! ncd-core as a supervised sidecar. Its stdout lines become "core" events
//! for every window, and `core_send` is the only way to its stdin. The shell
//! keeps the latest hello, config, snapshot and tick per widget, so a window
//! that loads late, or reloads, starts from the full state.
use std::collections::BTreeMap;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Serialize;
use serde_json::Value;
use tauri::{AppHandle, Emitter, Manager};

use crate::toast;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

/// The only messages a page may send; anything else never reaches the core.
const ALLOWED: [&str; 4] = ["action", "refresh", "config.patch", "ask"];
/// A core that has not said hello this long after starting is reported.
const HELLO_WITHIN: Duration = Duration::from_secs(8);

pub struct Core {
    args: Vec<String>,
    child: Mutex<Option<CommandChild>>,
    quitting: AtomicBool,
    cache: Mutex<Cache>,
    link: Mutex<Link>,
    /// The core's last line on stderr: usually the reason it stopped.
    last_err: Mutex<Option<String>>,
    /// Counts starts, so a watchdog knows which start it is watching, and
    /// the start whose process is running now (0: none).
    starts: AtomicU64,
    running: AtomicU64,
    actions: AtomicU64,
}

/// Whether the core is answering, and if not, why — shown on the rail as a
/// dial of its own, never an invisible app.
#[derive(Clone, Default, Serialize)]
pub struct Link {
    pub up: bool,
    pub error: Option<String>,
    /// When it stopped answering (ms since the epoch), while it is down.
    pub since: Option<u64>,
}

#[derive(Default)]
struct Cache {
    hello: Option<String>,
    config: Option<Value>,
    config_line: Option<String>,
    snapshots: BTreeMap<String, String>,
    ticks: BTreeMap<String, String>,
}

impl Core {
    pub fn new(args: Vec<String>) -> Self {
        Self {
            args,
            child: Mutex::new(None),
            quitting: AtomicBool::new(false),
            cache: Mutex::new(Cache::default()),
            link: Mutex::new(Link::default()),
            last_err: Mutex::new(None),
            starts: AtomicU64::new(0),
            running: AtomicU64::new(0),
            actions: AtomicU64::new(0),
        }
    }

    /// A widget's right-click items, as its last snapshot offered them.
    pub fn menu_of(&self, widget: &str) -> Vec<Value> {
        let Ok(cache) = self.cache.lock() else { return Vec::new() };
        cache
            .snapshots
            .get(widget)
            .and_then(|line| serde_json::from_str::<Value>(line).ok())
            .and_then(|message| message.pointer("/widget/menu").cloned())
            .and_then(|menu| menu.as_array().cloned())
            .unwrap_or_default()
    }

    /// A fresh id for an action the shell sends on the user's behalf.
    pub fn action_id(&self) -> String {
        format!("shell-{}", self.actions.fetch_add(1, Ordering::Relaxed))
    }

    /// The dock's section of the config, as the core last announced it.
    pub fn dock_config(&self) -> Option<Value> {
        self.cache.lock().ok()?.config.as_ref()?.get("dock").cloned()
    }
}

/// Runs the core for the life of the app: a crash is logged and the core is
/// started again, backing off while it keeps failing.
pub fn supervise(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        let mut backoff = Duration::from_millis(250);
        while !app.state::<Core>().quitting.load(Ordering::Acquire) {
            let args = app.state::<Core>().args.clone();
            let spawned = app.shell().sidecar("ncd-core").map(|c| c.args(args)).and_then(|c| c.spawn());
            let (mut events, child) = match spawned {
                Ok(pair) => pair,
                Err(error) => {
                    eprintln!("ncd: could not start ncd-core: {error}");
                    set_link(&app, false, Some(format!("ncd-core could not be started: {error}")));
                    tokio::time::sleep(backoff).await;
                    backoff = (backoff * 2).min(Duration::from_secs(10));
                    continue;
                }
            };
            if let Ok(mut slot) = app.state::<Core>().child.lock() {
                *slot = Some(child);
            }
            if let Ok(mut last) = app.state::<Core>().last_err.lock() {
                last.take();
            }
            watch_hello(&app);
            let started = Instant::now();
            while let Some(event) = events.recv().await {
                match event {
                    // One event per line, the newline included.
                    CommandEvent::Stdout(bytes) => relay(&app, String::from_utf8_lossy(&bytes).trim_end()),
                    CommandEvent::Stderr(bytes) => {
                        let line = String::from_utf8_lossy(&bytes).trim_end().to_owned();
                        eprintln!("{line}");
                        if let Ok(mut last) = app.state::<Core>().last_err.lock() {
                            let reason = line.strip_prefix("ncd-core: ").unwrap_or(&line);
                            *last = Some(reason.chars().take(160).collect());
                        }
                    }
                    CommandEvent::Terminated(status) => {
                        app.state::<Core>().running.store(0, Ordering::Release);
                        eprintln!("ncd: ncd-core exited (code {:?}, signal {:?})", status.code, status.signal);
                        if !app.state::<Core>().quitting.load(Ordering::Acquire) {
                            let mut why = match (status.code, status.signal) {
                                (Some(code), _) => format!("ncd-core stopped with exit code {code}"),
                                (_, Some(signal)) => format!("ncd-core was stopped by signal {signal}"),
                                _ => "ncd-core stopped".to_owned(),
                            };
                            if let Some(reason) = app.state::<Core>().last_err.lock().ok().and_then(|mut l| l.take()) {
                                why = format!("{why} ({reason})");
                            }
                            set_link(&app, false, Some(why));
                        }
                        break;
                    }
                    _ => {}
                }
            }
            if let Ok(mut slot) = app.state::<Core>().child.lock() {
                slot.take();
            }
            if started.elapsed() > Duration::from_secs(30) {
                backoff = Duration::from_millis(250);
            }
            tokio::time::sleep(backoff).await;
            backoff = (backoff * 2).min(Duration::from_secs(10));
        }
    });
}

/// Starts a watchdog for this start: a core that runs but never says hello
/// is as broken as one that does not start. Only a process that is still
/// running is "not answering"; one that exited has already said why
/// (design M2 r1b: the watchdog's words replaced an exit's).
fn watch_hello(app: &AppHandle) {
    let start = app.state::<Core>().starts.fetch_add(1, Ordering::AcqRel) + 1;
    app.state::<Core>().running.store(start, Ordering::Release);
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(HELLO_WITHIN).await;
        let core = app.state::<Core>();
        let silent = core.starts.load(Ordering::Acquire) == start
            && core.running.load(Ordering::Acquire) == start
            && !core.link.lock().map(|l| l.up).unwrap_or(false);
        if silent {
            set_link(&app, false, Some("ncd-core started but has not answered".to_owned()));
        }
    });
}

fn set_link(app: &AppHandle, up: bool, error: Option<String>) {
    let now = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(0);
    let mut link = Link { up, error, since: None };
    if let Ok(mut current) = app.state::<Core>().link.lock() {
        // Down since the first failure, not the latest retry.
        if !up {
            link.since = Some(current.since.filter(|_| !current.up).unwrap_or(now));
        }
        *current = link.clone();
    }
    let _ = app.emit("core-link", link);
}

/// Whether the core is answering, for a page that loads after the fact.
#[tauri::command]
pub fn core_link(core: tauri::State<Core>) -> Link {
    core.link.lock().map(|l| l.clone()).unwrap_or_default()
}

/// Remembers what a line changes, then hands it to every window. This runs
/// on the sidecar's reader task, off the main thread: it parses, caches and
/// emits, and anything that places a window (a toast) is handed to the main
/// thread by the module that owns it.
fn relay(app: &AppHandle, line: &str) {
    if line.is_empty() {
        return;
    }
    let Ok(message) = serde_json::from_str::<Value>(line) else {
        eprintln!("ncd: ncd-core wrote a line that is not JSON");
        return;
    };
    let kind = message.get("type").and_then(Value::as_str);
    crate::watchdog::note(format!("core → {}", kind.unwrap_or("?")));
    if let Ok(mut cache) = app.state::<Core>().cache.lock() {
        match kind {
            Some("hello") => {
                // A (re)started core: everything before it is gone.
                *cache = Cache {
                    hello: Some(line.to_owned()),
                    config: message.get("config").cloned(),
                    ..Cache::default()
                };
            }
            Some("snapshot") => {
                if let Some(id) = message.pointer("/widget/id").and_then(Value::as_str) {
                    cache.ticks.remove(id);
                    cache.snapshots.insert(id.to_owned(), line.to_owned());
                }
            }
            Some("tick") => {
                if let Some(id) = message.get("widget").and_then(Value::as_str) {
                    cache.ticks.insert(id.to_owned(), line.to_owned());
                }
            }
            Some("config") => {
                cache.config = message.get("config").cloned();
                cache.config_line = Some(line.to_owned());
            }
            _ => {}
        }
    }
    let _ = app.emit("core", line);
    match kind {
        Some("hello") => set_link(app, true, None),
        Some("toast") => {
            if let Some(t) = message.get("toast") {
                let field = |k: &str| t.get(k).and_then(Value::as_str).unwrap_or_default().to_owned();
                toast::push(app, field("widget"), field("tone"), field("text"));
            }
        }
        _ => {}
    }
}

/// The full state, in the order the core itself would replay it.
#[tauri::command]
pub fn core_state(core: tauri::State<Core>) -> Vec<String> {
    let Ok(cache) = core.cache.lock() else { return Vec::new() };
    let mut lines: Vec<String> = cache.hello.iter().chain(cache.config_line.iter()).cloned().collect();
    for (id, snapshot) in &cache.snapshots {
        lines.push(snapshot.clone());
        if let Some(tick) = cache.ticks.get(id) {
            lines.push(tick.clone());
        }
    }
    lines
}

/// A page's message to the core: parsed, limited to the UI → core types, and
/// re-serialised, so it is always exactly one line.
#[tauri::command]
pub fn core_send(app: AppHandle, line: String) -> Result<(), String> {
    let message: Value = serde_json::from_str(&line).map_err(|_| "The message is not JSON.")?;
    match message.get("type").and_then(Value::as_str) {
        Some(kind) if ALLOWED.contains(&kind) => send(&app, &message),
        _ => Err("That message type is not allowed.".into()),
    }
}

/// Writes one message to the core's stdin.
pub fn send(app: &AppHandle, message: &Value) -> Result<(), String> {
    crate::watchdog::note(format!("core ← {}", message.get("type").and_then(Value::as_str).unwrap_or("?")));
    let mut bytes = serde_json::to_vec(message).map_err(|e| e.to_string())?;
    bytes.push(b'\n');
    let core = app.state::<Core>();
    let mut child = core.child.lock().map_err(|_| "The core is unavailable.")?;
    child
        .as_mut()
        .ok_or("ncd-core is not running.")?
        .write(&bytes)
        .map_err(|e| e.to_string())
}

/// Stops the core with the app. The plugin only kills children spawned from
/// JavaScript; the core would also exit on its own when stdin closes.
pub fn shutdown(app: &AppHandle) {
    let core = app.state::<Core>();
    core.quitting.store(true, Ordering::Release);
    let child = core.child.lock().ok().and_then(|mut slot| slot.take());
    if let Some(child) = child {
        let _ = child.kill();
    }
}

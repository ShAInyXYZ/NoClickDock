//! ncd-core as a supervised sidecar. Its stdout lines become "core" events
//! for every window, and `core_send` is the only way to its stdin. The shell
//! keeps the latest hello, config, snapshot and tick per widget, so a window
//! that loads late, or reloads, starts from the full state.
use std::collections::BTreeMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde_json::Value;
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

/// The only messages a page may send; anything else never reaches the core.
const ALLOWED: [&str; 3] = ["action", "refresh", "config.patch"];

pub struct Core {
    args: Vec<String>,
    child: Mutex<Option<CommandChild>>,
    quitting: AtomicBool,
    cache: Mutex<Cache>,
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
        }
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
                    tokio::time::sleep(backoff).await;
                    backoff = (backoff * 2).min(Duration::from_secs(10));
                    continue;
                }
            };
            if let Ok(mut slot) = app.state::<Core>().child.lock() {
                *slot = Some(child);
            }
            let started = Instant::now();
            while let Some(event) = events.recv().await {
                match event {
                    // One event per line, the newline included.
                    CommandEvent::Stdout(bytes) => relay(&app, String::from_utf8_lossy(&bytes).trim_end()),
                    CommandEvent::Stderr(bytes) => eprintln!("{}", String::from_utf8_lossy(&bytes).trim_end()),
                    CommandEvent::Terminated(status) => {
                        eprintln!("ncd: ncd-core exited (code {:?}, signal {:?})", status.code, status.signal);
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

/// Remembers what a line changes, then hands it to every window.
fn relay(app: &AppHandle, line: &str) {
    if line.is_empty() {
        return;
    }
    let Ok(message) = serde_json::from_str::<Value>(line) else {
        eprintln!("ncd: ncd-core wrote a line that is not JSON");
        return;
    };
    if let Ok(mut cache) = app.state::<Core>().cache.lock() {
        match message.get("type").and_then(Value::as_str) {
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

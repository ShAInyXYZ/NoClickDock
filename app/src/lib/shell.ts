// The Tauri shell's side of the hover and geometry protocol. Rust owns every
// window: the pages only report what they measured (in CSS px) and where the
// pointer is, and are told which panel is open.
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { getCurrentWindow } from "@tauri-apps/api/window";
import type { Side } from "./dock/side";
import type { Tone } from "./protocol";

export interface Rect { x: number; y: number; width: number; height: number }

export interface RailLayout {
  width: number;
  height: number;
  /** window.innerWidth, so the shell can check its CSS-to-physical scale. */
  viewport: number;
  dials: (Rect & { widget: string })[];
}

export interface PanelOpen {
  widget: string;
  /** The page last chosen for this widget, in either panel window. */
  page?: string | null;
  /** Echoed back with the panel's size, so a stale measurement is ignored. */
  seq: number;
  side: Side;
  /** The tallest the panel may be on this monitor, in CSS px. */
  maxHeight: number;
}

export interface PanelState { widget: string | null; side: Side }

/** Sends an error to the shell's stderr; a production webview has no console. */
export function report(error: unknown) {
  const message = error instanceof Error ? `${error.message}\n${error.stack ?? ""}` : String(error);
  invoke("page_error", { message }).catch(() => {});
}

/** Everything a page throws and nobody catches goes to the shell too. */
export function reportUncaught() {
  window.addEventListener("error", (event) => report(event.error ?? event.message));
  window.addEventListener("unhandledrejection", (event) => report(event.reason));
}

export const railLayout = (layout: RailLayout) => invoke<void>("rail_layout", { layout });
export const hoverDial = (widget: string | null) => invoke<void>("hover_dial", { widget });
export const hoverPanel = (inside: boolean) => invoke<void>("hover_panel", { inside });
export const panelLayout = (seq: number, width: number, height: number) =>
  invoke<void>("panel_layout", { seq, width, height });
/** Painted at this size (the one given to `panelLayout`); an older size shows nothing. */
export const panelPainted = (seq: number, width: number, height: number) =>
  invoke<void>("panel_painted", { seq, width, height });
/** A tab chosen: the shell keeps it for the widget, for both panel windows. */
export const panelPage = (widget: string, page: string) => invoke<void>("panel_page", { widget, page });
export const startDrag = () => getCurrentWindow().startDragging();
export const dialMenu = (widget: string) => invoke<void>("dial_menu", { widget });
export const railMenu = () => invoke<void>("rail_menu");

/** The toast stack the shell wants drawn, newest first. */
export interface ToastStackMessage {
  seq: number;
  side: Side;
  items: { key: number; tone: Tone; text: string; leaving: boolean }[];
}
export const onToasts = (fn: (stack: ToastStackMessage) => void): Promise<UnlistenFn> =>
  listen<ToastStackMessage>("toasts", (event) => fn(event.payload));
export const toastLayout = (seq: number, width: number, height: number, anchor: number) =>
  invoke<void>("toast_layout", { seq, width, height, anchor });
export const toastDismiss = () => invoke<void>("toast_dismiss");

// The shell has two panel windows and addresses one at a time: a panel page
// hears only what is sent to its own window (a listener without a target
// would hear what is sent to the other one too).
const own = () => ({ target: getCurrentWindow().label });
export const onPanelOpen = (fn: (open: PanelOpen) => void): Promise<UnlistenFn> =>
  listen<PanelOpen>("panel-open", (event) => fn(event.payload), own());
export const onPanelState = (fn: (state: PanelState) => void): Promise<UnlistenFn> =>
  listen<PanelState>("panel-state", (event) => fn(event.payload));

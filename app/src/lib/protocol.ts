// The wire contract with ncd-core, mirrored from core/internal/model.
// docs/rewrite/ARCHITECTURE.md is the authority for both.

export type State = "ok" | "warn" | "crit" | "idle" | "busy";
/** A tone colours one element; absent means neutral. Never a raw colour. */
export type Tone = "ok" | "warn" | "crit" | "idle" | "info" | "busy";

export interface Action {
  id: string;
  label: string;
  confirm?: string;
  danger?: boolean;
  args?: Record<string, string>;
}

export interface SectionBlock { kind: "section"; title: string; meta?: string; tone?: Tone }
export interface RowBlock {
  kind: "row"; name: string; sub?: string; value?: string; tone?: Tone; led?: Tone; mono?: boolean; actions?: Action[];
}
export interface MeterBlock {
  kind: "meter"; label: string; frac: number; value?: string; note?: string; tone?: Tone; mono?: boolean;
}
export interface LimitBlock { kind: "limit"; label: string; pct: number; resets?: string; tone?: Tone }
export interface StatsBlock { kind: "stats"; items: { key: string; value: string; tone?: Tone }[] }
export interface ChipsBlock { kind: "chips"; items: { label: string; tone: Tone; value?: string }[]; columns: number }
export interface AlertBlock {
  kind: "alert"; title: string; meta?: string; body?: string; mono?: boolean; tone: Tone; actions?: Action[];
}
export interface NoteBlock { kind: "note"; text: string; tone?: Tone; mono?: boolean }
/** A table cell: text, or text with its own tone (a hot VRAM junction). */
export type TableCell = string | { text: string; tone?: Tone };
export interface TableBlock {
  kind: "table";
  columns: { label: string; align?: "left" | "right"; mono?: boolean }[];
  /** A row with `group` opens a group of rows: a strip with its title and meta, no cells. */
  rows: { cells: TableCell[]; tone?: Tone; led?: Tone; sub?: string; group?: string; meta?: string }[];
}
export interface DonutBlock { kind: "donut"; segments: { label: string; value: number; display: string }[]; center: string; unit?: string }
/** A null value was not measured: drawn as a floor stub, never a short bar. */
export interface BarsBlock { kind: "bars"; values: (number | null)[]; labels?: string[]; highlight?: number; unit?: string }
export interface SplitBlock { kind: "split"; columns: Block[][] }
/** One honest sentence when there is nothing to list, then any commands that would change it. */
export interface EmptyBlock { kind: "empty"; text: string; commands?: string[] }

export type Block =
  | SectionBlock | RowBlock | MeterBlock | LimitBlock | StatsBlock | ChipsBlock
  | AlertBlock | NoteBlock | TableBlock | DonutBlock | BarsBlock | SplitBlock | EmptyBlock;

export interface Page {
  id: string;
  label: string;
  badge?: string;
  tone?: Tone;
  blocks: Block[];
}

export interface WidgetSnapshot {
  id: string;
  title: string;
  order: number;
  state: State;
  stateLabel: string;
  glance: { text: string; tone?: Tone };
  /** 0..1 while busy; absent draws the indeterminate arc. */
  progress?: number;
  updatedAt: string;
  pollSecs: number;
  error?: string;
  /** Preferred panel width in logical px; absent means --panel-w. */
  width?: number;
  pages: Page[];
  footer?: string;
  /** Right-click entries; one with `url` or `copy` is handled by the shell itself. */
  menu?: { id: string; label: string; url?: string; copy?: string }[];
}

/** What every widget's section has: whether it runs, and its place. */
export interface WidgetConfig {
  enabled: boolean;
  order: number;
}

export interface PowerConfig extends WidgetConfig {
  price_per_kwh: number;
  currency: string;
  overhead_watts: number;
  peripheral_watts: number;
  poll_interval: number;
  log_interval: "off" | "hourly" | "daily" | "weekly" | "monthly";
  tou: boolean;
  peak_rate: number | null;
  off_peak_rate: number | null;
  peak_hours: string;
  tou_weekend_offpeak: boolean;
}

export type Edge = "left" | "right" | "top" | "bottom" | "";

export interface Config {
  dock: {
    orientation: "vertical" | "horizontal";
    density: "compact" | "normal" | "large";
    readouts: boolean;
    position?: { monitor?: string; edge?: Edge; along?: number; x: number; y: number };
  };
  start?: { launchAtLogin: boolean };
  widgets?: {
    claude?: WidgetConfig;
    codex?: WidgetConfig;
    comfyui?: WidgetConfig & { host: string; port: number };
    disk?: WidgetConfig;
    docker?: WidgetConfig;
    power?: PowerConfig;
    /** watch: null watches every peer seen online since the core started. */
    tailscale?: WidgetConfig & { watch: string[] | null };
  };
}

export interface Toast {
  widget: string;
  tone: Tone;
  text: string;
  at: string;
}

/** Core → UI: one stdout line, or one SSE event named after its type. */
export type CoreMessage =
  | { type: "hello"; version: string; platform: string; config: Config; widgets: string[] }
  | { type: "snapshot"; widget: WidgetSnapshot }
  | { type: "toast"; toast: Toast }
  | { type: "tick"; widget: string; at: string }
  | { type: "config"; config: Config }
  | { type: "running"; id: string; widget: string; action: string; args?: Record<string, string> }
  | { type: "result"; id: string; ok: boolean; message: string; widget?: string; action?: string; args?: Record<string, string> }
  | { type: "config.result"; id: string; ok: boolean; field?: string; message?: string }
  | { type: "answer"; id: string; ok: boolean; message?: string; items?: string[] };

/** UI → core: one stdin line, or one POST /v1/send. */
export type UiMessage =
  | { type: "action"; id: string; widget: string; action: string; args?: Record<string, string> }
  | { type: "refresh"; widget?: string }
  | { type: "config.patch"; id?: string; patch: DeepPartial<Config> }
  | { type: "ask"; id: string; widget: string; what: string; args?: Record<string, string> };

export type DeepPartial<T> = { [K in keyof T]?: T[K] extends object ? DeepPartial<T[K]> | null : T[K] | null };

export const CORE_MESSAGE_TYPES = ["hello", "snapshot", "tick", "toast", "config", "running", "result", "config.result", "answer"] as const;

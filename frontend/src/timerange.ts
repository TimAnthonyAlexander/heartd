// Time-range model for the dashboard charts. A range is either a "live" rolling
// window (a preset like 1h that follows the clock and keeps appending fresh
// samples) or a "fixed" absolute [from, to] window (the custom picker with a
// past end) shown statically. The hook seeds history for the window on every
// range change and only live-appends for live ranges.

// RETENTION_MS is the furthest back a custom range may reach — the server's
// 7-day retention horizon. The backend clamps too; this keeps the picker honest.
export const RETENTION_MS = 7 * 24 * 60 * 60 * 1000

export interface RangePreset {
  key: string
  label: string
  spanMs: number
}

export const RANGE_PRESETS: RangePreset[] = [
  { key: '15m', label: '15m', spanMs: 15 * 60_000 },
  { key: '1h', label: '1h', spanMs: 60 * 60_000 },
  { key: '6h', label: '6h', spanMs: 6 * 60 * 60_000 },
  { key: '24h', label: '24h', spanMs: 24 * 60 * 60_000 },
  { key: '7d', label: '7d', spanMs: 7 * 24 * 60 * 60_000 },
]

export interface TimeRange {
  key: string // identifies the active preset, or 'custom'
  live: boolean // rolling window that follows now and keeps appending
  spanMs: number // window width; for live ranges this is the rolling width
  from?: number // epoch ms, only set for fixed (custom) ranges
  to?: number // epoch ms, only set for fixed (custom) ranges
}

export function livePreset(p: RangePreset): TimeRange {
  return { key: p.key, live: true, spanMs: p.spanMs }
}

// customRange builds a fixed window, clamping to [now - retention, now] so it can
// never ask for data outside what the server retains.
export function customRange(fromMs: number, toMs: number): TimeRange {
  const now = Date.now()
  const from = Math.max(fromMs, now - RETENTION_MS)
  const to = Math.min(toMs, now)
  return { key: 'custom', live: false, spanMs: Math.max(0, to - from), from, to }
}

// resolveWindow returns the [fromSec, toSec] epoch-second window to fetch for a
// range, resolving "live" ranges against the current clock.
export function resolveWindow(r: TimeRange): { fromSec: number; toSec: number } {
  const now = Date.now()
  const toMs = r.live ? now : (r.to ?? now)
  const fromMs = r.live ? now - r.spanMs : (r.from ?? now - r.spanMs)
  return { fromSec: Math.floor(fromMs / 1000), toSec: Math.ceil(toMs / 1000) }
}

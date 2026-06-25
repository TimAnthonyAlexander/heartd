import { createTheme } from '@mui/material'

// A calm, minimal dark theme. Near-black backgrounds with a single soft accent,
// muted status colors, generous spacing, and tabular figures for steady numbers.
export const colors = {
  bg: '#0d0f13',
  panel: '#15181e',
  panelHover: '#1a1e25',
  border: 'rgba(255,255,255,0.07)',
  text: '#e7e9ec',
  textDim: '#8b9099',
  textFaint: '#5c626b',
  accent: '#6ea8fe',
  ok: '#4ec27e',
  warn: '#e0a64b',
  error: '#ec6a5e',
  unknown: '#6e7681',
}

// Maps a logical status to a color. Node statuses (ok/down/unknown) and check
// statuses (ok/failing/unknown) both route through here.
export function statusColor(status: string): string {
  switch (status) {
    case 'ok':
      return colors.ok
    case 'failing':
    case 'down':
      return colors.error
    default:
      return colors.unknown
  }
}

// Threshold coloring for a 0-100 percentage (CPU/mem/disk).
export function percentColor(percent: number): string {
  if (percent >= 90) return colors.error
  if (percent >= 70) return colors.warn
  return colors.ok
}

export const theme = createTheme({
  palette: {
    mode: 'dark',
    background: { default: colors.bg, paper: colors.panel },
    text: { primary: colors.text, secondary: colors.textDim },
    primary: { main: colors.accent },
    success: { main: colors.ok },
    warning: { main: colors.warn },
    error: { main: colors.error },
    divider: colors.border,
  },
  shape: { borderRadius: 10 },
  typography: {
    fontFamily:
      '"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    // Tabular figures so changing numbers don't jitter horizontally.
    allVariants: { fontFeatureSettings: '"tnum", "cv01"' },
    h1: { fontSize: '2.6rem', fontWeight: 600, letterSpacing: '-0.02em' },
    h6: { fontWeight: 600, letterSpacing: '-0.01em' },
    overline: { letterSpacing: '0.08em', fontWeight: 600 },
  },
  components: {
    MuiPaper: {
      styleOverrides: {
        root: { backgroundImage: 'none', border: `1px solid ${colors.border}` },
      },
    },
    MuiCssBaseline: {
      styleOverrides: {
        body: { backgroundColor: colors.bg },
        '*::-webkit-scrollbar': { width: 8, height: 8 },
        '*::-webkit-scrollbar-thumb': {
          background: 'rgba(255,255,255,0.12)',
          borderRadius: 8,
        },
      },
    },
  },
})

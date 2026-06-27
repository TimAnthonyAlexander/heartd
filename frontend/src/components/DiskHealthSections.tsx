import { Box, Typography } from '@mui/material'
import type { DiskHealth, RaidArray, SmartDisk } from '../api'
import { colors } from '../theme'

// severity is an ordered health level shared by RAID arrays and SMART disks so
// the Disk card's header badge can take the worst of both independent sources.
export type Severity = 'ok' | 'warn' | 'error'

const SEVERITY_RANK: Record<Severity, number> = { ok: 0, warn: 1, error: 2 }

export function severityColor(sev: Severity): string {
  if (sev === 'error') return colors.error
  if (sev === 'warn') return colors.warn
  return colors.ok
}

// raidSeverity maps a RAID state to a severity: clean→ok, rebuilding→warn,
// degraded/failed→error.
function raidSeverity(state: RaidArray['state']): Severity {
  if (state === 'clean') return 'ok'
  if (state === 'rebuilding') return 'warn'
  return 'error'
}

// smartSeverity maps a per-disk rollup to a severity: ok→ok, warn→warn, fail→error.
function smartSeverity(rollup: SmartDisk['rollup']): Severity {
  if (rollup === 'ok') return 'ok'
  if (rollup === 'warn') return 'warn'
  return 'error'
}

// worstSeverity is the most severe level across all RAID arrays and SMART disks
// present, or null when neither source has any data (so the Disk card can omit
// its health badge entirely on hosts without RAID/SMART).
export function worstSeverity(health: DiskHealth): Severity | null {
  if (health.raid.length === 0 && health.smart.length === 0) return null
  let worst: Severity = 'ok'
  for (const r of health.raid) {
    const s = raidSeverity(r.state)
    if (SEVERITY_RANK[s] > SEVERITY_RANK[worst]) worst = s
  }
  for (const d of health.smart) {
    const s = smartSeverity(d.rollup)
    if (SEVERITY_RANK[s] > SEVERITY_RANK[worst]) worst = s
  }
  return worst
}

function formatHours(h: number): string {
  if (h >= 1000) return `${(h / 1000).toFixed(1)}k h`
  return `${h} h`
}

// Counter shows one SMART counter, colored when non-zero per its severity.
function Counter({ label, value, severity }: { label: string; value: number; severity: Severity }) {
  const color = value > 0 ? severityColor(severity) : colors.textFaint
  return (
    <Typography sx={{ fontSize: 11, color }}>
      {label} <Box component="span" sx={{ fontWeight: 600 }}>{value}</Box>
    </Typography>
  )
}

// DiskHealthSections renders the software-RAID and SMART blocks INSIDE the Disk
// card (no Paper/header of its own — the card owns those). RAID and SMART are
// independent sources: each block renders ONLY when its data is present, and the
// whole thing returns null when BOTH are absent — so hosts without either (a dev
// mac, a plain VM) add nothing to the card, leaving it as before.
export function DiskHealthSections({ health }: { health: DiskHealth }) {
  const hasRaid = health.raid.length > 0
  const hasSmart = health.smart.length > 0
  if (!hasRaid && !hasSmart) return null

  return (
    <Box sx={{ mt: 2.5, pt: 2.5, borderTop: `1px solid ${colors.border}` }}>
      {hasRaid && (
        <Box sx={{ mb: hasSmart ? 2.5 : 0 }}>
          <Typography sx={{ fontSize: 11, color: colors.textFaint, mb: 1 }}>RAID</Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            {health.raid.map((r) => (
              <RaidRow key={r.name} array={r} />
            ))}
          </Box>
        </Box>
      )}

      {hasSmart && (
        <Box>
          <Typography sx={{ fontSize: 11, color: colors.textFaint, mb: 1 }}>SMART</Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
            {health.smart.map((d) => (
              <SmartRow key={d.device} disk={d} />
            ))}
          </Box>
        </Box>
      )}
    </Box>
  )
}

function RaidRow({ array }: { array: RaidArray }) {
  const color = severityColor(raidSeverity(array.state))
  return (
    <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1.5 }}>
      <Typography sx={{ fontSize: 14, fontWeight: 600, width: 52 }}>{array.name}</Typography>
      <Typography sx={{ fontSize: 12, color: colors.textDim, width: 52 }}>{array.level}</Typography>
      <Typography sx={{ fontSize: 12, color: colors.textFaint, width: 44 }}>
        [{array.active_devices}/{array.total_devices}]
      </Typography>
      <Typography sx={{ fontSize: 13, fontWeight: 600, color, flex: 1, minWidth: 0 }} noWrap>
        {array.state}
        {array.state === 'rebuilding' && ` ${array.resync_percent.toFixed(0)}%`}
        {array.state === 'rebuilding' && array.detail && (
          <Box component="span" sx={{ color: colors.textFaint, fontWeight: 400, ml: 1 }}>
            {array.detail}
          </Box>
        )}
      </Typography>
    </Box>
  )
}

function SmartRow({ disk }: { disk: SmartDisk }) {
  const color = severityColor(smartSeverity(disk.rollup))
  const healthFailed = disk.health.toUpperCase() === 'FAILED'
  return (
    <Box
      sx={{
        border: `1px solid ${colors.border}`,
        borderLeft: `2px solid ${color}`,
        borderRadius: 1.5,
        px: 1.5,
        py: 1.25,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
        <Typography sx={{ fontSize: 13, fontWeight: 600, flex: 1, minWidth: 0 }} noWrap>
          {disk.device}
        </Typography>
        {disk.stale && (
          <Typography sx={{ fontSize: 10, color: colors.warn, fontWeight: 600 }}>stale</Typography>
        )}
        <Box
          component="span"
          sx={{
            fontSize: 10,
            fontWeight: 700,
            px: 0.75,
            py: 0.25,
            borderRadius: 1,
            color: healthFailed ? colors.error : colors.ok,
            bgcolor: `${healthFailed ? colors.error : colors.ok}1f`,
          }}
        >
          {disk.health || '—'}
        </Box>
      </Box>
      <Typography sx={{ fontSize: 11, color: colors.textFaint, mb: 0.75 }} noWrap title={disk.model}>
        {disk.model || '—'}
      </Typography>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1.5, alignItems: 'baseline' }}>
        <Counter label="realloc" value={disk.reallocated} severity="warn" />
        <Counter label="pending" value={disk.pending} severity="error" />
        <Counter label="uncorr" value={disk.uncorrectable} severity="error" />
        <Typography sx={{ fontSize: 11, color: colors.textDim }}>{disk.temp_c}°C</Typography>
        <Typography sx={{ fontSize: 11, color: colors.textFaint }}>
          {formatHours(disk.power_on_hours)}
        </Typography>
      </Box>
    </Box>
  )
}

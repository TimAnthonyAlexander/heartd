import { Box, Paper, Typography } from '@mui/material'
import type { NetInterface } from '../api'
import { colors } from '../theme'
import { formatRate } from './NetworkPanel'

interface Props {
  interfaces: NetInterface[]
  dimmed?: boolean
}

// countColor renders an error/drop total in the error color when non-zero so a
// flaky link stands out, and faint otherwise so a healthy zero recedes.
function countColor(total: number): string {
  return total > 0 ? colors.error : colors.textFaint
}

export function NetInterfacesPanel({ interfaces, dimmed }: Props) {
  return (
    <Box sx={{ opacity: dimmed ? 0.45 : 1 }}>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 2, mb: 1.5 }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Network Interfaces
        </Typography>
        <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
          per-NIC throughput &amp; errors
        </Typography>
      </Box>

      <Paper elevation={0} sx={{ borderRadius: 2.5, overflow: 'hidden' }}>
        {interfaces.length === 0 ? (
          <Box sx={{ px: 2.5, py: 2.5 }}>
            <Typography sx={{ fontSize: 13, color: colors.textFaint }}>collecting data…</Typography>
          </Box>
        ) : (
          interfaces.map((n, i) => {
            const errs = n.recv_errs + n.sent_errs
            const drops = n.recv_drops + n.sent_drops
            return (
              <Box
                key={n.iface}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 2,
                  px: 2.5,
                  py: 1.5,
                  borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                }}
              >
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap>
                    {n.iface}
                  </Typography>
                </Box>
                <Typography sx={{ fontSize: 13, color: colors.textDim, width: 96, textAlign: 'right' }}>
                  ↓ {formatRate(n.recv_rate)}
                </Typography>
                <Typography sx={{ fontSize: 13, color: colors.textDim, width: 96, textAlign: 'right' }}>
                  ↑ {formatRate(n.sent_rate)}
                </Typography>
                <Typography
                  sx={{ fontSize: 13, fontWeight: errs > 0 ? 600 : 400, color: countColor(errs), width: 96, textAlign: 'right' }}
                  title="receive + send errors since boot"
                >
                  {errs} err
                </Typography>
                <Typography
                  sx={{ fontSize: 13, fontWeight: drops > 0 ? 600 : 400, color: countColor(drops), width: 96, textAlign: 'right' }}
                  title="receive + send drops since boot"
                >
                  {drops} drop
                </Typography>
              </Box>
            )
          })
        )}
      </Paper>
    </Box>
  )
}

import { Box, Typography } from '@mui/material'
import type { Breakpoint } from '@mui/material'
import type { ReactNode } from 'react'
import { colors } from '../theme'

interface Props {
  // A small section heading (omit for headingless rows whose children carry
  // their own labels, e.g. the Checks/Alerts split). Matches the panels' own
  // overline headers, set apart from them by a hairline rule.
  title?: string
  // The number of equal columns the sub-grid expands to on wide screens. Since
  // each section declares exactly as many columns as it has panels, rows always
  // fill evenly — no orphan card regardless of whether a panel is conditional.
  columns: number
  // The breakpoint at which the grid expands from one column to `columns`.
  // Below it, panels stack into a single column for narrow/mobile widths.
  breakpoint?: Breakpoint
  gap?: number
  align?: 'stretch' | 'start'
  children: ReactNode
}

// DashboardSection groups related panels into a labeled, responsive sub-grid.
// Composing the dashboard from these (rather than one big auto-fit grid) keeps
// rows balanced and the page scannable, Netdata-style.
export function DashboardSection({
  title,
  columns,
  breakpoint = 'md',
  gap = 2.5,
  align = 'stretch',
  children,
}: Props) {
  return (
    <Box component="section" sx={{ mb: 4 }}>
      {title && (
        <Box sx={{ mb: 2, pb: 1, borderBottom: `1px solid ${colors.border}` }}>
          <Typography variant="overline" sx={{ color: colors.textDim }}>
            {title}
          </Typography>
        </Box>
      )}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: {
            xs: '1fr',
            [breakpoint]: `repeat(${columns}, minmax(0, 1fr))`,
          },
          gap,
          alignItems: align,
        }}
      >
        {children}
      </Box>
    </Box>
  )
}

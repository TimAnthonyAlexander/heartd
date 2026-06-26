import { Box } from '@mui/material'
import { colors } from '../theme'

export interface TabItem<T extends string> {
  value: T
  label: string
}

interface Props<T extends string> {
  items: TabItem<T>[]
  value: T
  onChange: (value: T) => void
}

// SegmentedTabs is an Apple-style segmented control: a subtle inset track with
// the active item raised as a pill (lighter translucent surface, hairline
// border, soft shadow). Inactive items are dim text that brighten on hover.
export function SegmentedTabs<T extends string>({ items, value, onChange }: Props<T>) {
  return (
    <Box
      role="tablist"
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '2px',
        p: '3px',
        borderRadius: '10px',
        bgcolor: 'rgba(255,255,255,0.04)',
        border: `1px solid ${colors.border}`,
        maxWidth: '100%',
        overflowX: 'auto',
        // Hide the horizontal scrollbar that appears on narrow screens.
        scrollbarWidth: 'none',
        '&::-webkit-scrollbar': { display: 'none' },
      }}
    >
      {items.map((it) => {
        const active = it.value === value
        return (
          <Box
            key={it.value}
            component="button"
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(it.value)}
            sx={{
              appearance: 'none',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
              fontFamily: 'inherit',
              fontSize: 13,
              fontWeight: 600,
              letterSpacing: '-0.01em',
              px: 1.75,
              py: 0.625,
              borderRadius: '7px',
              transition: 'color 120ms ease, background-color 120ms ease, box-shadow 120ms ease',
              color: active ? colors.text : colors.textDim,
              bgcolor: active ? 'rgba(255,255,255,0.09)' : 'transparent',
              border: active ? '1px solid rgba(255,255,255,0.10)' : '1px solid transparent',
              boxShadow: active ? '0 1px 2px rgba(0,0,0,0.4)' : 'none',
              '&:hover': active ? {} : { color: colors.text, bgcolor: 'rgba(255,255,255,0.05)' },
            }}
          >
            {it.label}
          </Box>
        )
      })}
    </Box>
  )
}

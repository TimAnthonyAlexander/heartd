import { Box } from '@mui/material'
import { colors } from '../theme'

interface Props {
  checked: boolean
  onChange: (checked: boolean) => void
  disabled?: boolean
  'aria-label'?: string
}

// Toggle is a compact, theme-native on/off switch — a cleaner alternative to the
// Material Switch.
export function Toggle({ checked, onChange, disabled, ...rest }: Props) {
  return (
    <Box
      role="switch"
      aria-checked={checked}
      aria-label={rest['aria-label']}
      onClick={(e) => {
        e.stopPropagation()
        if (!disabled) onChange(!checked)
      }}
      sx={{
        width: 32,
        height: 19,
        flexShrink: 0,
        borderRadius: '10px',
        p: '2px',
        cursor: disabled ? 'default' : 'pointer',
        opacity: disabled ? 0.5 : 1,
        display: 'flex',
        justifyContent: checked ? 'flex-end' : 'flex-start',
        bgcolor: checked ? colors.accent : 'rgba(255,255,255,0.14)',
        transition: 'background-color 160ms ease',
      }}
    >
      <Box
        sx={{
          width: 15,
          height: 15,
          borderRadius: '50%',
          bgcolor: '#fff',
          boxShadow: '0 1px 2px rgba(0,0,0,0.4)',
        }}
      />
    </Box>
  )
}

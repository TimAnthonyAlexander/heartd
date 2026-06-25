import { Box, List, ListItemButton, ListItemText, Typography } from '@mui/material'
import type { Node } from '../api'

interface Props {
  nodes: Node[]
  selected: string | null
  onSelect: (name: string) => void
}

const statusColor: Record<Node['status'], string> = {
  ok: '#4caf50',
  failing: '#f44336',
  down: '#f44336',
  unknown: '#9e9e9e',
}

export function NodeSidebar({ nodes, selected, onSelect }: Props) {
  return (
    <Box sx={{ width: 240, borderRight: '1px solid #2a2a2a', height: '100vh', bgcolor: '#161616' }}>
      <Typography variant="h6" sx={{ p: 2, color: '#fff', fontWeight: 700 }}>
        heartd
      </Typography>
      <List dense>
        {nodes.map((node) => (
          <ListItemButton
            key={node.name}
            selected={node.name === selected}
            onClick={() => onSelect(node.name)}
          >
            <Box
              sx={{
                width: 10,
                height: 10,
                borderRadius: '50%',
                bgcolor: statusColor[node.status],
                mr: 1.5,
              }}
            />
            <ListItemText
              primary={node.name}
              secondary={node.local ? 'local' : node.status}
              slotProps={{
                primary: { color: '#eee' },
                secondary: { color: '#777' },
              }}
            />
          </ListItemButton>
        ))}
      </List>
    </Box>
  )
}

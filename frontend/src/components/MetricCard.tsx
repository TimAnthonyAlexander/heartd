import { Box, Paper, Typography } from '@mui/material'
import { Line, LineChart, ResponsiveContainer, YAxis } from 'recharts'

interface Props {
  title: string
  value: string
  percent: number
  history: number[]
}

export function MetricCard({ title, value, percent, history }: Props) {
  const data = history.map((v, i) => ({ i, v }))
  const color = percent > 90 ? '#f44336' : percent > 70 ? '#ff9800' : '#4caf50'

  return (
    <Paper sx={{ p: 2, bgcolor: '#1d1d1d', color: '#eee', flex: 1, minWidth: 220 }} elevation={0}>
      <Typography variant="overline" sx={{ color: '#888' }}>
        {title}
      </Typography>
      <Typography variant="h4" sx={{ fontWeight: 700, mb: 1 }}>
        {value}
      </Typography>
      <Box sx={{ height: 48 }}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={data}>
            <YAxis hide domain={[0, 100]} />
            <Line type="monotone" dataKey="v" stroke={color} strokeWidth={2} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </Box>
    </Paper>
  )
}

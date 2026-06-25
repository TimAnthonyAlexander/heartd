import { Line, LineChart, ResponsiveContainer, YAxis } from 'recharts'

interface Props {
  values: number[]
  color: string
  height?: number
}

// A minimal axis-less line for inline glances (sidebar node rows).
export function Sparkline({ values, color, height = 22 }: Props) {
  if (values.length < 2) return <div style={{ height }} />
  const data = values.map((v, i) => ({ i, v }))
  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={data} margin={{ top: 2, bottom: 2, left: 0, right: 0 }}>
        <YAxis hide domain={[0, 100]} />
        <Line
          type="monotone"
          dataKey="v"
          stroke={color}
          strokeWidth={1.5}
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}

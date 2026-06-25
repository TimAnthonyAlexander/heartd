import { useState } from 'react'
import { Box, InputAdornment, TextField } from '@mui/material'
import type { GeneralSettings } from '../../api'
import { updateGeneral } from '../../api'
import { percentColor } from '../../theme'
import { FeedbackText, SaveButton, Section, type Feedback } from './shared'

interface Props {
  nodeName: string
  initial: GeneralSettings
  onSaved: (g: GeneralSettings) => void
}

type ThresholdKey = 'cpu_threshold' | 'mem_threshold' | 'disk_threshold'

// ThresholdsSection edits this node's alert thresholds. It saves the full General
// object (carrying the sampling/retention fields through from `initial`) since the
// backend persists general settings as one unit.
export function ThresholdsSection({ nodeName, initial, onSaved }: Props) {
  const [form, setForm] = useState({
    cpu_threshold: String(initial.cpu_threshold),
    mem_threshold: String(initial.mem_threshold),
    disk_threshold: String(initial.disk_threshold),
  })
  const [feedback, setFeedback] = useState<Feedback>('idle')

  const save = async () => {
    const cpu = Number(form.cpu_threshold)
    const mem = Number(form.mem_threshold)
    const disk = Number(form.disk_threshold)
    for (const v of [cpu, mem, disk]) {
      if (!(v >= 0 && v <= 100)) {
        setFeedback({ error: 'Thresholds must be between 0 and 100.' })
        return
      }
    }

    const payload: GeneralSettings = {
      metrics_interval_sec: initial.metrics_interval_sec,
      peer_poll_interval_sec: initial.peer_poll_interval_sec,
      retention_sec: initial.retention_sec,
      cpu_threshold: cpu,
      mem_threshold: mem,
      disk_threshold: disk,
    }

    setFeedback('saving')
    try {
      const saved = await updateGeneral(nodeName, payload)
      setForm({
        cpu_threshold: String(saved.cpu_threshold),
        mem_threshold: String(saved.mem_threshold),
        disk_threshold: String(saved.disk_threshold),
      })
      onSaved(saved)
      setFeedback('saved')
    } catch (err) {
      setFeedback({ error: err instanceof Error ? err.message : 'Save failed' })
    }
  }

  const threshold = (key: ThresholdKey, label: string) => {
    const num = Number(form[key])
    const valid = num >= 0 && num <= 100
    return (
      <TextField
        label={label}
        type="number"
        size="small"
        value={form[key]}
        onChange={(e) => setForm((p) => ({ ...p, [key]: e.target.value }))}
        slotProps={{
          htmlInput: { min: 0, max: 100, step: 1 },
          input: {
            endAdornment: <InputAdornment position="end">%</InputAdornment>,
            sx: valid ? { color: percentColor(num) } : undefined,
          },
        }}
        fullWidth
      />
    )
  }

  return (
    <Section
      label="Alert thresholds"
      description="When CPU, memory, or disk usage crosses these levels, this node fires an alert through its notification channels. Set to 0 to disable a metric."
      actions={
        <>
          <SaveButton feedback={feedback} onClick={save} />
          <FeedbackText feedback={feedback} />
        </>
      }
    >
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr 1fr' }, gap: 2 }}>
        {threshold('cpu_threshold', 'CPU')}
        {threshold('mem_threshold', 'Memory')}
        {threshold('disk_threshold', 'Disk')}
      </Box>
    </Section>
  )
}

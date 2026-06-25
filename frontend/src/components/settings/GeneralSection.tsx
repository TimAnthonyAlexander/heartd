import { useState } from 'react'
import { Box, InputAdornment, TextField, Typography } from '@mui/material'
import type { GeneralSettings } from '../../api'
import { updateGeneral } from '../../api'
import { colors, percentColor } from '../../theme'
import { FeedbackText, SaveButton, Section, type Feedback } from './shared'

interface Props {
  initial: GeneralSettings
  onSaved: (g: GeneralSettings) => void
}

// Local form mirrors the server model except retention, which is edited in days.
interface FormState {
  metrics_interval_sec: string
  peer_poll_interval_sec: string
  retention_days: string
  cpu_threshold: string
  mem_threshold: string
  disk_threshold: string
}

function toForm(g: GeneralSettings): FormState {
  return {
    metrics_interval_sec: String(g.metrics_interval_sec),
    peer_poll_interval_sec: String(g.peer_poll_interval_sec),
    retention_days: String(g.retention_sec / 86400),
    cpu_threshold: String(g.cpu_threshold),
    mem_threshold: String(g.mem_threshold),
    disk_threshold: String(g.disk_threshold),
  }
}

export function GeneralSection({ initial, onSaved }: Props) {
  const [form, setForm] = useState<FormState>(toForm(initial))
  const [feedback, setFeedback] = useState<Feedback>('idle')

  const set = (key: keyof FormState) => (value: string) =>
    setForm((prev) => ({ ...prev, [key]: value }))

  const save = async () => {
    const metrics = Number(form.metrics_interval_sec)
    const peer = Number(form.peer_poll_interval_sec)
    const days = Number(form.retention_days)
    const cpu = Number(form.cpu_threshold)
    const mem = Number(form.mem_threshold)
    const disk = Number(form.disk_threshold)

    if (!(metrics > 0) || !(peer > 0)) {
      setFeedback({ error: 'Intervals must be greater than 0.' })
      return
    }
    if (!(days > 0)) {
      setFeedback({ error: 'Retention must be greater than 0 days.' })
      return
    }
    for (const v of [cpu, mem, disk]) {
      if (!(v >= 0 && v <= 100)) {
        setFeedback({ error: 'Thresholds must be between 0 and 100.' })
        return
      }
    }

    const payload: GeneralSettings = {
      metrics_interval_sec: Math.round(metrics),
      peer_poll_interval_sec: Math.round(peer),
      retention_sec: Math.round(days * 86400),
      cpu_threshold: cpu,
      mem_threshold: mem,
      disk_threshold: disk,
    }

    setFeedback('saving')
    try {
      const saved = await updateGeneral(payload)
      setForm(toForm(saved))
      onSaved(saved)
      setFeedback('saved')
    } catch (err) {
      setFeedback({ error: err instanceof Error ? err.message : 'Save failed' })
    }
  }

  const threshold = (key: 'cpu_threshold' | 'mem_threshold' | 'disk_threshold', label: string) => {
    const num = Number(form[key])
    const valid = num >= 0 && num <= 100
    return (
      <TextField
        label={label}
        type="number"
        size="small"
        value={form[key]}
        onChange={(e) => set(key)(e.target.value)}
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
      label="General"
      description="Sampling cadence, data retention, and alert thresholds."
      actions={
        <>
          <SaveButton feedback={feedback} onClick={save} />
          <FeedbackText feedback={feedback} />
        </>
      }
    >
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 2 }}>
        <TextField
          label="Metrics sample interval"
          type="number"
          size="small"
          value={form.metrics_interval_sec}
          onChange={(e) => set('metrics_interval_sec')(e.target.value)}
          slotProps={{
            htmlInput: { min: 1, step: 1 },
            input: { endAdornment: <InputAdornment position="end">seconds</InputAdornment> },
          }}
          fullWidth
        />
        <TextField
          label="Peer poll interval"
          type="number"
          size="small"
          value={form.peer_poll_interval_sec}
          onChange={(e) => set('peer_poll_interval_sec')(e.target.value)}
          slotProps={{
            htmlInput: { min: 1, step: 1 },
            input: { endAdornment: <InputAdornment position="end">seconds</InputAdornment> },
          }}
          fullWidth
        />
        <TextField
          label="Retention"
          type="number"
          size="small"
          value={form.retention_days}
          onChange={(e) => set('retention_days')(e.target.value)}
          slotProps={{
            htmlInput: { min: 1, step: 1 },
            input: { endAdornment: <InputAdornment position="end">days</InputAdornment> },
          }}
          fullWidth
        />
      </Box>

      <Typography variant="overline" sx={{ color: colors.textFaint, display: 'block', mt: 3, mb: 1 }}>
        Alert thresholds
      </Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr 1fr' }, gap: 2 }}>
        {threshold('cpu_threshold', 'CPU')}
        {threshold('mem_threshold', 'Memory')}
        {threshold('disk_threshold', 'Disk')}
      </Box>
    </Section>
  )
}

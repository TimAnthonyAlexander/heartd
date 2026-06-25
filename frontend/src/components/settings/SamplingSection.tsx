import { useState } from 'react'
import { Box, InputAdornment, TextField } from '@mui/material'
import type { GeneralSettings } from '../../api'
import { updateGeneral } from '../../api'
import { FeedbackText, SaveButton, Section, type Feedback } from './shared'

interface Props {
  nodeName: string
  initial: GeneralSettings
  onSaved: (g: GeneralSettings) => void
}

// SamplingSection edits this node's sampling cadence and data retention.
// Retention is edited in days but stored as seconds. It saves the full General
// object (carrying the thresholds through from `initial`).
export function SamplingSection({ nodeName, initial, onSaved }: Props) {
  const [form, setForm] = useState({
    metrics_interval_sec: String(initial.metrics_interval_sec),
    peer_poll_interval_sec: String(initial.peer_poll_interval_sec),
    retention_days: String(initial.retention_sec / 86400),
  })
  const [feedback, setFeedback] = useState<Feedback>('idle')

  const set = (key: keyof typeof form) => (value: string) =>
    setForm((prev) => ({ ...prev, [key]: value }))

  const save = async () => {
    const metrics = Number(form.metrics_interval_sec)
    const peer = Number(form.peer_poll_interval_sec)
    const days = Number(form.retention_days)

    if (!(metrics > 0) || !(peer > 0)) {
      setFeedback({ error: 'Intervals must be greater than 0.' })
      return
    }
    if (!(days > 0)) {
      setFeedback({ error: 'Retention must be greater than 0 days.' })
      return
    }

    const payload: GeneralSettings = {
      metrics_interval_sec: Math.round(metrics),
      peer_poll_interval_sec: Math.round(peer),
      retention_sec: Math.round(days * 86400),
      cpu_threshold: initial.cpu_threshold,
      mem_threshold: initial.mem_threshold,
      disk_threshold: initial.disk_threshold,
    }

    setFeedback('saving')
    try {
      const saved = await updateGeneral(nodeName, payload)
      setForm({
        metrics_interval_sec: String(saved.metrics_interval_sec),
        peer_poll_interval_sec: String(saved.peer_poll_interval_sec),
        retention_days: String(saved.retention_sec / 86400),
      })
      onSaved(saved)
      setFeedback('saved')
    } catch (err) {
      setFeedback({ error: err instanceof Error ? err.message : 'Save failed' })
    }
  }

  return (
    <Section
      label="Sampling & retention"
      description="How often this node samples its metrics and polls its peers, and how long it keeps historical data."
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
    </Section>
  )
}

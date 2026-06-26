import { useState } from 'react'
import { Box, Button, FormControlLabel, Stack, Switch, TextField, Typography } from '@mui/material'
import type { EmailNotify, NotifySettings, WebhookNotify } from '../../api'
import { fetchNodes, testNotify, updateNotify } from '../../api'
import { colors } from '../../theme'
import { FeedbackText, SaveButton, Section, type Feedback } from './shared'

// Result of fanning the notify config out to every node.
interface BulkResult {
  total: number
  failed: { name: string; error: string }[]
}

interface Props {
  nodeName: string
  initial: NotifySettings
  onSaved: (n: NotifySettings) => void
}

// Email form keeps `to` as a comma-separated string for editing.
interface EmailForm extends Omit<EmailNotify, 'to'> {
  to: string
}

function toEmailForm(e: EmailNotify): EmailForm {
  return { ...e, to: (e.to ?? []).join(', ') }
}

function fromEmailForm(f: EmailForm): EmailNotify {
  return {
    ...f,
    smtp_port: Math.round(Number(f.smtp_port)) || 0,
    to: f.to
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0),
  }
}

export function NotifySection({ nodeName, initial, onSaved }: Props) {
  const [webhook, setWebhook] = useState<WebhookNotify>(initial.webhook)
  const [email, setEmail] = useState<EmailForm>(toEmailForm(initial.email))
  const [feedback, setFeedback] = useState<Feedback>('idle')
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<Record<string, string> | null>(null)
  const [testError, setTestError] = useState<string | null>(null)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkResult, setBulkResult] = useState<BulkResult | null>(null)

  const current = (): NotifySettings => ({
    webhook: { ...webhook, url: webhook.url.trim() },
    email: fromEmailForm(email),
  })

  const setEmailField =
    <K extends keyof EmailForm>(key: K) =>
    (value: EmailForm[K]) =>
      setEmail((prev) => ({ ...prev, [key]: value }))

  const save = async () => {
    setFeedback('saving')
    try {
      const saved = await updateNotify(nodeName, current())
      setWebhook(saved.webhook)
      setEmail(toEmailForm(saved.email))
      onSaved(saved)
      setFeedback('saved')
    } catch (err) {
      setFeedback({ error: err instanceof Error ? err.message : 'Save failed' })
    }
  }

  const test = async () => {
    setTesting(true)
    setTestResult(null)
    setTestError(null)
    try {
      const result = await testNotify(nodeName, current())
      setTestResult(result)
    } catch (err) {
      setTestError(err instanceof Error ? err.message : 'Test failed')
    } finally {
      setTesting(false)
    }
  }

  // saveAll pushes the current notify config (webhook + email) to EVERY node —
  // the local node and each peer, via the same per-node endpoint that proxies to
  // peers. It reports per-node results since an unreachable/old peer can fail
  // while others succeed.
  const saveAll = async () => {
    setBulkResult(null)
    let nodes
    try {
      nodes = await fetchNodes()
    } catch (err) {
      setBulkResult({ total: 0, failed: [{ name: '(cluster)', error: err instanceof Error ? err.message : 'could not list nodes' }] })
      return
    }
    if (nodes.length === 0) return
    if (
      !window.confirm(
        `Apply these notification settings (webhook + email) to all ${nodes.length} node(s)? ` +
          `This overwrites each node's existing notification config.`,
      )
    )
      return

    setBulkBusy(true)
    const payload = current()
    const results: { name: string; ok: boolean; error?: string }[] = await Promise.all(
      nodes.map(async (n) => {
        try {
          await updateNotify(n.name, payload)
          return { name: n.name, ok: true }
        } catch (err) {
          return { name: n.name, ok: false, error: err instanceof Error ? err.message : 'failed' }
        }
      }),
    )
    setBulkBusy(false)
    setBulkResult({
      total: results.length,
      failed: results.filter((r) => !r.ok).map((r) => ({ name: r.name, error: r.error ?? 'failed' })),
    })
    // Keep the parent's cached copy for this node in sync with what we just sent.
    onSaved(payload)
  }

  return (
    <Section
      label="Notifications"
      description="Where heartd sends alerts when a check fails or a threshold is crossed."
      actions={
        <>
          <SaveButton feedback={feedback} onClick={save} />
          <Button variant="outlined" size="small" onClick={test} disabled={testing}>
            {testing ? 'Testing…' : 'Send test'}
          </Button>
          <Button variant="outlined" size="small" onClick={saveAll} disabled={bulkBusy}>
            {bulkBusy ? 'Saving all…' : 'Save for all nodes'}
          </Button>
          <FeedbackText feedback={feedback} />
        </>
      }
    >
      <Typography variant="overline" sx={{ color: colors.textFaint, display: 'block', mb: 1 }}>
        Webhook
      </Typography>
      <Stack spacing={2}>
        <FormControlLabel
          control={
            <Switch
              checked={webhook.enabled}
              onChange={(e) => setWebhook((prev) => ({ ...prev, enabled: e.target.checked }))}
            />
          }
          label="Enable webhook"
        />
        <TextField
          label="Webhook URL"
          size="small"
          value={webhook.url}
          onChange={(e) => setWebhook((prev) => ({ ...prev, url: e.target.value }))}
          disabled={!webhook.enabled}
          fullWidth
          placeholder="https://example.com/hooks/heartd"
        />
      </Stack>

      <Typography variant="overline" sx={{ color: colors.textFaint, display: 'block', mt: 3, mb: 1 }}>
        Email
      </Typography>
      <Stack spacing={2}>
        <FormControlLabel
          control={
            <Switch
              checked={email.enabled}
              onChange={(e) => setEmailField('enabled')(e.target.checked)}
            />
          }
          label="Enable email"
        />
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '2fr 1fr' }, gap: 2 }}>
          <TextField
            label="SMTP host"
            size="small"
            value={email.smtp_host}
            onChange={(e) => setEmailField('smtp_host')(e.target.value)}
            disabled={!email.enabled}
            fullWidth
          />
          <TextField
            label="SMTP port"
            type="number"
            size="small"
            // Show blank (not a misleading 0) when unset; most providers use 587.
            value={email.smtp_port === 0 ? '' : email.smtp_port}
            onChange={(e) => setEmailField('smtp_port')(Number(e.target.value))}
            disabled={!email.enabled}
            placeholder="587"
            slotProps={{ htmlInput: { min: 1, max: 65535, step: 1 } }}
            fullWidth
          />
        </Box>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 2 }}>
          <TextField
            label="Username"
            size="small"
            value={email.username}
            onChange={(e) => setEmailField('username')(e.target.value)}
            disabled={!email.enabled}
            autoComplete="off"
            fullWidth
          />
          <TextField
            label="Password"
            type="password"
            size="small"
            value={email.password}
            onChange={(e) => setEmailField('password')(e.target.value)}
            disabled={!email.enabled}
            autoComplete="new-password"
            fullWidth
          />
        </Box>
        <TextField
          label="From address"
          size="small"
          value={email.from}
          onChange={(e) => setEmailField('from')(e.target.value)}
          disabled={!email.enabled}
          fullWidth
        />
        <TextField
          label="To (comma-separated)"
          size="small"
          value={email.to}
          onChange={(e) => setEmailField('to')(e.target.value)}
          disabled={!email.enabled}
          fullWidth
          placeholder="ops@example.com, oncall@example.com"
        />
        <TextField
          label="Subject prefix"
          size="small"
          value={email.subject_prefix}
          onChange={(e) => setEmailField('subject_prefix')(e.target.value)}
          disabled={!email.enabled}
          fullWidth
          placeholder="[heartd]"
        />
      </Stack>

      {bulkResult && (
        <Box
          sx={{ mt: 2, p: 1.5, borderRadius: 1.5, border: `1px solid ${colors.border}`, bgcolor: colors.bg }}
        >
          <Typography variant="overline" sx={{ color: colors.textFaint }}>
            Save for all nodes
          </Typography>
          {bulkResult.failed.length === 0 ? (
            <Typography sx={{ fontSize: 13, mt: 0.5, color: colors.ok }}>
              Saved to all {bulkResult.total} node{bulkResult.total === 1 ? '' : 's'}.
            </Typography>
          ) : (
            <>
              <Typography sx={{ fontSize: 13, mt: 0.5, color: colors.text }}>
                Saved to {bulkResult.total - bulkResult.failed.length} of {bulkResult.total} nodes. Failed:
              </Typography>
              {bulkResult.failed.map((f) => (
                <Typography key={f.name} sx={{ fontSize: 13, mt: 0.25, color: colors.error }}>
                  {f.name}: {f.error}
                </Typography>
              ))}
            </>
          )}
        </Box>
      )}

      {(testResult || testError) && (
        <Box
          sx={{
            mt: 2,
            p: 1.5,
            borderRadius: 1.5,
            border: `1px solid ${colors.border}`,
            bgcolor: colors.bg,
          }}
        >
          <Typography variant="overline" sx={{ color: colors.textFaint }}>
            Test result
          </Typography>
          {testError && (
            <Typography sx={{ fontSize: 13, color: colors.error, mt: 0.5 }}>{testError}</Typography>
          )}
          {testResult &&
            Object.entries(testResult).map(([channel, result]) => {
              const ok = result.toLowerCase() === 'ok'
              return (
                <Typography
                  key={channel}
                  sx={{ fontSize: 13, mt: 0.5, color: ok ? colors.ok : colors.error }}
                >
                  {channel}: {result}
                </Typography>
              )
            })}
        </Box>
      )}
    </Section>
  )
}

import { useState } from 'react'
import { Box, Button, FormControlLabel, Stack, Switch, TextField, Typography } from '@mui/material'
import type { EmailNotify, NotifySettings, WebhookNotify } from '../../api'
import { testNotify, updateNotify } from '../../api'
import { colors } from '../../theme'
import { FeedbackText, SaveButton, Section, type Feedback } from './shared'

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
            value={email.smtp_port}
            onChange={(e) => setEmailField('smtp_port')(Number(e.target.value))}
            disabled={!email.enabled}
            slotProps={{ htmlInput: { min: 0, step: 1 } }}
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

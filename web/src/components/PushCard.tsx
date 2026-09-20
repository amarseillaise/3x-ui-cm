import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { t } from '../i18n/ru'
import { disablePush, enablePush, pushState, type PushState } from '../push'
import { isIOS, isStandalone } from '../platform'
import Button from './Button'
import Card from './Card'

interface Props {
  enabled: boolean
  vapidPublicKey: string
}

/** Notification toggle for the current device. Renders nothing when the server has no VAPID keys. */
export default function PushCard({ enabled, vapidPublicKey }: Props) {
  const qc = useQueryClient()
  const [state, setState] = useState<PushState | 'loading'>('loading')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void pushState().then((s) => {
      if (!cancelled) setState(s)
    })
    return () => {
      cancelled = true
    }
  }, [])

  if (!enabled) return null

  const run = async (fn: () => Promise<PushState>) => {
    setBusy(true)
    setError(null)
    try {
      setState(await fn())
      await qc.invalidateQueries({ queryKey: ['me'] })
    } catch {
      setError(t.push.error)
    } finally {
      setBusy(false)
    }
  }

  let body: string
  let action: { label: string; onClick: () => void } | null = null
  switch (state) {
    case 'loading':
      body = t.loading
      break
    case 'unsupported':
      body = isIOS() && !isStandalone() ? t.push.iosInstallFirst : t.push.unsupported
      break
    case 'denied':
      body = t.push.denied
      break
    case 'on':
      body = t.push.on
      action = { label: t.push.disable, onClick: () => void run(disablePush) }
      break
    default:
      body = t.push.off
      action = { label: t.push.enable, onClick: () => void run(() => enablePush(vapidPublicKey)) }
  }

  return (
    <Card>
      <h2 className="mb-1 font-semibold">{t.push.title}</h2>
      <p className="text-sm text-slate-400">{body}</p>
      {error && <p className="mt-2 text-sm text-rose-300">{error}</p>}
      {action && (
        <Button variant={state === 'on' ? 'secondary' : 'primary'} className="mt-3 w-full" onClick={action.onClick} disabled={busy}>
          {action.label}
        </Button>
      )}
    </Card>
  )
}

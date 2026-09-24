import { useState } from 'react'
import { Link } from 'react-router'
import { QRCodeSVG } from 'qrcode.react'
import { isApiError } from '../api'
import { errorText } from '../errors'
import { t } from '../i18n/ru'
import { useLogout, useMe } from '../hooks/useMe'
import { daysWord, formatBytes, formatDate, relativeTime } from '../format'
import { copyText, isIOS, isStandalone } from '../platform'
import type { SubscriptionInfo } from '../types'
import Button from '../components/Button'
import Card from '../components/Card'
import Centered from '../components/Centered'
import StatusBadge from '../components/StatusBadge'
import PushCard from '../components/PushCard'
import InvalidLinkPage from './InvalidLinkPage'
import NoSessionPage from './NoSessionPage'

export default function SubscriptionPage() {
  const me = useMe()
  const logout = useLogout()

  if (me.isPending) return <Centered>{t.loading}</Centered>
  if (isApiError(me.error, 401)) return <NoSessionPage />
  if (isApiError(me.error, 404)) return <InvalidLinkPage />
  if (me.error || !me.data) {
    return (
      <Centered>
        <div className="flex flex-col items-center gap-3">
          <span>{errorText(me.error)}</span>
          <Button variant="secondary" onClick={() => void me.refetch()}>
            {t.retry}
          </Button>
        </div>
      </Centered>
    )
  }

  const { subscription: sub, renewal, push } = me.data
  const showIOSHint = isIOS() && !isStandalone()

  return (
    <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-12">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t.subscription.title}</h1>
        <Button variant="ghost" className="min-h-10 px-3 py-2 text-sm" onClick={() => logout.mutate()} disabled={logout.isPending}>
          {t.logout}
        </Button>
      </header>

      <StatusCard sub={sub} />

      <Card>
        <div className="flex flex-col gap-2 text-sm">
          <TrafficRow sub={sub} />
          <OnlineRow sub={sub} />
          {sub.inboundCount > 0 && (
            <div className="flex justify-between text-slate-400">
              <span>{t.subscription.servers}</span>
              <span>{sub.inboundCount}</span>
            </div>
          )}
        </div>
      </Card>

      {renewal.enabled && !sub.unlimited && (
        <Card>
          {renewal.hasPending ? (
            <p className="text-center text-sm text-amber-300">{t.renew.pending}</p>
          ) : (
            <Link to="/renew" className="block">
              <Button className="w-full">{t.renew.button}</Button>
            </Link>
          )}
        </Card>
      )}

      <PushCard enabled={push.enabled} vapidPublicKey={push.vapidPublicKey} />

      <LinkCard url={sub.subscriptionUrl} />

      {showIOSHint && (
        <Card className="border-blue-900/60 bg-blue-950/40">
          <h2 className="mb-1 font-semibold">{t.ios.title}</h2>
          <p className="text-sm text-slate-300">{t.ios.body}</p>
        </Card>
      )}
    </main>
  )
}

function StatusCard({ sub }: { sub: SubscriptionInfo }) {
  let headline: string
  let sub2: string | null = null
  switch (sub.status) {
    case 'unlimited':
      headline = t.subscription.unlimitedHeadline
      break
    case 'expired':
      headline = `${t.subscription.expiredOn} ${sub.expiresAt ? formatDate(sub.expiresAt) : ''}`
      break
    case 'depleted':
      headline = t.subscription.depletedHeadline
      break
    case 'disabled':
      headline = t.subscription.disabledHeadline
      break
    default:
      headline = sub.expiresAt ? `${t.subscription.validUntil} ${formatDate(sub.expiresAt)}` : t.subscription.unlimitedHeadline
      if (sub.daysLeft >= 0) sub2 = `${t.subscription.daysLeft} ${daysWord(sub.daysLeft)}`
  }
  const tone = sub.status === 'expiring' ? 'text-amber-200' : sub.status === 'expired' || sub.status === 'depleted' ? 'text-rose-200' : 'text-slate-100'
  return (
    <Card>
      <div className="mb-3">
        <StatusBadge status={sub.status} />
      </div>
      <p className={`text-lg font-semibold ${tone}`}>{headline}</p>
      {sub2 && <p className="mt-1 text-sm text-slate-400">{sub2}</p>}
    </Card>
  )
}

function TrafficRow({ sub }: { sub: SubscriptionInfo }) {
  if (sub.quotaBytes <= 0) {
    return (
      <div className="flex justify-between text-slate-400">
        <span>{t.subscription.traffic}</span>
        <span>
          {t.subscription.trafficUnlimited} · {t.subscription.used} {formatBytes(sub.usedBytes)}
        </span>
      </div>
    )
  }
  const pct = Math.min(100, sub.trafficPercent)
  const bar = pct >= 90 ? 'bg-rose-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div>
      <div className="flex justify-between text-slate-400">
        <span>{t.subscription.traffic}</span>
        <span>
          {formatBytes(sub.usedBytes)} / {formatBytes(sub.quotaBytes)}
        </span>
      </div>
      <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-slate-800">
        <div className={`h-full ${bar}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

function OnlineRow({ sub }: { sub: SubscriptionInfo }) {
  return (
    <div className="flex items-center justify-between text-slate-400">
      <span className="flex items-center gap-2">
        <span className={`inline-block h-2 w-2 rounded-full ${sub.online ? 'bg-emerald-400' : 'bg-slate-600'}`} />
        {sub.online ? t.subscription.online : t.subscription.offline}
      </span>
      {!sub.online && (
        <span>{sub.lastOnline ? `${t.subscription.lastSeen} ${relativeTime(sub.lastOnline)}` : t.subscription.neverSeen}</span>
      )}
    </div>
  )
}

function LinkCard({ url }: { url: string }) {
  const [copied, setCopied] = useState(false)
  if (!url) {
    return (
      <Card>
        <h2 className="mb-1 font-semibold">{t.link.title}</h2>
        <p className="text-sm text-slate-400">{t.link.unavailable}</p>
      </Card>
    )
  }
  const onCopy = async () => {
    if (await copyText(url)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }
  return (
    <Card>
      <h2 className="mb-1 font-semibold">{t.link.title}</h2>
      <p className="mb-3 text-sm text-slate-400">{t.link.hint}</p>
      <div className="mx-auto mb-3 w-fit rounded-xl bg-white p-3">
        <QRCodeSVG value={url} size={192} bgColor="#ffffff" fgColor="#0f172a" level="M" />
      </div>
      <p className="mb-3 break-all rounded-lg bg-slate-950 px-3 py-2 font-mono text-xs text-slate-300">{url}</p>
      <div className="grid grid-cols-2 gap-2">
        <Button variant="secondary" onClick={() => void onCopy()}>
          {copied ? t.link.copied : t.link.copy}
        </Button>
        <a href={url} className="block" target="_blank" rel="noreferrer">
          <Button variant="secondary" className="w-full">
            {t.link.open}
          </Button>
        </a>
      </div>
    </Card>
  )
}

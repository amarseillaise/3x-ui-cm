import { useState, type FormEvent } from 'react'
import { isApiError } from '../api'
import { errorText } from '../errors'
import { t } from '../i18n/ru'
import { ADMIN_PATH } from '../routes'
import { formatDate, formatDateTime, formatMoney, relativeTime } from '../format'
import { useAdminLogin, useAdminRenewals, useAdminStats, useResolveRenewal } from '../hooks/useAdmin'
import { useLogout } from '../hooks/useMe'
import type { RenewalRequest } from '../types'
import Button from '../components/Button'
import Card from '../components/Card'
import Centered from '../components/Centered'
import Message from '../components/Message'
import BroadcastForm from '../components/BroadcastForm'
import ConfirmDialog from '../components/ConfirmDialog'
import PushCard from '../components/PushCard'
import RenewalStatusChip from '../components/RenewalStatusChip'

type Tab = 'pending' | 'history' | 'broadcast'

export default function AdminPage() {
  const stats = useAdminStats()
  const logout = useLogout(ADMIN_PATH)
  const [tab, setTab] = useState<Tab>('pending')

  if (stats.isPending) return <Centered>{t.loading}</Centered>
  if (isApiError(stats.error, 401)) return <AdminLogin />
  if (isApiError(stats.error, 403)) {
    return (
      <div className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-4 p-6 text-center">
        <h1 className="text-xl font-semibold">{t.admin.forbiddenTitle}</h1>
        <p className="text-slate-400">{t.admin.forbiddenBody}</p>
        <Button variant="secondary" onClick={() => logout.mutate()} disabled={logout.isPending}>
          {t.logout}
        </Button>
      </div>
    )
  }
  if (stats.error || !stats.data) return <Message title={t.admin.title} body={errorText(stats.error)} />

  const pendingCount = stats.data.renewals.pending ?? 0
  return (
    <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-12">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t.admin.title}</h1>
        <Button variant="ghost" className="min-h-10 px-3 py-2 text-sm" onClick={() => logout.mutate()}>
          {t.logout}
        </Button>
      </header>

      <nav className="grid grid-cols-3 gap-1 rounded-xl bg-slate-900 p-1 text-sm">
        {(['pending', 'history', 'broadcast'] as Tab[]).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            className={`min-h-10 rounded-lg px-2 ${tab === k ? 'bg-slate-700 text-white' : 'text-slate-400'}`}
          >
            {t.admin.tabs[k]}
            {k === 'pending' && pendingCount > 0 && <span className="ml-1 rounded-full bg-amber-500/30 px-1.5 text-xs text-amber-200">{pendingCount}</span>}
          </button>
        ))}
      </nav>

      {tab === 'pending' && <RenewalList status="pending" />}
      {tab === 'history' && <RenewalList status="" />}
      {tab === 'broadcast' && <BroadcastForm pushEnabled={stats.data.pushEnabled} />}

      <PushCard enabled={stats.data.pushEnabled} vapidPublicKey={stats.data.vapidPublicKey} />

      <Card>
        <h2 className="mb-2 font-semibold">{t.admin.stats}</h2>
        <dl className="grid grid-cols-2 gap-1 text-sm text-slate-300">
          <dt className="text-slate-500">{t.admin.statSessions}</dt>
          <dd>{stats.data.sessions}</dd>
          <dt className="text-slate-500">{t.admin.statPush}</dt>
          <dd>
            {stats.data.push.client ?? 0} / {stats.data.push.admin ?? 0}
          </dd>
          <dt className="text-slate-500">{t.admin.statNotifications}</dt>
          <dd>{stats.data.notifications}</dd>
        </dl>
      </Card>
    </main>
  )
}

function RenewalList({ status }: { status: string }) {
  const list = useAdminRenewals(status)
  const resolve = useResolveRenewal()
  const [asking, setAsking] = useState<RenewalRequest | null>(null)
  if (list.isPending) return <Centered>{t.loading}</Centered>
  if (list.error || !list.data) return <Card className="text-sm text-rose-200">{errorText(list.error)}</Card>
  if (list.data.requests.length === 0) return <Card className="text-sm text-slate-400">{status === 'pending' ? t.admin.noPending : t.admin.noHistory}</Card>

  return (
    <div className="flex flex-col gap-3">
      {resolve.error && <Card className="text-sm text-rose-200">{errorText(resolve.error)}</Card>}
      {asking && (
        <ConfirmDialog
          title={t.admin.rejectTitle}
          body={`${t.admin.rejectConfirm} ${asking.email}, ${asking.planTitle}, −${asking.appliedDays} ${t.admin.days}`}
          confirmLabel={resolve.isPending ? t.admin.rejecting : t.admin.reject}
          busy={resolve.isPending}
          onCancel={() => setAsking(null)}
          onConfirm={() => resolve.mutate({ id: asking.id, action: 'reject' }, { onSettled: () => setAsking(null) })}
        />
      )}
      {list.data.requests.map((r) => (
        <Card key={r.id}>
          <div className="mb-1 flex items-start justify-between gap-2">
            <div className="min-w-0">
              <div className="truncate font-medium">{r.email}</div>
              <div className="text-sm text-slate-300">
                {r.planTitle} · {formatMoney(r.amount, r.currency)}
              </div>
            </div>
            <RenewalStatusChip status={r.status} />
          </div>
          <div className="text-xs text-slate-500">
            {formatDateTime(r.createdAt)} ({relativeTime(r.createdAt)})
            {r.expiryBefore && r.expiryAfter && (
              <span className="block">
                {formatDate(r.expiryBefore)} → {formatDate(r.expiryAfter)} (+{r.appliedDays} {t.admin.days})
              </span>
            )}
            {r.error && <span className="block text-rose-300">{r.error}</span>}
          </div>
          {r.status === 'pending' && (
            <div className="mt-3 grid grid-cols-2 gap-2">
              <Button onClick={() => resolve.mutate({ id: r.id, action: 'confirm' })} disabled={resolve.isPending}>
                {t.admin.confirm}
              </Button>
              <Button variant="secondary" onClick={() => setAsking(r)} disabled={resolve.isPending}>
                {t.admin.reject}
              </Button>
            </div>
          )}
        </Card>
      ))}
    </div>
  )
}

function AdminLogin() {
  const login = useAdminLogin()
  const [password, setPassword] = useState('')
  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (password) login.mutate(password)
  }
  return (
    <main className="mx-auto flex min-h-dvh max-w-sm flex-col justify-center gap-4 p-6">
      <h1 className="text-xl font-semibold">{t.admin.loginTitle}</h1>
      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        {/* A fixed user name lets the iPhone keychain save and fill the password for this site. */}
        <input type="text" name="username" autoComplete="username" value="admin" readOnly hidden />
        <label className="flex flex-col gap-1 text-sm text-slate-300">
          {t.admin.password}
          <input
            type="password"
            name="password"
            autoComplete="current-password"
            autoFocus
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="min-h-12 rounded-xl border border-slate-700 bg-slate-900 px-4 text-base text-slate-100 outline-none focus:border-blue-500"
          />
        </label>
        {login.error && <p className="text-sm text-rose-300">{errorText(login.error)}</p>}
        <Button type="submit" disabled={login.isPending || !password}>
          {login.isPending ? t.admin.loggingIn : t.admin.login}
        </Button>
      </form>
    </main>
  )
}

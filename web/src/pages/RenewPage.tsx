import { useState } from 'react'
import { Link } from 'react-router'
import { isApiError } from '../api'
import { t } from '../i18n/ru'
import { formatDate, formatDateTime, formatMoney } from '../format'
import { copyText } from '../platform'
import { useMyRenewals, usePlans, useRenew } from '../hooks/useRenewal'
import type { Plan, PlansResponse, RenewalRequest } from '../types'
import Button from '../components/Button'
import Card from '../components/Card'
import Centered from '../components/Centered'
import Message from '../components/Message'
import RenewalStatusChip from '../components/RenewalStatusChip'
import NoSessionPage from './NoSessionPage'

export default function RenewPage() {
  const plans = usePlans()
  const history = useMyRenewals()
  const renew = useRenew()
  const [plan, setPlan] = useState<Plan | null>(null)

  if (plans.isPending) return <Centered>{t.loading}</Centered>
  if (isApiError(plans.error, 401)) return <NoSessionPage />
  if (plans.error || !plans.data) return <Message title={t.renew.title} body={t.errorGeneric} />
  if (!plans.data.enabled) return <Message title={t.renew.title} body={t.renew.disabled} />

  if (renew.isSuccess) {
    const until = renew.data.expiresAt ? formatDate(renew.data.expiresAt) : null
    return (
      <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-12">
        <Card className="border-emerald-900/60 bg-emerald-950/30">
          <h1 className="mb-2 text-xl font-semibold">{t.renew.doneTitle}</h1>
          <p className="text-slate-200">{until ? `${t.renew.doneUntil} ${until}.` : ''}</p>
          <p className="mt-2 text-sm text-slate-400">{t.renew.doneNote}</p>
        </Card>
        <Link to="/">
          <Button className="w-full">{t.renew.backToSubscription}</Button>
        </Link>
      </main>
    )
  }

  return (
    <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-12">
      <header className="flex items-center gap-3">
        <Link to="/" className="text-slate-400" aria-label={t.back}>
          ←
        </Link>
        <h1 className="text-xl font-semibold">{t.renew.title}</h1>
      </header>

      {plans.data.hasPending && <Card className="border-amber-900/60 bg-amber-950/30 text-sm text-amber-200">{t.renew.pending}</Card>}

      {plan && !plans.data.hasPending ? (
        <PayStep plan={plan} data={plans.data} busy={renew.isPending} error={renew.error} onBack={() => setPlan(null)} onPaid={() => renew.mutate(plan.id)} />
      ) : (
        <ChooseStep data={plans.data} onChoose={setPlan} disabled={plans.data.hasPending} />
      )}

      {history.data && history.data.requests.length > 0 && <History requests={history.data.requests} />}
    </main>
  )
}

function ChooseStep({ data, onChoose, disabled }: { data: PlansResponse; onChoose: (p: Plan) => void; disabled: boolean }) {
  return (
    <Card>
      <h2 className="mb-3 font-semibold">{t.renew.choosePlan}</h2>
      <div className="flex flex-col gap-2">
        {data.plans.map((p) => (
          <button
            key={p.id}
            type="button"
            disabled={disabled}
            onClick={() => onChoose(p)}
            className="flex min-h-14 items-center justify-between rounded-xl border border-slate-700 bg-slate-800/60 px-4 py-3 text-left transition hover:border-blue-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className="font-medium">{p.title}</span>
            <span className="text-lg font-semibold text-blue-300">{formatMoney(p.price, data.currency)}</span>
          </button>
        ))}
      </div>
    </Card>
  )
}

function PayStep({
  plan,
  data,
  busy,
  error,
  onBack,
  onPaid,
}: {
  plan: Plan
  data: PlansResponse
  busy: boolean
  error: unknown
  onBack: () => void
  onPaid: () => void
}) {
  let errorText: string | null = null
  if (error) {
    if (isApiError(error, 409) && error.code === 'pending_exists') errorText = t.renew.pending
    else if (isApiError(error, 409) && error.code === 'unlimited') errorText = t.renew.unlimited
    else if (isApiError(error, 502)) errorText = t.panelUnavailable
    else errorText = t.errorGeneric
  }
  return (
    <>
      <Card>
        <div className="flex items-baseline justify-between">
          <span className="text-slate-300">{plan.title}</span>
          <span className="text-2xl font-semibold">{formatMoney(plan.price, data.currency)}</span>
        </div>
      </Card>
      <Card>
        <h2 className="mb-3 font-semibold">{t.renew.requisites}</h2>
        <div className="flex flex-col gap-3">
          {data.requisites.map((r) => (
            <RequisiteRow key={r.label + r.value} label={r.label} value={r.value} note={r.note} />
          ))}
        </div>
        {data.paymentNote && <p className="mt-4 text-sm text-slate-400">{data.paymentNote}</p>}
      </Card>
      {errorText && <Card className="border-rose-900/60 bg-rose-950/30 text-sm text-rose-200">{errorText}</Card>}
      <Button onClick={onPaid} disabled={busy} className="w-full">
        {busy ? t.renew.sending : `${t.renew.paid} ${formatMoney(plan.price, data.currency)}`}
      </Button>
      <Button variant="ghost" onClick={onBack} disabled={busy}>
        {t.back}
      </Button>
    </>
  )
}

function RequisiteRow({ label, value, note }: { label: string; value: string; note?: string }) {
  const [copied, setCopied] = useState(false)
  const onCopy = async () => {
    if (await copyText(value)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }
  return (
    <div className="flex items-center justify-between gap-3">
      <div className="min-w-0">
        <div className="text-xs text-slate-400">{label}</div>
        <div className="break-all font-mono text-base">{value}</div>
        {note && <div className="text-xs text-slate-500">{note}</div>}
      </div>
      <Button variant="secondary" className="min-h-10 shrink-0 px-3 py-2 text-sm" onClick={() => void onCopy()}>
        {copied ? t.link.copied : t.link.copy}
      </Button>
    </div>
  )
}

function History({ requests }: { requests: RenewalRequest[] }) {
  return (
    <Card>
      <h2 className="mb-3 font-semibold">{t.renew.history}</h2>
      <ul className="flex flex-col gap-2 text-sm">
        {requests.map((r) => (
          <li key={r.id} className="flex items-center justify-between gap-2">
            <span className="text-slate-300">
              {r.planTitle} · {formatMoney(r.amount, r.currency)}
              <span className="block text-xs text-slate-500">{formatDateTime(r.createdAt)}</span>
            </span>
            <RenewalStatusChip status={r.status} />
          </li>
        ))}
      </ul>
    </Card>
  )
}

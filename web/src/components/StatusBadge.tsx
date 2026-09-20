import type { SubscriptionStatus } from '../types'
import { t } from '../i18n/ru'

const colors: Record<SubscriptionStatus, string> = {
  unlimited: 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/30',
  active: 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/30',
  expiring: 'bg-amber-500/15 text-amber-300 ring-amber-500/30',
  expired: 'bg-rose-500/15 text-rose-300 ring-rose-500/30',
  depleted: 'bg-rose-500/15 text-rose-300 ring-rose-500/30',
  disabled: 'bg-slate-500/15 text-slate-300 ring-slate-500/30',
}

export default function StatusBadge({ status }: { status: SubscriptionStatus }) {
  return <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ring-1 ${colors[status]}`}>{t.status[status]}</span>
}

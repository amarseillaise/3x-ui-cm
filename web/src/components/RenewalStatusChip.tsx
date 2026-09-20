import type { RenewalStatus } from '../types'
import { t } from '../i18n/ru'

const colors: Record<RenewalStatus, string> = {
  pending: 'bg-amber-500/15 text-amber-300',
  confirmed: 'bg-emerald-500/15 text-emerald-300',
  rejected: 'bg-rose-500/15 text-rose-300',
  failed: 'bg-slate-500/15 text-slate-300',
}

export default function RenewalStatusChip({ status }: { status: RenewalStatus }) {
  return <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${colors[status]}`}>{t.renewalStatus[status]}</span>
}

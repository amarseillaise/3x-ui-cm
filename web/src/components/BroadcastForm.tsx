import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, isApiError } from '../api'
import { t } from '../i18n/ru'
import Button from './Button'
import Card from './Card'

interface BroadcastResult {
  recipients: number
  sent: number
  failed: number
  removed: number
  unknownEmails: string[]
}

export default function BroadcastForm({ pushEnabled }: { pushEnabled: boolean }) {
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [url, setUrl] = useState('')
  const [mode, setMode] = useState<'all' | 'emails'>('all')
  const [emails, setEmails] = useState('')

  const send = useMutation({
    mutationFn: () => {
      const target = mode === 'all' ? { all: true } : { emails: emails.split(/[\s,;]+/).filter(Boolean) }
      return api<BroadcastResult>('/api/admin/push', { method: 'POST', body: JSON.stringify({ title, body, url, target }) })
    },
  })

  if (!pushEnabled) return <Card className="text-sm text-slate-400">{t.admin.broadcast.disabled}</Card>

  const input = 'w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-base outline-none focus:border-blue-500'
  const canSend = title.trim().length > 0 && (mode === 'all' || emails.trim().length > 0) && !send.isPending
  return (
    <Card>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          if (canSend) send.mutate()
        }}
      >
        <label className="flex flex-col gap-1 text-sm text-slate-400">
          {t.admin.broadcast.title}
          <input className={input} value={title} onChange={(e) => setTitle(e.target.value)} maxLength={100} required />
        </label>
        <label className="flex flex-col gap-1 text-sm text-slate-400">
          {t.admin.broadcast.body}
          <textarea className={`${input} min-h-24`} value={body} onChange={(e) => setBody(e.target.value)} maxLength={500} />
        </label>
        <label className="flex flex-col gap-1 text-sm text-slate-400">
          {t.admin.broadcast.url}
          <input className={input} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="/renew" />
        </label>
        <div className="grid grid-cols-2 gap-1 rounded-xl bg-slate-950 p-1 text-sm">
          {(['all', 'emails'] as const).map((m) => (
            <button key={m} type="button" onClick={() => setMode(m)} className={`min-h-10 rounded-lg ${mode === m ? 'bg-slate-700 text-white' : 'text-slate-400'}`}>
              {t.admin.broadcast.target[m]}
            </button>
          ))}
        </div>
        {mode === 'emails' && (
          <label className="flex flex-col gap-1 text-sm text-slate-400">
            {t.admin.broadcast.emailsHint}
            <textarea className={`${input} min-h-20 font-mono text-sm`} value={emails} onChange={(e) => setEmails(e.target.value)} />
          </label>
        )}
        <Button type="submit" disabled={!canSend}>
          {send.isPending ? t.admin.broadcast.sending : t.admin.broadcast.send}
        </Button>
        {send.isSuccess && (
          <p className="text-sm text-emerald-300">
            {t.admin.broadcast.result(send.data.sent, send.data.recipients)}
            {send.data.unknownEmails.length > 0 && (
              <span className="block text-amber-300">
                {t.admin.broadcast.unknown}: {send.data.unknownEmails.join(', ')}
              </span>
            )}
          </p>
        )}
        {send.error && (
          <p className="text-sm text-rose-300">{isApiError(send.error) ? send.error.message : t.errorGeneric}</p>
        )}
      </form>
    </Card>
  )
}

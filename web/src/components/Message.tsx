import type { ReactNode } from 'react'

export default function Message({ title, body, children }: { title: string; body: string; children?: ReactNode }) {
  return (
    <div className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-xl font-semibold">{title}</h1>
      <p className="text-slate-400">{body}</p>
      {children}
    </div>
  )
}

import type { ReactNode } from 'react'

export default function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <section className={`rounded-2xl border border-slate-800 bg-slate-900/70 p-4 ${className}`}>{children}</section>
}

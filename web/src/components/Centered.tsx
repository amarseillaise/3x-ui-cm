import type { ReactNode } from 'react'

export default function Centered({ children }: { children: ReactNode }) {
  return <div className="flex min-h-dvh items-center justify-center p-6 text-center text-slate-300">{children}</div>
}

import type { ButtonHTMLAttributes } from 'react'

type Variant = 'primary' | 'secondary' | 'ghost'

const styles: Record<Variant, string> = {
  primary: 'bg-blue-600 text-white hover:bg-blue-500 disabled:bg-slate-700 disabled:text-slate-400',
  secondary: 'bg-slate-800 text-slate-100 hover:bg-slate-700 disabled:text-slate-500',
  ghost: 'bg-transparent text-slate-300 hover:bg-slate-800',
}

export default function Button({ variant = 'primary', className = '', ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }) {
  return (
    <button
      type="button"
      className={`min-h-12 rounded-xl px-4 py-3 text-base font-medium transition active:scale-[0.98] disabled:cursor-not-allowed ${styles[variant]} ${className}`}
      {...rest}
    />
  )
}

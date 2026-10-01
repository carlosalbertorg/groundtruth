import type { ReactNode } from 'react'

export function AuthCard({
  title,
  subtitle,
  children,
}: {
  title: string
  subtitle?: string
  children: ReactNode
}) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-neutral-950 px-4">
      <div className="w-full max-w-sm rounded-lg border border-neutral-800 bg-neutral-900 p-6 shadow-lg">
        <h1 className="text-lg font-semibold text-neutral-100">groundtruth</h1>
        <p className="mt-1 text-sm text-neutral-400">{title}</p>
        {subtitle && <p className="mt-0.5 text-xs text-neutral-500">{subtitle}</p>}
        <div className="mt-6">{children}</div>
      </div>
    </div>
  )
}

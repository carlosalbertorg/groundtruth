const STYLES: Record<string, string> = {
  clean: 'bg-emerald-950 text-emerald-400',
  drifted: 'bg-amber-950 text-amber-400',
  failed: 'bg-red-950 text-red-400',
  running: 'bg-neutral-800 text-neutral-300',
  queued: 'bg-neutral-800 text-neutral-400',
}

export function StatusBadge({ status }: { status: string | null }) {
  if (!status) {
    return <span className="text-xs text-neutral-500">never checked</span>
  }
  const style = STYLES[status] ?? 'bg-neutral-800 text-neutral-300'
  return <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${style}`}>{status}</span>
}

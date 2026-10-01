import type { ResourceDrift } from '../api/checks'

function formatValue(v: unknown): string {
  if (v === undefined) return '—'
  if (v === null) return 'null'
  if (typeof v === 'string') return v
  return JSON.stringify(v)
}

function valuesEqual(a: unknown, b: unknown): boolean {
  if (a === undefined) a = null
  if (b === undefined) b = null
  return JSON.stringify(a) === JSON.stringify(b)
}

const ACTION_LABEL: Record<string, string> = {
  'no-op': 'No change',
  create: 'Created',
  update: 'Updated',
  delete: 'Deleted',
  replace: 'Replaced',
}

export function ResourceDiffTable({ resource }: { resource: ResourceDrift }) {
  const before = resource.before ?? {}
  const after = resource.after ?? {}
  const keys = Array.from(new Set([...Object.keys(before), ...Object.keys(after)])).sort()

  return (
    <div className="rounded-lg border border-neutral-800">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-neutral-800 px-4 py-3">
        <div className="min-w-0 break-all">
          <span className="font-mono text-sm text-neutral-100">{resource.address}</span>
          <span className="ml-2 text-xs text-neutral-500">{resource.type}</span>
        </div>
        <div className="flex items-center gap-2">
          {resource.has_sensitive && (
            <span className="rounded bg-neutral-800 px-1.5 py-0.5 text-xs text-neutral-400">
              has sensitive values
            </span>
          )}
          <span className="text-xs text-neutral-400">
            {ACTION_LABEL[resource.action] ?? resource.action}
          </span>
        </div>
      </div>

      {keys.length === 0 ? (
        <p className="px-4 py-3 text-sm text-neutral-500">No attribute detail.</p>
      ) : (
        <table className="w-full table-fixed text-left text-sm">
          <thead className="text-neutral-500">
            <tr className="border-b border-neutral-800">
              <th className="w-1/4 px-4 py-2 font-medium">Attribute</th>
              <th className="w-[37.5%] px-4 py-2 font-medium">Before</th>
              <th className="w-[37.5%] px-4 py-2 font-medium">After</th>
            </tr>
          </thead>
          <tbody>
            {keys.map((key) => {
              const changed = !valuesEqual(before[key], after[key])
              return (
                <tr key={key} className="border-b border-neutral-900 last:border-0">
                  <td className="px-4 py-2 font-mono text-xs break-all text-neutral-400">{key}</td>
                  <td
                    className={`px-4 py-2 font-mono text-xs break-all ${changed ? 'text-red-400/90' : 'text-neutral-500'}`}
                  >
                    {formatValue(before[key])}
                  </td>
                  <td
                    className={`px-4 py-2 font-mono text-xs break-all ${changed ? 'text-emerald-400/90' : 'text-neutral-500'}`}
                  >
                    {formatValue(after[key])}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}

import { Link, useParams } from 'react-router-dom'
import { useRunCheck, useWorkspaceChecks } from '../api/checks'
import { useWorkspace } from '../api/workspaces'
import { StatusBadge } from '../components/StatusBadge'

export function WorkspaceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const workspace = useWorkspace(id!)
  const history = useWorkspaceChecks(id!)
  const runCheck = useRunCheck(id!)

  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <header className="border-b border-neutral-800 px-4 py-4 sm:px-8">
        <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">
          &larr; Workspaces
        </Link>
      </header>

      <main className="p-4 sm:p-8">
        {workspace.isPending && <p className="text-sm text-neutral-400">Loading…</p>}
        {workspace.isError && (
          <p className="text-sm text-red-400">Couldn&rsquo;t load workspace.</p>
        )}

        {workspace.data && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="min-w-0">
                <h1 className="text-lg font-semibold">{workspace.data.name}</h1>
                <p className="mt-1 truncate text-sm text-neutral-500">
                  {workspace.data.source_path}
                </p>
              </div>
              <button
                onClick={() => runCheck.mutate()}
                disabled={runCheck.isPending}
                className="btn-primary shrink-0 disabled:opacity-50"
              >
                {runCheck.isPending ? 'Checking…' : 'Check now'}
              </button>
            </div>

            <h2 className="mt-8 text-sm font-medium text-neutral-300">History</h2>

            {history.isPending && <p className="mt-4 text-sm text-neutral-400">Loading…</p>}
            {history.isError && (
              <p className="mt-4 text-sm text-red-400">Couldn&rsquo;t load check history.</p>
            )}
            {history.data && history.data.length === 0 && (
              <p className="mt-4 text-sm text-neutral-500">No checks yet.</p>
            )}

            {history.data && history.data.length > 0 && (
              <div className="mt-4 overflow-x-auto">
                <table className="w-full min-w-[640px] text-left text-sm">
                  <thead className="text-neutral-500">
                    <tr className="border-b border-neutral-800">
                      <th className="py-2 font-medium">When</th>
                      <th className="py-2 font-medium">Status</th>
                      <th className="py-2 font-medium">Summary</th>
                      <th className="py-2 font-medium">Triggered by</th>
                    </tr>
                  </thead>
                  <tbody>
                    {history.data.map((check) => (
                      <tr key={check.id} className="border-b border-neutral-900">
                        <td className="py-3">
                          <Link to={`/checks/${check.id}`} className="hover:underline">
                            {new Date(check.started_at).toLocaleString()}
                          </Link>
                        </td>
                        <td className="py-3">
                          <StatusBadge status={check.status} />
                        </td>
                        <td className="py-3 text-neutral-400">
                          {check.status === 'failed'
                            ? check.error_message
                            : `+${check.summary.added} ~${check.summary.changed} -${check.summary.destroyed}`}
                        </td>
                        <td className="py-3 text-neutral-500">{check.triggered_by}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </main>
    </div>
  )
}

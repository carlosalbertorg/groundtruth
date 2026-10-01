import { Link, useParams } from 'react-router-dom'
import { useCheckDetail } from '../api/checks'
import { ResourceDiffTable } from '../components/ResourceDiffTable'
import { StatusBadge } from '../components/StatusBadge'

export function CheckDetailPage() {
  const { id } = useParams<{ id: string }>()
  const check = useCheckDetail(id!)

  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <header className="border-b border-neutral-800 px-8 py-4">
        {check.data ? (
          <Link
            to={`/workspaces/${check.data.workspace_id}`}
            className="text-sm text-neutral-400 hover:text-neutral-200"
          >
            &larr; Workspace
          </Link>
        ) : (
          <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">
            &larr; Workspaces
          </Link>
        )}
      </header>

      <main className="p-8">
        {check.isPending && <p className="text-sm text-neutral-400">Loading…</p>}
        {check.isError && <p className="text-sm text-red-400">Couldn&rsquo;t load this check.</p>}

        {check.data && (
          <>
            <div className="flex items-center gap-3">
              <h1 className="text-lg font-semibold">
                {new Date(check.data.started_at).toLocaleString()}
              </h1>
              <StatusBadge status={check.data.status} />
            </div>
            <p className="mt-1 text-sm text-neutral-500">
              Triggered {check.data.triggered_by}
              {check.data.duration_ms !== null &&
                ` · ${(check.data.duration_ms / 1000).toFixed(1)}s`}
            </p>

            {check.data.status === 'failed' && (
              <p className="mt-4 rounded border border-red-900 bg-red-950/50 px-4 py-3 text-sm text-red-300">
                {check.data.error_message}
              </p>
            )}

            {check.data.resources &&
              check.data.resources.length === 0 &&
              check.data.status !== 'failed' && (
                <p className="mt-6 text-sm text-neutral-500">No drift detected.</p>
              )}

            <div className="mt-6 flex flex-col gap-4">
              {check.data.resources?.map((resource) => (
                <ResourceDiffTable key={resource.address} resource={resource} />
              ))}
            </div>
          </>
        )}
      </main>
    </div>
  )
}

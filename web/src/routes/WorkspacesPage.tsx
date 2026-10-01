import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useLogout } from '../api/auth'
import { describeCheckError, useRunCheck } from '../api/checks'
import { type Workspace, useDeleteWorkspace, useWorkspaces } from '../api/workspaces'
import { StatusBadge } from '../components/StatusBadge'
import { WorkspaceFormDialog } from './WorkspaceFormDialog'

export function WorkspacesPage({ email }: { email: string }) {
  const workspaces = useWorkspaces()
  const logout = useLogout()
  const deleteWorkspace = useDeleteWorkspace()
  const [dialogState, setDialogState] = useState<{ open: boolean; workspace?: Workspace }>({
    open: false,
  })

  function handleDelete(ws: Workspace) {
    if (!window.confirm(`Delete workspace "${ws.name}"? This cannot be undone.`)) return
    deleteWorkspace.mutate(ws.id)
  }

  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-neutral-800 px-4 py-4 sm:px-8">
        <h1 className="text-lg font-semibold">groundtruth</h1>
        <div className="flex flex-wrap items-center gap-4 text-sm text-neutral-400">
          <Link to="/settings" className="hover:text-neutral-200">
            Settings
          </Link>
          <span className="max-w-[12rem] truncate sm:max-w-none">{email}</span>
          <button onClick={() => logout.mutate()} className="hover:text-neutral-200">
            Sign out
          </button>
        </div>
      </header>

      <main className="p-4 sm:p-8">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-medium">Workspaces</h2>
          <button onClick={() => setDialogState({ open: true })} className="btn-primary">
            New workspace
          </button>
        </div>

        {workspaces.isPending && <p className="mt-6 text-sm text-neutral-400">Loading…</p>}
        {workspaces.isError && (
          <p className="mt-6 text-sm text-red-400">Couldn&rsquo;t load workspaces.</p>
        )}

        {workspaces.data && workspaces.data.length === 0 && (
          <p className="mt-6 text-sm text-neutral-500">
            No workspaces yet. Add one to start checking for drift.
          </p>
        )}

        {workspaces.data && workspaces.data.length > 0 && (
          <div className="mt-6 overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-sm">
              <thead className="text-neutral-500">
                <tr className="border-b border-neutral-800">
                  <th className="py-2 font-medium">Name</th>
                  <th className="py-2 font-medium">Binary</th>
                  <th className="py-2 font-medium">Interval</th>
                  <th className="py-2 font-medium">Status</th>
                  <th className="py-2 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                {workspaces.data.map((ws) => (
                  <WorkspaceRow
                    key={ws.id}
                    workspace={ws}
                    onEdit={() => setDialogState({ open: true, workspace: ws })}
                    onDelete={() => handleDelete(ws)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </main>

      <WorkspaceFormDialog
        open={dialogState.open}
        workspace={dialogState.workspace}
        onOpenChange={(open) => setDialogState((s) => ({ ...s, open }))}
      />
    </div>
  )
}

function WorkspaceRow({
  workspace: ws,
  onEdit,
  onDelete,
}: {
  workspace: Workspace
  onEdit: () => void
  onDelete: () => void
}) {
  const runCheck = useRunCheck(ws.id)

  return (
    <tr className="border-b border-neutral-900">
      <td className="py-3">
        <Link to={`/workspaces/${ws.id}`} className="hover:underline">
          {ws.name}
        </Link>
        {!ws.is_enabled && (
          <span className="ml-2 rounded bg-neutral-800 px-1.5 py-0.5 text-xs text-neutral-400">
            disabled
          </span>
        )}
      </td>
      <td className="py-3 text-neutral-400">
        {ws.binary_kind === 'tofu' ? 'OpenTofu' : 'Terraform'}
      </td>
      <td className="py-3 text-neutral-400">every {ws.check_interval_minutes}m</td>
      <td className="py-3">
        <StatusBadge status={ws.last_check_status} />
      </td>
      <td className="py-3 text-right whitespace-nowrap">
        <button
          onClick={() => runCheck.mutate()}
          disabled={runCheck.isPending}
          className="text-neutral-400 hover:text-neutral-200 disabled:opacity-50"
        >
          {runCheck.isPending ? 'Checking…' : 'Check now'}
        </button>
        <button onClick={onEdit} className="ml-4 text-neutral-400 hover:text-neutral-200">
          Edit
        </button>
        <button onClick={onDelete} className="ml-4 text-red-400/80 hover:text-red-400">
          Delete
        </button>
        {runCheck.isError && (
          <p role="alert" className="mt-1 text-xs whitespace-normal text-red-400">
            {describeCheckError(runCheck.error)}
          </p>
        )}
      </td>
    </tr>
  )
}

import { useState } from 'react'
import { useLogout } from '../api/auth'
import { type Workspace, useDeleteWorkspace, useWorkspaces } from '../api/workspaces'
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
      <header className="flex items-center justify-between border-b border-neutral-800 px-8 py-4">
        <h1 className="text-lg font-semibold">groundtruth</h1>
        <div className="flex items-center gap-4 text-sm text-neutral-400">
          <span>{email}</span>
          <button onClick={() => logout.mutate()} className="hover:text-neutral-200">
            Sign out
          </button>
        </div>
      </header>

      <main className="p-8">
        <div className="flex items-center justify-between">
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
          <table className="mt-6 w-full text-left text-sm">
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
                <tr key={ws.id} className="border-b border-neutral-900">
                  <td className="py-3">
                    {ws.name}
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
                  <td className="py-3 text-neutral-500">
                    {ws.last_check_status ?? 'never checked'}
                  </td>
                  <td className="py-3 text-right">
                    <button
                      onClick={() => setDialogState({ open: true, workspace: ws })}
                      className="text-neutral-400 hover:text-neutral-200"
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => handleDelete(ws)}
                      className="ml-4 text-red-400/80 hover:text-red-400"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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

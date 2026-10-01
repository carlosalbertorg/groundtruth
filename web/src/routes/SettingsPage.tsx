import { type FormEvent, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  type AlertDestination,
  type AlertDestinationInput,
  useAlertDestinations,
  useCreateAlertDestination,
  useDeleteAlertDestination,
  useUpdateAlertDestination,
} from '../api/alertDestinations'
import { useAPITokens, useCreateAPIToken, useRevokeAPIToken } from '../api/apiTokens'

export function SettingsPage() {
  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <header className="border-b border-neutral-800 px-4 py-4 sm:px-8">
        <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">
          &larr; Workspaces
        </Link>
      </header>

      <main className="mx-auto max-w-3xl p-4 sm:p-8">
        <h1 className="text-lg font-semibold">Settings</h1>

        <AlertDestinationsSection />
        <APITokensSection />
      </main>
    </div>
  )
}

function emptyDestinationForm(): AlertDestinationInput {
  return {
    workspace_id: null,
    name: '',
    kind: 'generic_webhook',
    url: '',
    shared_secret: '',
    is_enabled: true,
  }
}

function AlertDestinationsSection() {
  const destinations = useAlertDestinations()
  const create = useCreateAlertDestination()
  const update = useUpdateAlertDestination()
  const remove = useDeleteAlertDestination()
  const [editingID, setEditingID] = useState<string | null>(null)
  const [form, setForm] = useState<AlertDestinationInput>(emptyDestinationForm())
  const [showForm, setShowForm] = useState(false)

  function startEdit(dest: AlertDestination) {
    setEditingID(dest.id)
    setForm({
      workspace_id: dest.workspace_id,
      name: dest.name,
      kind: dest.kind,
      url: dest.url,
      shared_secret: '',
      is_enabled: dest.is_enabled,
    })
    setShowForm(true)
  }

  function startCreate() {
    setEditingID(null)
    setForm(emptyDestinationForm())
    setShowForm(true)
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const input: AlertDestinationInput = { ...form, shared_secret: form.shared_secret || undefined }
    const onSuccess = () => setShowForm(false)
    if (editingID) {
      update.mutate({ id: editingID, input }, { onSuccess })
    } else {
      create.mutate(input, { onSuccess })
    }
  }

  const mutation = editingID ? update : create

  return (
    <section className="mt-8">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-neutral-300">Alert destinations</h2>
        {!showForm && (
          <button onClick={startCreate} className="btn-primary">
            New destination
          </button>
        )}
      </div>
      <p className="mt-1 text-xs text-neutral-500">
        Notified when a workspace transitions into drift, out of drift, or into a failed check.
      </p>

      {destinations.isPending && <p className="mt-4 text-sm text-neutral-400">Loading…</p>}
      {destinations.isError && (
        <p className="mt-4 text-sm text-red-400">Couldn&rsquo;t load alert destinations.</p>
      )}
      {destinations.data && destinations.data.length === 0 && !showForm && (
        <p className="mt-4 text-sm text-neutral-500">No alert destinations configured.</p>
      )}

      {destinations.data && destinations.data.length > 0 && (
        <ul className="mt-4 flex flex-col gap-2">
          {destinations.data.map((dest) => (
            <li
              key={dest.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded border border-neutral-800 px-3 py-2 text-sm"
            >
              <div className="min-w-0 break-all">
                <span className="font-medium">{dest.name}</span>
                <span className="ml-2 text-xs text-neutral-500">
                  {dest.kind === 'slack' ? 'Slack' : 'Webhook'} &middot; {dest.url}
                </span>
                {!dest.is_enabled && (
                  <span className="ml-2 rounded bg-neutral-800 px-1.5 py-0.5 text-xs text-neutral-400">
                    disabled
                  </span>
                )}
              </div>
              <div>
                <button
                  onClick={() => startEdit(dest)}
                  className="text-neutral-400 hover:text-neutral-200"
                >
                  Edit
                </button>
                <button
                  onClick={() => remove.mutate(dest.id)}
                  className="ml-4 text-red-400/80 hover:text-red-400"
                >
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {showForm && (
        <form
          onSubmit={handleSubmit}
          className="mt-4 flex flex-col gap-3 rounded border border-neutral-800 p-4"
        >
          <label className="flex flex-col gap-1 text-sm text-neutral-300">
            Name
            <input
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="input"
            />
          </label>
          <label className="flex flex-col gap-1 text-sm text-neutral-300">
            Kind
            <select
              value={form.kind}
              onChange={(e) =>
                setForm({ ...form, kind: e.target.value as AlertDestinationInput['kind'] })
              }
              className="input"
            >
              <option value="generic_webhook">Generic webhook</option>
              <option value="slack">Slack</option>
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm text-neutral-300">
            URL
            <input
              required
              type="url"
              value={form.url}
              onChange={(e) => setForm({ ...form, url: e.target.value })}
              className="input"
            />
          </label>
          <label className="flex flex-col gap-1 text-sm text-neutral-300">
            Shared secret
            <input
              value={form.shared_secret}
              onChange={(e) => setForm({ ...form, shared_secret: e.target.value })}
              placeholder={editingID ? 'Leave blank to keep the current secret' : 'Optional'}
              className="input"
            />
          </label>
          <label className="flex items-center gap-2 text-sm text-neutral-300">
            <input
              type="checkbox"
              checked={form.is_enabled}
              onChange={(e) => setForm({ ...form, is_enabled: e.target.checked })}
            />
            Enabled
          </label>
          {mutation.isError && (
            <p className="text-sm text-red-400">Something went wrong. Please try again.</p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowForm(false)}
              className="text-sm text-neutral-400"
            >
              Cancel
            </button>
            <button type="submit" disabled={mutation.isPending} className="btn-primary">
              {mutation.isPending ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      )}
    </section>
  )
}

function APITokensSection() {
  const tokens = useAPITokens()
  const create = useCreateAPIToken()
  const revoke = useRevokeAPIToken()
  const [name, setName] = useState('')
  const [justCreated, setJustCreated] = useState<string | null>(null)

  function handleCreate(e: FormEvent) {
    e.preventDefault()
    create.mutate(name, {
      onSuccess: (token) => {
        setJustCreated(token.token)
        setName('')
      },
    })
  }

  return (
    <section className="mt-10">
      <h2 className="text-sm font-medium text-neutral-300">API tokens</h2>
      <p className="mt-1 text-xs text-neutral-500">
        For triggering a check from CI without a browser session (
        <code>Authorization: Bearer &lt;token&gt;</code>).
      </p>

      {justCreated && (
        <div className="mt-4 rounded border border-amber-900 bg-amber-950/50 px-3 py-2 text-sm text-amber-200">
          <p>Save this token now &mdash; it won&rsquo;t be shown again:</p>
          <code className="mt-1 block break-all text-xs">{justCreated}</code>
          <button
            onClick={() => setJustCreated(null)}
            className="mt-2 text-xs text-amber-400 hover:underline"
          >
            Dismiss
          </button>
        </div>
      )}

      {tokens.isPending && <p className="mt-4 text-sm text-neutral-400">Loading…</p>}
      {tokens.isError && (
        <p className="mt-4 text-sm text-red-400">Couldn&rsquo;t load API tokens.</p>
      )}
      {tokens.data && tokens.data.length === 0 && (
        <p className="mt-4 text-sm text-neutral-500">No API tokens yet.</p>
      )}

      {tokens.data && tokens.data.length > 0 && (
        <ul className="mt-4 flex flex-col gap-2">
          {tokens.data.map((t) => (
            <li
              key={t.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded border border-neutral-800 px-3 py-2 text-sm"
            >
              <div className="min-w-0 break-all">
                <span className="font-medium">{t.name}</span>
                <span className="ml-2 text-xs text-neutral-500">
                  {t.last_used_at
                    ? `last used ${new Date(t.last_used_at).toLocaleString()}`
                    : 'never used'}
                </span>
              </div>
              <button
                onClick={() => revoke.mutate(t.id)}
                className="text-red-400/80 hover:text-red-400"
              >
                Revoke
              </button>
            </li>
          ))}
        </ul>
      )}

      <form onSubmit={handleCreate} className="mt-4 flex gap-2">
        <input
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Token name (e.g. github-actions)"
          className="input flex-1"
        />
        <button type="submit" disabled={create.isPending} className="btn-primary">
          Create
        </button>
      </form>
    </section>
  )
}

import { type FormEvent, useEffect, useState } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../components/ui/dialog'
import {
  type BinaryKind,
  type Workspace,
  type WorkspaceInput,
  useCreateWorkspace,
  useUpdateWorkspace,
} from '../api/workspaces'
import { ApiError } from '../api/client'

interface FormState {
  name: string
  description: string
  sourcePath: string
  workingSubdirectory: string
  binaryKind: BinaryKind
  binaryVersion: string
  credentialEnvFile: string
  checkIntervalMinutes: string
  checkTimeoutSeconds: string
  isEnabled: boolean
}

function emptyForm(): FormState {
  return {
    name: '',
    description: '',
    sourcePath: '',
    workingSubdirectory: '',
    binaryKind: 'terraform',
    binaryVersion: '',
    credentialEnvFile: '',
    checkIntervalMinutes: '60',
    checkTimeoutSeconds: '600',
    isEnabled: true,
  }
}

function formFromWorkspace(ws: Workspace): FormState {
  return {
    name: ws.name,
    description: ws.description ?? '',
    sourcePath: ws.source_path,
    workingSubdirectory: ws.working_subdirectory ?? '',
    binaryKind: ws.binary_kind,
    binaryVersion: ws.binary_version ?? '',
    credentialEnvFile: ws.credential_env_file ?? '',
    checkIntervalMinutes: String(ws.check_interval_minutes),
    checkTimeoutSeconds: String(ws.check_timeout_seconds),
    isEnabled: ws.is_enabled,
  }
}

function toInput(form: FormState): WorkspaceInput {
  return {
    name: form.name.trim(),
    description: form.description.trim() || null,
    source_path: form.sourcePath.trim(),
    working_subdirectory: form.workingSubdirectory.trim() || null,
    binary_kind: form.binaryKind,
    binary_version: form.binaryVersion.trim() || null,
    credential_env_file: form.credentialEnvFile.trim() || null,
    check_interval_minutes: Number(form.checkIntervalMinutes),
    check_timeout_seconds: Number(form.checkTimeoutSeconds),
    is_enabled: form.isEnabled,
  }
}

function describeError(err: unknown): string {
  if (!(err instanceof ApiError)) return 'Something went wrong. Please try again.'
  switch (err.code) {
    case 'name_required':
      return 'Name is required.'
    case 'source_path_required':
      return 'Source path is required.'
    case 'source_path_must_be_absolute':
      return 'Source path must be an absolute path inside the container, starting with /.'
    case 'invalid_working_subdirectory':
      return 'Working subdirectory must be a relative path that stays inside the module.'
    case 'credential_env_file_must_be_absolute':
      return 'Credential env file must be an absolute path inside the container, starting with /.'
    case 'invalid_binary_kind':
      return 'Binary kind must be Terraform or OpenTofu.'
    case 'invalid_check_interval_minutes':
      return 'Check interval must be a positive number of minutes.'
    case 'invalid_check_timeout_seconds':
      return 'Check timeout must be between 1 and 3600 seconds.'
    case 'name_taken':
      return 'A workspace with this name already exists.'
    default:
      return 'Something went wrong. Please try again.'
  }
}

export function WorkspaceFormDialog({
  open,
  onOpenChange,
  workspace,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  workspace?: Workspace
}) {
  const [form, setForm] = useState<FormState>(emptyForm)
  const createWorkspace = useCreateWorkspace()
  const updateWorkspace = useUpdateWorkspace()
  const isEditing = workspace !== undefined
  const mutation = isEditing ? updateWorkspace : createWorkspace

  useEffect(() => {
    if (open) {
      setForm(workspace ? formFromWorkspace(workspace) : emptyForm())
      mutation.reset()
    }
    // mutation identity changes every render; only reset the form when
    // the dialog opens or the workspace being edited changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, workspace])

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const input = toInput(form)

    if (isEditing) {
      updateWorkspace.mutate({ id: workspace.id, input }, { onSuccess: () => onOpenChange(false) })
    } else {
      createWorkspace.mutate(input, { onSuccess: () => onOpenChange(false) })
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle className="text-lg font-semibold text-neutral-100">
          {isEditing ? 'Edit workspace' : 'New workspace'}
        </DialogTitle>
        <DialogDescription className="mt-1 text-sm text-neutral-400">
          Points groundtruth at a Terraform or OpenTofu root module to check for drift.
        </DialogDescription>

        <form
          onSubmit={handleSubmit}
          className="mt-6 flex max-h-[70vh] flex-col gap-4 overflow-y-auto"
        >
          <LabeledInput
            label="Name"
            required
            value={form.name}
            onChange={(v) => setForm({ ...form, name: v })}
          />
          <LabeledInput
            label="Description"
            value={form.description}
            onChange={(v) => setForm({ ...form, description: v })}
          />
          <LabeledInput
            label="Source path"
            required
            value={form.sourcePath}
            onChange={(v) => setForm({ ...form, sourcePath: v })}
            hint="Path to the root module inside the groundtruth container, e.g. /modules/prod-network."
          />
          <LabeledInput
            label="Working subdirectory"
            value={form.workingSubdirectory}
            onChange={(v) => setForm({ ...form, workingSubdirectory: v })}
            hint="Optional, for monorepos."
          />

          <div className="flex flex-col gap-1">
            <label className="text-sm font-medium text-neutral-300" htmlFor="binary-kind">
              Binary
            </label>
            <select
              id="binary-kind"
              className="input"
              value={form.binaryKind}
              onChange={(e) => setForm({ ...form, binaryKind: e.target.value as BinaryKind })}
            >
              <option value="terraform">Terraform</option>
              <option value="tofu">OpenTofu</option>
            </select>
          </div>

          <LabeledInput
            label="Binary version"
            value={form.binaryVersion}
            onChange={(v) => setForm({ ...form, binaryVersion: v })}
            hint="Optional note for your own reference — not enforced. groundtruth always runs the terraform/tofu binary installed in its image."
          />
          <LabeledInput
            label="Credential env file"
            value={form.credentialEnvFile}
            onChange={(v) => setForm({ ...form, credentialEnvFile: v })}
            hint="Optional absolute path to an operator-mounted env file (e.g. /secrets/credentials.env), read fresh on every check. Its contents are never stored by groundtruth."
          />

          <div className="grid grid-cols-2 gap-4">
            <LabeledInput
              label="Check interval (minutes)"
              type="number"
              min={1}
              value={form.checkIntervalMinutes}
              onChange={(v) => setForm({ ...form, checkIntervalMinutes: v })}
            />
            <LabeledInput
              label="Check timeout (seconds)"
              type="number"
              min={1}
              value={form.checkTimeoutSeconds}
              onChange={(v) => setForm({ ...form, checkTimeoutSeconds: v })}
            />
          </div>

          <label className="flex items-center gap-2 text-sm text-neutral-300">
            <input
              type="checkbox"
              checked={form.isEnabled}
              onChange={(e) => setForm({ ...form, isEnabled: e.target.checked })}
            />
            Enabled
          </label>

          {mutation.isError && (
            <p className="text-sm text-red-400">{describeError(mutation.error)}</p>
          )}

          <div className="mt-2 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => onOpenChange(false)}
              className="rounded-md px-3 py-2 text-sm text-neutral-400 hover:text-neutral-200"
            >
              Cancel
            </button>
            <button type="submit" disabled={mutation.isPending} className="btn-primary">
              {mutation.isPending ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function LabeledInput({
  label,
  value,
  onChange,
  required,
  type = 'text',
  min,
  hint,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  required?: boolean
  type?: string
  min?: number
  hint?: string
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="flex flex-col gap-1 text-sm font-medium text-neutral-300">
        {label}
        <input
          type={type}
          min={min}
          required={required}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="input font-normal"
        />
      </label>
      {hint && <p className="text-xs text-neutral-500">{hint}</p>}
    </div>
  )
}

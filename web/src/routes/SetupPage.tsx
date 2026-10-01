import { type FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useCreateAdmin } from '../api/auth'
import { ApiError } from '../api/client'
import { AuthCard } from './AuthCard'

const MIN_PASSWORD_LENGTH = 12

export function SetupPage() {
  const navigate = useNavigate()
  const createAdmin = useCreateAdmin()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setFormError(null)

    if (password.length < MIN_PASSWORD_LENGTH) {
      setFormError(`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`)
      return
    }
    if (password !== confirmPassword) {
      setFormError('Passwords do not match.')
      return
    }

    createAdmin.mutate(
      { email, password },
      { onSuccess: () => navigate('/login', { replace: true }) },
    )
  }

  const serverError =
    createAdmin.error instanceof ApiError ? describeError(createAdmin.error) : null

  return (
    <AuthCard title="Create the admin account" subtitle="This is a one-time setup step.">
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <Field label="Email" htmlFor="email">
          <input
            id="email"
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input"
          />
        </Field>
        <Field label="Password" htmlFor="password">
          <input
            id="password"
            type="password"
            required
            minLength={MIN_PASSWORD_LENGTH}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input"
          />
        </Field>
        <Field label="Confirm password" htmlFor="confirm-password">
          <input
            id="confirm-password"
            type="password"
            required
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            className="input"
          />
        </Field>

        {(formError || serverError) && (
          <p className="text-sm text-red-400">{formError ?? serverError}</p>
        )}

        <button type="submit" disabled={createAdmin.isPending} className="btn-primary">
          {createAdmin.isPending ? 'Creating account…' : 'Create account'}
        </button>
      </form>
    </AuthCard>
  )
}

function describeError(err: ApiError): string {
  switch (err.code) {
    case 'invalid_email':
      return 'That email address doesn’t look valid.'
    case 'password_too_short':
      return `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`
    case 'setup_already_complete':
      return 'Setup has already been completed.'
    default:
      return 'Something went wrong. Please try again.'
  }
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string
  htmlFor: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={htmlFor} className="text-sm font-medium text-neutral-300">
        {label}
      </label>
      {children}
    </div>
  )
}

import { Navigate, Route, Routes } from 'react-router-dom'
import { useCurrentUser, useSetupStatus } from './api/auth'
import { CheckDetailPage } from './routes/CheckDetailPage'
import { LoginPage } from './routes/LoginPage'
import { SetupPage } from './routes/SetupPage'
import { WorkspaceDetailPage } from './routes/WorkspaceDetailPage'
import { WorkspacesPage } from './routes/WorkspacesPage'

function FullScreenMessage({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-neutral-950 text-neutral-400">
      {children}
    </div>
  )
}

export default function App() {
  const setupStatus = useSetupStatus()
  const currentUser = useCurrentUser(setupStatus.data?.setup_complete === true)

  if (setupStatus.isPending) {
    return <FullScreenMessage>Loading&hellip;</FullScreenMessage>
  }
  if (setupStatus.isError) {
    return <FullScreenMessage>Couldn&rsquo;t reach the server. Retrying&hellip;</FullScreenMessage>
  }

  if (!setupStatus.data.setup_complete) {
    return (
      <Routes>
        <Route path="/setup" element={<SetupPage />} />
        <Route path="*" element={<Navigate to="/setup" replace />} />
      </Routes>
    )
  }

  if (currentUser.isPending) {
    return <FullScreenMessage>Loading&hellip;</FullScreenMessage>
  }

  const user = currentUser.data ?? null

  if (!user) {
    return (
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    )
  }

  return (
    <Routes>
      <Route path="/setup" element={<Navigate to="/" replace />} />
      <Route path="/login" element={<Navigate to="/" replace />} />
      <Route path="/" element={<WorkspacesPage email={user.email} />} />
      <Route path="/workspaces/:id" element={<WorkspaceDetailPage />} />
      <Route path="/checks/:id" element={<CheckDetailPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

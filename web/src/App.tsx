import { Navigate, Route, Routes } from 'react-router-dom'
import { useCurrentUser, useSetupStatus } from './api/auth'
import { LoginPage } from './routes/LoginPage'
import { SetupPage } from './routes/SetupPage'
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

  return (
    <Routes>
      <Route path="/setup" element={<Navigate to={user ? '/' : '/login'} replace />} />
      <Route path="/login" element={user ? <Navigate to="/" replace /> : <LoginPage />} />
      <Route
        path="/"
        element={user ? <WorkspacesPage email={user.email} /> : <Navigate to="/login" replace />}
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

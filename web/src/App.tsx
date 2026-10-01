import { Routes, Route } from 'react-router-dom'

function HomePage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-neutral-950 text-neutral-100">
      <div className="text-center">
        <h1 className="text-2xl font-semibold">groundtruth</h1>
        <p className="mt-2 text-sm text-neutral-400">
          Self-hosted drift detection for Terraform &amp; OpenTofu.
        </p>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<HomePage />} />
    </Routes>
  )
}

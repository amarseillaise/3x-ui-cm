import { Navigate, Route, Routes } from 'react-router'
import { ADMIN_PATH } from './routes'
import SubscriptionPage from './pages/SubscriptionPage'
import RenewPage from './pages/RenewPage'
import AdminPage from './pages/AdminPage'
import NoSessionPage from './pages/NoSessionPage'
import InvalidLinkPage from './pages/InvalidLinkPage'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<SubscriptionPage />} />
      <Route path="/renew" element={<RenewPage />} />
      {/* Not "/admin": scanners probe that. Old /admin and /a/<secret> links fall through to "/". */}
      <Route path={ADMIN_PATH} element={<AdminPage />} />
      <Route path="/no-session" element={<NoSessionPage />} />
      <Route path="/invalid-link" element={<InvalidLinkPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

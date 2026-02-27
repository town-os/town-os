import {
  BrowserRouter,
  Routes,
  Route,
} from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster } from '@/components/ui/sonner'
import Dashboard from '@/components/layout/Dashboard.jsx'
import Login from '@/routes/Login.jsx'
import Register from '@/routes/Register.jsx'
import Logout from '@/routes/Logout.jsx'
import DashboardHome from '@/routes/DashboardHome.jsx'
import StorageManagement from '@/routes/StorageManagement.jsx'
import UserManagement from '@/routes/UserManagement.jsx'
import CreateUser from '@/routes/CreateUser.jsx'
import SystemManagement from '@/routes/SystemManagement.jsx'
import PackageManagement from '@/routes/PackageManagement.jsx'
import AuditLog from '@/routes/AuditLog.jsx'
import PagesManagement from '@/routes/PagesManagement.jsx'
import SystemSettings from '@/routes/SystemSettings.jsx'

function DashboardRoute({ children }) {
  return <Dashboard>{children}</Dashboard>
}

export default function App() {
  return (
    <TooltipProvider>
      <Toaster />
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Login />} />
          <Route path="/register" element={<Register />} />
          <Route path="/logout" element={<Logout />} />
          <Route
            path="/dashboard"
            element={
              <DashboardRoute>
                <DashboardHome />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/storage"
            element={
              <DashboardRoute>
                <StorageManagement />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/users"
            element={
              <DashboardRoute>
                <UserManagement />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/users/create"
            element={
              <DashboardRoute>
                <CreateUser />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/system"
            element={
              <DashboardRoute>
                <SystemManagement />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/packages"
            element={
              <DashboardRoute>
                <PackageManagement />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/pages"
            element={
              <DashboardRoute>
                <PagesManagement />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/log"
            element={
              <DashboardRoute>
                <AuditLog />
              </DashboardRoute>
            }
          />
          <Route
            path="/dashboard/settings"
            element={
              <DashboardRoute>
                <SystemSettings />
              </DashboardRoute>
            }
          />
        </Routes>
      </BrowserRouter>
    </TooltipProvider>
  )
}

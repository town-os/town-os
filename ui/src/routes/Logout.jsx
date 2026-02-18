import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { clearToken } from '@/lib/auth.js'

export default function Logout() {
  const navigate = useNavigate()

  useEffect(() => {
    clearToken()
    navigate('/')
  }, [navigate])

  return null
}

import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import { setToken, setAccount, getToken } from '@/lib/auth.js'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'

export default function Login() {
  const { t } = useI18n()
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  useEffect(() => { document.title = t('login.page_title') }, [t])

  useEffect(() => {
    const token = getToken()
    if (token) {
      getClient()
        .sessionUsername(token)
        .then(() => navigate('/dashboard'))
        .catch((err) => console.debug('session check failed:', err))
    }

    getClient()
      .ping()
      .then((resp) => {
        if (resp.needs_setup) navigate('/register')
      })
      .catch((err) => console.debug('ping failed:', err))
  }, [navigate])

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)
    setLoading(true)

    const form = e.target.elements
    try {
      const resp = await getClient().authenticate(
        form.username.value,
        form.password.value,
      )
      setToken(resp.token)
      setAccount(resp.account)
      navigate('/dashboard')
    } catch (err) {
      setError(t('login.error_invalid_credentials'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="w-full max-w-sm space-y-6">
      <div className="flex justify-center">
        <img src="/512.png" alt={t('login.logo_alt')} className="h-32 w-32" />
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('login.title')}</CardTitle>
          <CardDescription>{t('login.description')}</CardDescription>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-2">
              <Label htmlFor="username">{t('login.username_label')}</Label>
              <Input id="username" name="username" required autoFocus />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">{t('login.password_label')}</Label>
              <Input
                id="password"
                name="password"
                type="password"
                required
              />
            </div>
          </CardContent>
          <CardFooter className="pt-2">
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? t('login.submit_loading') : t('login.submit')}
            </Button>
          </CardFooter>
        </form>
      </Card>
      </div>
    </div>
  )
}

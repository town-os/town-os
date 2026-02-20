import { useState, useEffect } from 'react'
import { useNavigate, Link } from 'react-router-dom'
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

export default function Register() {
  useEffect(() => { document.title = 'Town OS - Register' }, [])
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [bootstrap, setBootstrap] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    getClient()
      .ping()
      .then((resp) => {
        if (resp.accounts === 0) setBootstrap(true)
      })
      .catch(() => {})
  }, [])

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)

    const form = e.target.elements
    if (form.password.value.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    if (form.password.value !== form.password2.value) {
      setError('Passwords do not match')
      return
    }

    setLoading(true)
    try {
      await getClient().createAccount(
        form.username.value,
        form.password.value,
        form.email.value || '',
        form.phone.value || '',
        form.realname.value || '',
        true,
      )

      // Auto-login after creation
      const token = getToken()
      if (!token) {
        const resp = await getClient().authenticate(
          form.username.value,
          form.password.value,
        )
        setToken(resp.token)
        setAccount(resp.account)
      }

      navigate('/dashboard')
    } catch (err) {
      setError(err.message || 'Failed to create account')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="w-full max-w-md space-y-4">
      {bootstrap && (
        <div className="text-center space-y-1">
          <h1 className="text-2xl font-bold tracking-tight">Welcome to town-os</h1>
          <p className="text-muted-foreground">
            No accounts exist yet. Create an administrator account to get started.
          </p>
        </div>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Create Account</CardTitle>
          <CardDescription>Set up a new town-os account</CardDescription>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-2">
              <Label htmlFor="username">Username</Label>
              <Input id="username" name="username" required autoFocus />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  name="password"
                  type="password"
                  required
                  minLength="8"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password2">Confirm Password</Label>
                <Input
                  id="password2"
                  name="password2"
                  type="password"
                  required
                  minLength="8"
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="realname">Real Name</Label>
              <Input id="realname" name="realname" />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="phone">Phone</Label>
                <Input id="phone" name="phone" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <Input id="email" name="email" type="email" />
              </div>
            </div>
          </CardContent>
          <CardFooter className="flex flex-col gap-3 pt-6">
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? 'Creating...' : 'Create Account'}
            </Button>
            <p className="text-sm text-muted-foreground">
              Already have an account?{' '}
              <Link to="/" className="underline">
                Sign in
              </Link>
            </p>
          </CardFooter>
        </form>
      </Card>
      </div>
    </div>
  )
}

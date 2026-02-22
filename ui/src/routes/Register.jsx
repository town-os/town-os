import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import getClient from '@/lib/client-instance.js'
import { setToken, setAccount } from '@/lib/auth.js'
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
import { toast } from 'sonner'

export default function Register() {
  useEffect(() => { document.title = 'Town OS - Register' }, [])
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [ready, setReady] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    getClient()
      .ping()
      .then((resp) => {
        if (resp.needs_setup) {
          setReady(true)
        } else {
          navigate('/')
        }
      })
      .catch(() => {})
  }, [navigate])

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

      const resp = await getClient().authenticate(
        form.username.value,
        form.password.value,
      )
      setToken(resp.token)
      setAccount(resp.account)

      navigate('/dashboard')
    } catch (err) {
      toast.error(err.message || 'Failed to create account')
    } finally {
      setLoading(false)
    }
  }

  if (!ready) return null

  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="w-full max-w-md space-y-4">
      <div className="flex justify-center">
        <img src="/512.png" alt="Town OS" className="h-32 w-32" />
      </div>
      <div className="text-center space-y-1">
        <h1 className="text-2xl font-bold tracking-tight">Welcome to Town OS</h1>
        <p className="text-muted-foreground">
          Create an administrator account to get started.
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Create Account</CardTitle>
          <CardDescription>Set up your first administrator account</CardDescription>
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
          <CardFooter className="pt-6">
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? 'Creating...' : 'Create Account'}
            </Button>
          </CardFooter>
        </form>
      </Card>
      </div>
    </div>
  )
}

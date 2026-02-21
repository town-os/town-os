import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import getClient from '@/lib/client-instance.js'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { toast } from 'sonner'

export default function CreateUser() {
  useEffect(() => { document.title = 'Town OS - Create User' }, [])
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  async function handleSubmit(e) {
    e.preventDefault()
    const form = e.target.elements
    if (!form.username.value) {
      toast.error('Username is required')
      return
    }
    if (!form.password.value) {
      toast.error('Password is required')
      return
    }
    if (form.password.value.length < 8) {
      toast.error('Password must be at least 8 characters')
      return
    }
    if (!form.password2.value) {
      toast.error('Password confirmation is required')
      return
    }
    if (form.password.value !== form.password2.value) {
      toast.error('Passwords do not match')
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
        !!form.admin?.checked,
      )
      navigate('/dashboard/users')
    } catch (err) {
      toast.error(err.message || 'Failed to create user')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-md mx-auto mt-8">
      <Card>
        <CardHeader>
          <CardTitle>Create New User</CardTitle>
        </CardHeader>
        <form onSubmit={handleSubmit} noValidate>
          <CardContent className="space-y-4">
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
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="admin"
                name="admin"
                className="rounded"
              />
              <Label htmlFor="admin">Admin privileges</Label>
            </div>
          </CardContent>
          <CardFooter className="flex gap-3 pt-6">
            <Button
              variant="outline"
              type="button"
              onClick={() => navigate('/dashboard/users')}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? 'Creating...' : 'Create User'}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  )
}

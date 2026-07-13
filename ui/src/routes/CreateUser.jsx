import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useI18n } from '@/i18n/I18nContext.jsx'
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
  const { t } = useI18n()
  useEffect(() => { document.title = t('create_user.page_title') }, [t])
  const [loading, setLoading] = useState(false)
  const [wireguard, setWireguard] = useState(false)
  const [networks, setNetworks] = useState([])
  const [selectedNetworks, setSelectedNetworks] = useState([])
  const navigate = useNavigate()

  // Fetch the enrollable networks for the scope selector. The default "home"
  // network has no WireGuard transport, so it is never a valid scope.
  useEffect(() => {
    getClient().listNetworks()
      .then((nets) => setNetworks((nets || []).filter((n) => n.name !== 'home')))
      .catch((err) => console.debug('listNetworks failed:', err))
  }, [])

  function toggleNetwork(name) {
    setSelectedNetworks((prev) =>
      prev.includes(name) ? prev.filter((n) => n !== name) : [...prev, name],
    )
  }

  async function handleSubmit(e) {
    e.preventDefault()
    const form = e.target.elements
    if (!form.username.value) {
      toast.error(t('create_user.error_username_required'))
      return
    }
    if (!form.password.value) {
      toast.error(t('create_user.error_password_required'))
      return
    }
    if (form.password.value.length < 8) {
      toast.error(t('create_user.error_password_min_length'))
      return
    }
    if (!form.password2.value) {
      toast.error(t('create_user.error_confirm_required'))
      return
    }
    if (form.password.value !== form.password2.value) {
      toast.error(t('create_user.error_passwords_mismatch'))
      return
    }
    if (wireguard && selectedNetworks.length === 0) {
      toast.error(t('create_user.error_networks_required'))
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
        // A WireGuard-only account is never an admin; the two are exclusive.
        wireguard ? false : !!form.admin?.checked,
        wireguard,
        wireguard ? selectedNetworks : [],
      )
      navigate('/dashboard/users')
    } catch (err) {
      toast.error(err.message || t('create_user.error_create_failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-md mx-auto mt-8">
      <Card>
        <CardHeader>
          <CardTitle>{t('create_user.card_title')}</CardTitle>
        </CardHeader>
        <form onSubmit={handleSubmit} noValidate>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username">{t('create_user.username_label')}</Label>
              <Input id="username" name="username" required autoFocus />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="password">{t('create_user.password_label')}</Label>
                <Input
                  id="password"
                  name="password"
                  type="password"
                  required
                  minLength="8"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password2">{t('create_user.confirm_password_label')}</Label>
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
              <Label htmlFor="realname">{t('create_user.real_name_label')}</Label>
              <Input id="realname" name="realname" />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="phone">{t('create_user.phone_label')}</Label>
                <Input id="phone" name="phone" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="email">{t('create_user.email_label')}</Label>
                <Input id="email" name="email" type="email" />
              </div>
            </div>
            {!wireguard && (
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="admin"
                  name="admin"
                  className="rounded"
                />
                <Label htmlFor="admin">{t('create_user.admin_label')}</Label>
              </div>
            )}
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="wireguard"
                name="wireguard"
                className="rounded"
                checked={wireguard}
                onChange={(e) => setWireguard(e.target.checked)}
              />
              <Label htmlFor="wireguard">{t('create_user.wireguard_label')}</Label>
            </div>
            {wireguard && (
              <div className="space-y-2">
                <Label>{t('create_user.networks_label')}</Label>
                <p className="text-sm text-muted-foreground">{t('create_user.networks_description')}</p>
                {networks.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{t('create_user.networks_none')}</p>
                ) : (
                  <div className="space-y-1">
                    {networks.map((n) => (
                      <div key={n.name} className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`network-${n.name}`}
                          className="rounded"
                          checked={selectedNetworks.includes(n.name)}
                          onChange={() => toggleNetwork(n.name)}
                        />
                        <Label htmlFor={`network-${n.name}`}>{n.name}</Label>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </CardContent>
          <CardFooter className="flex gap-3 pt-6">
            <Button
              variant="outline"
              type="button"
              onClick={() => navigate('/dashboard/users')}
            >
              {t('create_user.cancel_btn')}
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? t('create_user.submit_loading') : t('create_user.submit')}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  )
}

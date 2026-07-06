import { useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { useRequireAuth, usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Plus, Trash2, Users } from 'lucide-react'

const DEFAULT_NETWORK = 'home'

export default function Networks() {
  const { t } = useI18n()
  const account = useRequireAuth()
  const isAdmin = !!account?.admin

  const [refreshKey, setRefreshKey] = useState(0)
  const [pending, setPending] = useState({}) // name -> in-flight bool
  const [createOpen, setCreateOpen] = useState(false)
  const [newName, setNewName] = useState('')
  const [newTld, setNewTld] = useState('')
  const [removeTarget, setRemoveTarget] = useState(null)
  const [peersTarget, setPeersTarget] = useState(null)

  const [networks] = usePolling(
    () => getClient().listNetworks().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  async function toggleEnabled(row, enabled) {
    setPending((p) => ({ ...p, [row.name]: true }))
    try {
      if (enabled) await getClient().enableNetwork(row.name)
      else await getClient().disableNetwork(row.name)
      toast.success(enabled ? t('networks.toast_enabled', { name: row.name }) : t('networks.toast_disabled', { name: row.name }))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setPending((p) => ({ ...p, [row.name]: false }))
    }
  }

  async function createNetwork() {
    try {
      await getClient().createNetwork(newName.trim(), newTld.trim())
      toast.success(t('networks.toast_created', { name: newName.trim() }))
      setCreateOpen(false)
      setNewName('')
      setNewTld('')
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function removeNetwork() {
    if (!removeTarget) return
    try {
      await getClient().removeNetwork(removeTarget.name)
      toast.success(t('networks.toast_removed', { name: removeTarget.name }))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setRemoveTarget(null)
    }
  }

  const columns = [
    {
      key: 'name',
      label: t('networks.col_name'),
      transform: (v, row) => (
        <div>
          <div className="font-medium text-sm">{v}</div>
          <div className="text-xs text-muted-foreground font-mono">{row.interface}</div>
        </div>
      ),
    },
    { key: 'tld', label: t('networks.col_tld'), transform: (v) => <span className="font-mono text-sm">.{v}</span> },
    { key: 'subnet', label: t('networks.col_subnet'), transform: (v) => <span className="font-mono text-sm">{v}</span> },
    { key: 'peer_count', label: t('networks.col_peers'), transform: (v) => <Badge variant="secondary">{v}</Badge> },
    {
      key: 'enabled',
      label: t('networks.col_remote'),
      sortable: false,
      transform: (v, row) => (
        <Switch
          checked={!!v}
          disabled={!isAdmin || pending[row.name]}
          onCheckedChange={(next) => toggleEnabled(row, next)}
          aria-label={t('networks.col_remote')}
        />
      ),
    },
    {
      key: 'actions',
      label: '',
      sortable: false,
      transform: (_v, row) => (
        <div className="flex items-center gap-2 justify-end">
          <Button variant="ghost" size="sm" onClick={() => setPeersTarget(row)}>
            <Users className="h-4 w-4 mr-1" />
            {t('networks.peers')}
          </Button>
          {row.name !== DEFAULT_NETWORK && (
            <Button
              variant="ghost"
              size="sm"
              disabled={!isAdmin}
              onClick={() => setRemoveTarget(row)}
              aria-label={t('networks.remove')}
            >
              <Trash2 className="h-4 w-4 text-destructive" />
            </Button>
          )}
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t('networks.title')}</h1>
          <p className="text-sm text-muted-foreground">{t('networks.description')}</p>
        </div>
        {isAdmin && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4 mr-2" />
            {t('networks.create')}
          </Button>
        )}
      </div>

      <DataTable data={networks} columns={columns} entryKey="name" />

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('networks.create')}</DialogTitle>
            <DialogDescription>{t('networks.create_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="net-name">{t('networks.field_name')}</Label>
              <Input
                id="net-name"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="office"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="net-tld">{t('networks.field_tld')}</Label>
              <Input
                id="net-tld"
                value={newTld}
                onChange={(e) => setNewTld(e.target.value)}
                placeholder={newName || 'office'}
              />
              <p className="text-xs text-muted-foreground">{t('networks.field_tld_help')}</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={createNetwork} disabled={!newName.trim()}>
              {t('networks.create')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!removeTarget}
        title={t('networks.remove')}
        confirmLabel={t('networks.remove')}
        variant="destructive"
        onConfirm={removeNetwork}
        onCancel={() => setRemoveTarget(null)}
      >
        {removeTarget ? t('networks.remove_confirm', { name: removeTarget.name }) : ''}
      </ConfirmDialog>

      {peersTarget && (
        <PeersDialog
          network={peersTarget}
          isAdmin={isAdmin}
          onClose={() => setPeersTarget(null)}
        />
      )}
    </div>
  )
}

function PeersDialog({ network, isAdmin, onClose }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [peerName, setPeerName] = useState('')
  const [generatedConfig, setGeneratedConfig] = useState('')

  const [peers] = usePolling(
    () => getClient().listNetworkPeers(network.name).catch(() => []),
    [],
    [refreshKey],
    0,
  )

  async function addPeer() {
    try {
      const result = await getClient().addNetworkPeer(network.name, peerName.trim())
      setGeneratedConfig(result.config || '')
      setPeerName('')
      setRefreshKey((k) => k + 1)
      toast.success(t('networks.toast_peer_added'))
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function removePeer(publicKey) {
    try {
      await getClient().removeNetworkPeer(network.name, publicKey)
      setRefreshKey((k) => k + 1)
      toast.success(t('networks.toast_peer_removed'))
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  function copyConfig() {
    navigator.clipboard.writeText(generatedConfig).then(
      () => toast.success(t('networks.toast_config_copied')),
      () => toast.error(t('networks.toast_copy_failed')),
    )
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('networks.peers_title', { name: network.name })}</DialogTitle>
          <DialogDescription>{t('networks.peers_description')}</DialogDescription>
        </DialogHeader>

        {isAdmin && (
          <div className="flex items-end gap-2">
            <div className="flex-1 space-y-2">
              <Label htmlFor="peer-name">{t('networks.peer_name')}</Label>
              <Input
                id="peer-name"
                value={peerName}
                onChange={(e) => setPeerName(e.target.value)}
                placeholder="laptop"
              />
            </div>
            <Button onClick={addPeer}>
              <Plus className="h-4 w-4 mr-2" />
              {t('networks.add_peer')}
            </Button>
          </div>
        )}

        {generatedConfig && (
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>{t('networks.device_config')}</Label>
              <Button variant="outline" size="sm" onClick={copyConfig}>
                {t('networks.copy')}
              </Button>
            </div>
            <pre className="text-xs bg-muted p-3 rounded-md overflow-x-auto whitespace-pre-wrap break-all">{generatedConfig}</pre>
            <p className="text-xs text-muted-foreground">{t('networks.device_config_help')}</p>
          </div>
        )}

        <div className="space-y-2">
          {(peers || []).length === 0 && (
            <p className="text-sm text-muted-foreground">{t('networks.no_peers')}</p>
          )}
          {(peers || []).map((p) => (
            <div key={p.public_key} className="flex items-center justify-between border rounded-md p-2">
              <div>
                <div className="text-sm font-medium">{p.name || t('networks.unnamed_peer')}</div>
                <div className="text-xs text-muted-foreground font-mono">{p.allowed_ip}</div>
              </div>
              {isAdmin && (
                <Button variant="ghost" size="sm" onClick={() => removePeer(p.public_key)} aria-label={t('networks.remove_peer')}>
                  <Trash2 className="h-4 w-4 text-destructive" />
                </Button>
              )}
            </div>
          ))}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('networks.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

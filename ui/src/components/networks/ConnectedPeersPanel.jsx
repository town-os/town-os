import { useState, useMemo } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import { formatBytes, formatAgo, PAGE_SIZE } from '@/lib/utils.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import { PlugZap } from 'lucide-react'

/**
 * The connected-peers panel: every device enrolled on a WireGuard network,
 * which account enrolled it, its overlay address, where it is dialing from, and
 * whether it is currently handshaking — plus a disconnect action.
 *
 * The default ("home") network never appears: the server excludes it because it
 * is a DNS-only scope with no overlay, so it has no peers by construction.
 *
 * @param {{ isAdmin: boolean }} props
 */
export default function ConnectedPeersPanel({ isAdmin }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [disconnectTarget, setDisconnectTarget] = useState(null)
  const [disconnecting, setDisconnecting] = useState(false)

  const [peers] = usePolling(
    () => getClient().listConnectedPeers().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  // A peer's identity is (network, public_key), not the key alone: the same
  // device can be enrolled on two networks, and DataTable uses entryKey as the
  // React row key. Keying on public_key alone would collide those two rows.
  const rows = useMemo(
    () => (peers || []).map((p) => ({ ...p, id: `${p.network}/${p.public_key}` })),
    [peers],
  )

  async function disconnectPeer() {
    if (!disconnectTarget) return
    setDisconnecting(true)
    try {
      await getClient().removeNetworkPeer(disconnectTarget.network, disconnectTarget.public_key)
      toast.success(
        t('networks.toast_peer_disconnected', {
          name: disconnectTarget.name || t('networks.unnamed_peer'),
        }),
      )
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setDisconnecting(false)
      setDisconnectTarget(null)
    }
  }

  const columns = [
    {
      key: 'name',
      label: t('networks.col_peer'),
      transform: (v, row) => (
        <div>
          <div className="font-medium text-sm">{v || t('networks.unnamed_peer')}</div>
          <div className="text-xs text-muted-foreground font-mono truncate max-w-[16rem]">
            {row.public_key}
          </div>
        </div>
      ),
    },
    {
      key: 'network',
      label: t('networks.col_network'),
      transform: (v, row) => (
        <div>
          <div className="text-sm">{v}</div>
          <div className="text-xs text-muted-foreground font-mono">.{row.tld}</div>
        </div>
      ),
    },
    {
      key: 'account',
      label: t('networks.col_account'),
      transform: (v) =>
        v ? (
          <span className="text-sm">{v}</span>
        ) : (
          <span className="text-sm text-muted-foreground">{t('networks.no_account')}</span>
        ),
    },
    {
      key: 'allowed_ip',
      label: t('networks.col_overlay_ip'),
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'endpoint',
      label: t('networks.col_endpoint'),
      transform: (v) =>
        v ? (
          <span className="font-mono text-sm">{v}</span>
        ) : (
          <span className="text-sm text-muted-foreground">—</span>
        ),
    },
    {
      key: 'connected',
      label: t('networks.col_status'),
      transform: (v, row) => {
        const ago = formatAgo(row.last_handshake)
        return (
          <div className="flex flex-col gap-1 items-start">
            <Badge variant={v ? 'default' : 'secondary'}>
              {v ? t('networks.status_connected') : t('networks.status_idle')}
            </Badge>
            <span className="text-xs text-muted-foreground">
              {ago ? t('networks.handshake_ago', { ago }) : t('networks.handshake_never')}
            </span>
          </div>
        )
      },
    },
    {
      key: 'rx_bytes',
      label: t('networks.col_transfer'),
      transform: (v, row) => (
        <div className="text-xs text-muted-foreground font-mono whitespace-nowrap">
          <div>↓ {formatBytes(v || 0)}</div>
          <div>↑ {formatBytes(row.tx_bytes || 0)}</div>
        </div>
      ),
    },
    {
      key: 'expires_at',
      label: t('networks.col_expires'),
      transform: (v) => {
        if (!v) return <span className="text-xs text-muted-foreground">{t('networks.expires_never')}</span>
        const then = new Date(v)
        const mins = Math.round((then.getTime() - Date.now()) / 60000)
        if (mins <= 0) {
          return <span className="text-xs text-destructive">{t('networks.expires_lapsed')}</span>
        }
        return <span className="text-xs text-muted-foreground">{t('networks.expires_in', { mins })}</span>
      },
    },
    {
      key: 'actions',
      label: '',
      sortable: false,
      transform: (_v, row) => (
        <div className="flex justify-end">
          <Button
            variant="ghost"
            size="sm"
            disabled={!isAdmin}
            onClick={() => setDisconnectTarget(row)}
            aria-label={t('networks.disconnect')}
          >
            <PlugZap className="h-4 w-4 mr-1 text-destructive" />
            {t('networks.disconnect')}
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-3">
      <div>
        <h2 className="text-lg font-semibold">{t('networks.peers_panel_title')}</h2>
        <p className="text-sm text-muted-foreground">{t('networks.peers_panel_description')}</p>
      </div>

      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground border rounded-md p-4">
          {t('networks.no_connected_peers')}
        </p>
      ) : (
        <DataTable
          data={rows}
          columns={columns}
          entryKey="id"
          page={page}
          setPage={setPage}
          pageSize={PAGE_SIZE}
        />
      )}

      <ConfirmDialog
        open={!!disconnectTarget}
        title={t('networks.disconnect')}
        confirmLabel={t('networks.disconnect')}
        variant="destructive"
        loading={disconnecting}
        onConfirm={disconnectPeer}
        onCancel={() => setDisconnectTarget(null)}
      >
        {disconnectTarget
          ? t('networks.disconnect_confirm', {
              name: disconnectTarget.name || t('networks.unnamed_peer'),
              network: disconnectTarget.network,
            })
          : ''}
      </ConfirmDialog>
    </div>
  )
}

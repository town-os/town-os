import { useState } from 'react'
import { toast } from 'sonner'

import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { useI18n } from '@/i18n/I18nContext.jsx'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'

/**
 * A partition's published links: the /f/<token> URLs its plain-HTTP view
 * serves.
 *
 * Read and withdraw only. Publishing a file is done from wherever the file is —
 * a client with the share mounted, or gfeh's own CLI — because it needs a path
 * inside the partition, and this page has no file browser to pick one from.
 */
export default function LinksTab({ network, isAdmin }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [withdrawTarget, setWithdrawTarget] = useState(null)
  const [busy, setBusy] = useState(false)

  const [exposures] = usePolling(
    () => (network ? getClient().listGfehExposures(network).catch(() => []) : Promise.resolve([])),
    [],
    [network, refreshKey],
    30000,
  )

  async function handleWithdraw() {
    setBusy(true)
    try {
      await getClient().withdrawGfehExposure(network, withdrawTarget.token)
      toast.success(t('objects.toast_link_withdrawn'))
      setWithdrawTarget(null)
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setBusy(false)
    }
  }

  const columns = [
    {
      key: 'token',
      label: t('objects.col_token'),
      transform: (v) => <code className="font-mono text-xs">{v}</code>,
      clip: true,
    },
    {
      key: 'path',
      label: t('objects.col_path'),
      sortable: true,
      transform: (v) => <code className="font-mono text-xs">{v}</code>,
    },
    { key: 'filename', label: t('objects.col_filename') },
    {
      key: 'actions',
      label: '',
      transform: (_v, row) =>
        isAdmin ? (
          <Button variant="ghost" size="sm" onClick={() => setWithdrawTarget(row)}>
            {t('objects.withdraw_link')}
          </Button>
        ) : null,
    },
  ]

  return (
    <div className="space-y-4">
      {exposures.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('objects.no_links')}</p>
      ) : (
        <DataTable data={exposures} columns={columns} entryKey="token" />
      )}

      <ConfirmDialog
        open={!!withdrawTarget}
        title={t('objects.withdraw_link')}
        confirmLabel={t('objects.withdraw_link')}
        loading={busy}
        onConfirm={handleWithdraw}
        onCancel={() => setWithdrawTarget(null)}
      >
        {t('objects.withdraw_link_confirm')}
      </ConfirmDialog>
    </div>
  )
}

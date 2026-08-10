import { useState } from 'react'
import { toast } from 'sonner'

import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { copyToClipboard } from '@/lib/utils.js'
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
export default function LinksTab({ network, canManage }) {
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

  async function handleCopy(url) {
    try {
      await copyToClipboard(url)
      toast.success(t('dashboard.copied_label'))
    } catch {
      toast.error(t('networks.toast_copy_failed'))
    }
  }

  const columns = [
    {
      // The link, not the token. A published link exists to be sent to
      // somebody, and a bare token is not something anybody can act on -- it
      // left the operator to work out that the serving name is the partition's
      // *http* view, qualified under that partition's network TLD, on :443, at
      // /f/<token>. The server composes it now (GfehExposureView.URL) from the
      // same collector that names the ingress vhost, so what is rendered here
      // is by construction a URL the ingress routes.
      key: 'url',
      label: t('objects.col_link'),
      transform: (v, row) => {
        if (!v) {
          // No HTTP view is being served, so nothing answers this token. The
          // token is still shown, because it is the row's identity and the
          // handle the withdraw action uses.
          return (
            <span title={t('objects.link_unavailable')}>
              <code className="font-mono text-xs text-muted-foreground">{row.token}</code>
            </span>
          )
        }
        if (!row.enabled) {
          // Deliberately not an anchor: the exposure exists but is disabled, so
          // the URL is what it *would* be. Rendering something clickable that
          // answers 404 is worse than rendering it plainly.
          return (
            <span className="font-mono text-xs text-muted-foreground line-through" title={t('objects.link_disabled')}>
              {v}
            </span>
          )
        }
        return (
          <div className="flex min-w-0 items-center gap-2">
            <a
              href={v}
              target="_blank"
              rel="noreferrer"
              className="truncate font-mono text-xs underline underline-offset-2"
            >
              {v}
            </a>
            <Button
              variant="ghost"
              size="sm"
              title={t('dashboard.copy_btn_label')}
              aria-label={t('dashboard.copy_btn_label')}
              onClick={() => handleCopy(v)}
            >
              {t('networks.copy')}
            </Button>
          </div>
        )
      },
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
        canManage ? (
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

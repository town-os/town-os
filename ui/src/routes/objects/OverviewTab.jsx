import { useI18n } from '@/i18n/I18nContext.jsx'
import DataTable from '@/components/DataTable.jsx'

/**
 * The addresses a partition publishes.
 *
 * The "reachable via" column is the point of this table. Four of the five views
 * are HTTP and sit behind the shared ingress on :443, so a user browses to the
 * name; SMB is not HTTP, cannot sit behind an HTTP router, and is dialled
 * directly on its own port. Telling somebody to browse to an SMB address would
 * hand them a connection that completes and then does nothing.
 */
export default function OverviewTab({ partition }) {
  const { t } = useI18n()

  if (!partition) return null

  const names = partition.names || []
  if (names.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('objects.no_names')}</p>
  }

  const columns = [
    { key: 'view', label: t('objects.col_view'), sortable: true },
    {
      key: 'fqdn',
      label: t('objects.col_address'),
      sortable: true,
      transform: (v) => <code className="font-mono text-xs">{v}</code>,
    },
    { key: 'port', label: t('objects.col_port'), sortable: true, width: '100px' },
    {
      key: 'http',
      label: t('objects.col_reachable'),
      transform: (v) => (
        <span className="text-muted-foreground">
          {v ? t('objects.via_ingress') : t('objects.via_port')}
        </span>
      ),
    },
  ]

  return <DataTable data={names} columns={columns} entryKey="view" />
}

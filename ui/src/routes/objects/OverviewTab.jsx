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
    // Two different facts, and they used to share one sentence.
    //
    // A partition whose daemon is not answering publishes no addresses *because
    // it is down*, which is a thing to go and look at; a running partition with
    // nothing published is a configuration. Reporting both as "publishes no
    // addresses" is most of why "object storage isn't working" was as far as
    // anybody could get in describing it. Say which one it is, and say that the
    // box is already retrying, so the answer is not "reboot it".
    return (
      <p className="text-sm text-muted-foreground">
        {partition.running ? t('objects.no_names') : t('objects.stopped_no_names')}
      </p>
    )
  }

  const columns = [
    { key: 'view', label: t('objects.col_view'), sortable: true },
    {
      key: 'fqdn',
      label: t('objects.col_address'),
      sortable: true,
      // A link for the views the ingress fronts, plain text for the ones it
      // does not. The whole table already turns on that distinction (see the
      // "reachable via" column) and an address somebody can click is the
      // difference between being told the name and being able to use it --
      // including the `index` row, which is the browsable page listing all of
      // these. SMB stays plain: https:// on an SMB address is a handshake that
      // completes and then does nothing.
      transform: (v, row) =>
        row.http ? (
          <a
            href={`https://${v}`}
            target="_blank"
            rel="noreferrer"
            className="font-mono text-xs underline underline-offset-2"
          >
            {v}
          </a>
        ) : (
          <code className="font-mono text-xs">{v}</code>
        ),
    },
    {
      key: 'port',
      label: t('objects.col_port'),
      sortable: true,
      width: '100px',
      // Blank for an HTTP view, and that is a correction rather than a
      // simplification. The port gfeh reports for those is the container-side
      // port the ingress proxies to; it is not reachable from anywhere a reader
      // of this table sits, and printing 9000 in a column headed "Port" beside
      // "Ingress (HTTPS)" invites exactly one thing -- somebody dialling
      // s3.gfeh.home:9000 and concluding object storage is broken. The ingress
      // answers these on :443, which is implied by the address being a link.
      // SMB keeps its number: there it is the real host port, and dialling it
      // is the only way in.
      transform: (v, row) => (row.http ? <span className="text-muted-foreground">—</span> : v),
    },
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

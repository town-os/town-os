import { Fragment, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { formatQuotaText, formatQuota } from '@/lib/storage-utils.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Pencil, ChevronRight, ChevronDown, Package, Upload, Download, Trash2 } from 'lucide-react'

export default function PackageVolumeTree({ packageGroups, onModifyVolume, onDownloadVolume, onUploadVolume, onDeleteVolume }) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState({})

  const totalVolumes = packageGroups.reduce((sum, g) => sum + g.volumes.length, 0)

  if (packageGroups.length === 0) return null

  function togglePkg(pkg) {
    setExpanded((prev) => ({ ...prev, [pkg]: !prev[pkg] }))
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Package className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-lg font-semibold">{t('storage.package_volumes_title')}</h3>
        <span className="text-sm text-muted-foreground ml-auto">
          {totalVolumes} volume{totalVolumes !== 1 ? 's' : ''}
        </span>
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead style={{ width: '35%' }}>{t('storage.col_name')}</TableHead>
              <TableHead style={{ width: '15%' }}>{t('storage.col_quota')}</TableHead>
              <TableHead style={{ width: '15%' }}>{t('storage.col_state')}</TableHead>
              <TableHead className="text-right" style={{ width: '25%' }}>{t('storage.col_actions')}</TableHead>
              <TableHead className="text-center" style={{ width: '10%' }}>{t('storage.col_pkg_delete')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {packageGroups.map((group) => {
              const isExpanded = !!expanded[group.package]
              const totalQ = group.volumes.reduce((sum, v) => sum + Number(v.quota || 0), 0)
              const states = [...new Set(group.volumes.map((v) => v.state))]
              return (
                <Fragment key={group.package}>
                  <TableRow
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => togglePkg(group.package)}
                  >
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-1">
                        {isExpanded
                          ? <ChevronDown className="h-4 w-4 text-muted-foreground" />
                          : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
                        <span className="font-mono text-sm">{group.package}</span>
                        <span className="text-xs text-muted-foreground ml-2">
                          ({group.volumes.length} volume{group.volumes.length !== 1 ? 's' : ''})
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {totalQ > 0 ? formatQuotaText(totalQ) : ''}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {states.map((s) => (
                          <Badge key={s} variant={s === 'installed' ? 'default' : 'secondary'}>{s}</Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell />
                    <TableCell />
                  </TableRow>
                  {isExpanded && group.volumes.map((vol) => (
                    <TableRow key={vol.internal_name}>
                      <TableCell>
                        <span className="font-mono text-sm pl-8 text-muted-foreground">
                          {vol.name}
                        </span>
                      </TableCell>
                      <TableCell>{formatQuota(vol.quota)}</TableCell>
                      <TableCell>
                        <Badge variant={vol.state === 'installed' ? 'default' : 'secondary'}>
                          {vol.state}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            title={t('storage.download_archive_label')}
                            aria-label={t('storage.download_archive_label')}
                            onClick={(e) => {
                              e.stopPropagation()
                              onDownloadVolume(vol)
                            }}
                          >
                            <Download className="h-3 w-3" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            title={t('storage.upload_archive_label')}
                            aria-label={t('storage.upload_archive_label')}
                            onClick={(e) => {
                              e.stopPropagation()
                              onUploadVolume(vol)
                            }}
                          >
                            <Upload className="h-3 w-3" />
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={(e) => {
                              e.stopPropagation()
                              onModifyVolume(vol)
                            }}
                          >
                            <Pencil className="h-3 w-3 mr-1" />
                            {t('storage.modify_btn')}
                          </Button>
                        </div>
                      </TableCell>
                      <TableCell className="text-center">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-destructive hover:text-destructive"
                          onClick={(e) => {
                            e.stopPropagation()
                            onDeleteVolume(vol.internal_name)
                          }}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </Fragment>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

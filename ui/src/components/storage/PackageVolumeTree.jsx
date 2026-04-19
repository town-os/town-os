import { Fragment, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { formatQuotaText, formatQuota } from '@/lib/storage-utils.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Pencil, ChevronRight, ChevronDown, Package, Upload, Download, Trash2 } from 'lucide-react'

// splitVolumeName splits the API's "version/..." path into the version
// component and the rest of the volume name. Back-end code always emits
// paths shaped as "<version>/<volName[/subpath...]>"; the split is
// deliberately on the first slash so nested volume paths (rare, legacy
// installs) stay intact on the leaf row.
function splitVolumeName(name) {
  const idx = name.indexOf('/')
  if (idx < 0) return { version: name, rest: '' }
  return { version: name.slice(0, idx), rest: name.slice(idx + 1) }
}

// groupByVersion rearranges a group's flat volume list into a nested
// version → volume map so the tree component can render three levels
// instead of two.
function groupByVersion(volumes) {
  const byVersion = new Map()
  for (const vol of volumes) {
    const { version, rest } = splitVolumeName(vol.name)
    if (!byVersion.has(version)) byVersion.set(version, [])
    byVersion.get(version).push({ ...vol, displayName: rest || vol.name })
  }
  // Sort versions so the UI is deterministic (roughly newest-last by
  // string order; the API already provides stable within-version order).
  const versions = [...byVersion.keys()].sort()
  return versions.map((version) => ({
    version,
    volumes: byVersion.get(version).slice().sort((a, b) => a.displayName.localeCompare(b.displayName)),
  }))
}

export default function PackageVolumeTree({
  packageGroups,
  onModifyVolume,
  onDownloadVolume,
  onUploadVolume,
  onDeleteVolume,
  onDeletePackage,
  onDeleteVersion,
}) {
  const { t } = useI18n()
  const [expandedPkg, setExpandedPkg] = useState({})
  const [expandedVer, setExpandedVer] = useState({})

  const totalVolumes = packageGroups.reduce((sum, g) => sum + g.volumes.length, 0)

  if (packageGroups.length === 0) return null

  function togglePkg(key) {
    setExpandedPkg((prev) => ({ ...prev, [key]: !prev[key] }))
  }
  function toggleVer(key) {
    setExpandedVer((prev) => ({ ...prev, [key]: !prev[key] }))
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
              <TableHead style={{ width: '40%' }}>{t('storage.col_name')}</TableHead>
              <TableHead style={{ width: '15%' }}>{t('storage.col_quota')}</TableHead>
              <TableHead style={{ width: '15%' }}>{t('storage.col_state')}</TableHead>
              <TableHead className="text-right" style={{ width: '20%' }}>{t('storage.col_actions')}</TableHead>
              <TableHead className="text-center" style={{ width: '10%' }}>{t('storage.col_pkg_delete')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {packageGroups.map((group) => {
              const pkgKey = `${group.repo}/${group.package}`
              const isPkgExpanded = !!expandedPkg[pkgKey]
              const versions = groupByVersion(group.volumes)
              const totalQ = group.volumes.reduce((sum, v) => sum + Number(v.quota || 0), 0)
              const states = [...new Set(group.volumes.map((v) => v.state))]
              return (
                <Fragment key={pkgKey}>
                  <TableRow
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => togglePkg(pkgKey)}
                  >
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-1">
                        {isPkgExpanded
                          ? <ChevronDown className="h-4 w-4 text-muted-foreground" />
                          : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
                        <Package className="h-4 w-4 text-muted-foreground" />
                        <span className="font-mono text-sm">{group.package}</span>
                        <span className="text-xs text-muted-foreground ml-2">
                          ({versions.length} {versions.length === 1 ? 'version' : 'versions'}, {group.volumes.length} {group.volumes.length === 1 ? 'volume' : 'volumes'})
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
                    {/* Non-leaf: no modify/upload/download */}
                    <TableCell />
                    <TableCell className="text-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-destructive hover:text-destructive"
                        aria-label={t('storage.col_pkg_delete')}
                        onClick={(e) => {
                          e.stopPropagation()
                          onDeletePackage && onDeletePackage(group)
                        }}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </TableCell>
                  </TableRow>
                  {isPkgExpanded && versions.map(({ version, volumes }) => {
                    const verKey = `${pkgKey}@${version}`
                    const isVerExpanded = !!expandedVer[verKey]
                    const verQ = volumes.reduce((sum, v) => sum + Number(v.quota || 0), 0)
                    const verStates = [...new Set(volumes.map((v) => v.state))]
                    return (
                      <Fragment key={verKey}>
                        <TableRow
                          className="cursor-pointer hover:bg-muted/50"
                          onClick={() => toggleVer(verKey)}
                        >
                          <TableCell>
                            <div className="flex items-center gap-1 pl-6">
                              {isVerExpanded
                                ? <ChevronDown className="h-4 w-4 text-muted-foreground" />
                                : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
                              <span className="font-mono text-sm">
                                {t('storage.version_row_label', { version })}
                              </span>
                              <span className="text-xs text-muted-foreground ml-2">
                                ({volumes.length} {volumes.length === 1 ? 'volume' : 'volumes'})
                              </span>
                            </div>
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {verQ > 0 ? formatQuotaText(verQ) : ''}
                          </TableCell>
                          <TableCell>
                            <div className="flex gap-1">
                              {verStates.map((s) => (
                                <Badge key={s} variant={s === 'installed' ? 'default' : 'secondary'}>{s}</Badge>
                              ))}
                            </div>
                          </TableCell>
                          <TableCell />
                          <TableCell className="text-center">
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-destructive hover:text-destructive"
                              aria-label={t('storage.col_pkg_delete')}
                              onClick={(e) => {
                                e.stopPropagation()
                                onDeleteVersion && onDeleteVersion(group, version)
                              }}
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          </TableCell>
                        </TableRow>
                        {isVerExpanded && volumes.map((vol) => (
                          <TableRow key={vol.internal_name}>
                            <TableCell>
                              <span className="font-mono text-sm pl-14 text-muted-foreground">
                                {vol.displayName}
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
                </Fragment>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

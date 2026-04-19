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

// pkgKeyFor uniquely identifies a package group across the tree. Repo is
// part of the key because two repos may ship a package with the same
// name, and effective_name is the flat --dep-- form so that a parent and
// its dep never collide (their pretty-form names differ anyway).
function pkgKeyFor(group) {
  return `${group.repo}/${group.effective_name || group.package}`
}

// buildPackageTree folds the flat list of PackageVolumeGroup entries
// returned by the backend into a parent/child forest using the
// effective_name chain: "gitea--dep--postgres" nests under "gitea", and
// "gitea--dep--postgres--dep--backup" nests under "gitea--dep--postgres".
// If a parent is missing from the response the orphan is promoted to a
// root so its volumes are never hidden — partial responses are unusual
// but the UI must still surface them.
function buildPackageTree(packageGroups) {
  const byKey = new Map()
  for (const g of packageGroups) byKey.set(pkgKeyFor(g), { group: g, children: [] })

  const roots = []
  for (const node of byKey.values()) {
    const eff = node.group.effective_name || node.group.package
    const lastDep = eff.lastIndexOf('--dep--')
    if (lastDep < 0) {
      roots.push(node)
      continue
    }
    const parentKey = `${node.group.repo}/${eff.slice(0, lastDep)}`
    const parent = byKey.get(parentKey)
    if (parent) parent.children.push(node)
    else roots.push(node)
  }

  const cmp = (a, b) => a.group.package.localeCompare(b.group.package)
  for (const n of byKey.values()) n.children.sort(cmp)
  roots.sort(cmp)
  return roots
}

// countDescendants counts nested sub-package nodes (not volumes). The
// parent row uses this to advertise "(N sub-packages)" so operators know
// there is more content hidden under a collapsed root without needing to
// expand it.
function countDescendants(node) {
  if (node.children.length === 0) return 0
  let n = node.children.length
  for (const c of node.children) n += countDescendants(c)
  return n
}

// PackageNode renders one package row plus, when expanded, its own
// version → volume cascade AND any child package nodes. The cascade is
// identical to the pre-hierarchy behaviour; sub-packages are simply
// rendered after the parent's versions at one additional depth level,
// so each child is itself a collapsible sub-tree.
function PackageNode({
  node,
  depth,
  expandedPkg,
  expandedVer,
  togglePkg,
  toggleVer,
  onModifyVolume,
  onDownloadVolume,
  onUploadVolume,
  onDeleteVolume,
  onDeletePackage,
  onDeleteVersion,
  t,
}) {
  const { group, children } = node
  const pkgKey = pkgKeyFor(group)
  const isPkgExpanded = !!expandedPkg[pkgKey]
  const versions = groupByVersion(group.volumes)
  const totalQ = group.volumes.reduce((sum, v) => sum + Number(v.quota || 0), 0)
  const states = [...new Set(group.volumes.map((v) => v.state))]
  const descCount = countDescendants(node)
  const pkgPad = 8 + depth * 24
  const verPad = pkgPad + 24
  const leafPad = pkgPad + 56

  const hasChildren = children.length > 0
  const hasVersions = versions.length > 0
  const summaryParts = []
  if (hasVersions) {
    summaryParts.push(`${versions.length} ${versions.length === 1 ? 'version' : 'versions'}`)
    summaryParts.push(`${group.volumes.length} ${group.volumes.length === 1 ? 'volume' : 'volumes'}`)
  }
  if (hasChildren) {
    summaryParts.push(`${descCount} sub-package${descCount === 1 ? '' : 's'}`)
  }

  return (
    <Fragment>
      <TableRow
        className="cursor-pointer hover:bg-muted/50"
        onClick={() => togglePkg(pkgKey)}
        data-testid={`pkg-tree-row-${pkgKey}`}
      >
        <TableCell className="font-medium">
          <div className="flex items-center gap-1" style={{ paddingLeft: pkgPad }}>
            {isPkgExpanded
              ? <ChevronDown className="h-4 w-4 text-muted-foreground" />
              : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
            <Package className="h-4 w-4 text-muted-foreground" />
            <span className="font-mono text-sm">{group.package}</span>
            {summaryParts.length > 0 && (
              <span className="text-xs text-muted-foreground ml-2">
                ({summaryParts.join(', ')})
              </span>
            )}
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
                <div className="flex items-center gap-1" style={{ paddingLeft: verPad }}>
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
                  <span className="font-mono text-sm text-muted-foreground" style={{ paddingLeft: leafPad }}>
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
      {isPkgExpanded && children.map((child) => (
        <PackageNode
          key={pkgKeyFor(child.group)}
          node={child}
          depth={depth + 1}
          expandedPkg={expandedPkg}
          expandedVer={expandedVer}
          togglePkg={togglePkg}
          toggleVer={toggleVer}
          onModifyVolume={onModifyVolume}
          onDownloadVolume={onDownloadVolume}
          onUploadVolume={onUploadVolume}
          onDeleteVolume={onDeleteVolume}
          onDeletePackage={onDeletePackage}
          onDeleteVersion={onDeleteVersion}
          t={t}
        />
      ))}
    </Fragment>
  )
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

  const roots = buildPackageTree(packageGroups)

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
            {roots.map((node) => (
              <PackageNode
                key={pkgKeyFor(node.group)}
                node={node}
                depth={0}
                expandedPkg={expandedPkg}
                expandedVer={expandedVer}
                togglePkg={togglePkg}
                toggleVer={toggleVer}
                onModifyVolume={onModifyVolume}
                onDownloadVolume={onDownloadVolume}
                onUploadVolume={onUploadVolume}
                onDeleteVolume={onDeleteVolume}
                onDeletePackage={onDeletePackage}
                onDeleteVersion={onDeleteVersion}
                t={t}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

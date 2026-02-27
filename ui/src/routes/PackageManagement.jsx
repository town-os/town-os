import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Plus, Trash2, FolderGit2, RefreshCw, AlertCircle, CheckCircle2, Info, ArrowUpCircle, ArrowUp, ArrowDown, ChevronRight, ChevronDown, X, Star, Download } from 'lucide-react'
import { Separator } from '@/components/ui/separator'

export default function PackageManagement() {
  useEffect(() => { document.title = 'Town OS - Packages' }, [])
  const [refreshKey, setRefreshKey] = useState(0)

  // Package state
  const [uninstallConfirm, setUninstallConfirm] = useState(null)
  const [purgeVolumes, setPurgeVolumes] = useState(false)
  const [clearedCachedFields, setClearedCachedFields] = useState({})
  const [questionsDialog, setQuestionsDialog] = useState({ open: false })
  const [infoDialog, setInfoDialog] = useState({ open: false })
  const [versionSelectDialog, setVersionSelectDialog] = useState({ open: false })
  const [volumeReuseDialog, setVolumeReuseDialog] = useState({ open: false })
  const [previewDialog, setPreviewDialog] = useState({ open: false })

  // Repository state
  const [repoDialog, setRepoDialog] = useState(false)
  const [deleteRepoConfirm, setDeleteRepoConfirm] = useState(null)

  // Group by repository toggle
  const [groupByRepo, setGroupByRepo] = useState(false)
  const [repoExpanded, setRepoExpanded] = useState({})
  const [showInstalledOnly, setShowInstalledOnly] = useState(false)

  // Sort state for packages tab
  const [pkgSortKey, setPkgSortKey] = useState('name')
  const [pkgSortDirection, setPkgSortDirection] = useState('asc')

  // Sort state for repositories tab (empty = natural insertion order)
  const [repoSortKey, setRepoSortKey] = useState('')
  const [repoSortDirection, setRepoSortDirection] = useState('')

  const PAGE_SIZE = 20

  const [pkgPage, setPkgPage] = useState(0)
  const [repoPage, setRepoPage] = useState(0)
  const [pkgSearch, setPkgSearch] = useState('')
  const [repoSearch, setRepoSearch] = useState('')

  const [pkgData, , pkgLoading] = usePolling(
    () => getClient().listPackages(pkgSortKey, pkgSortDirection, PAGE_SIZE, pkgPage * PAGE_SIZE, pkgSearch || undefined, showInstalledOnly || undefined),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, pkgSortKey, pkgSortDirection, pkgPage, pkgSearch, showInstalledOnly],
  )
  const packages = pkgData.entries || []

  const [featuredData] = usePolling(
    () => getClient().listFeaturedPackages(),
    [],
    [refreshKey],
  )
  const featuredGroups = featuredData || []

  const [byRepoData] = usePolling(
    () => groupByRepo ? getClient().listPackagesByRepo(pkgSearch || undefined) : Promise.resolve([]),
    [],
    [refreshKey, groupByRepo, pkgSearch],
  )
  const packagesByRepo = byRepoData || []

  const [repoData, , repoLoading] = usePolling(
    () => getClient().listRepositories(repoSortKey, repoSortDirection, PAGE_SIZE, repoPage * PAGE_SIZE, repoSearch || undefined),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, repoSortKey, repoSortDirection, repoPage, repoSearch],
  )
  const repositories = repoData.entries || []
  const displayRepos = [...repositories].reverse()

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  // Build installed lookup from the flat packages list (which includes installed status).
  const installedMap = {}
  for (const pkg of packages) {
    if (pkg.installed) {
      installedMap[`${pkg.repo}/${pkg.name}`] = pkg.installed_version || ''
    }
  }

  function installedVersion(row) {
    // If the row has the installed field (from flat list), use it directly.
    if (row.installed !== undefined) {
      if (!row.installed) return null
      return row.installed_version || ''
    }
    // Grouped view: look up from the installedMap built from flat data.
    const key = `${row.repo}/${row.name}`
    if (key in installedMap) return installedMap[key]
    return null
  }

  async function handleStartInstall(repo, name, latestVersion) {
    try {
      const versions = await getClient().listPackageVersions(name)
      if (versions && versions.length > 1) {
        setVersionSelectDialog({
          open: true,
          repo,
          name,
          versions,
          selectedVersion: latestVersion,
        })
      } else {
        await handleShowPreview(repo, name, latestVersion)
      }
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleShowPreview(repo, name, version) {
    try {
      const preview = await getClient().installPreview(repo, name, version)
      setPreviewDialog({ open: true, ...preview })
    } catch {
      // Preview not available — proceed directly.
      await handleCheckVolumes(repo, name, version)
    }
  }

  async function handleCheckVolumes(repo, name, version, importFromVersion) {
    try {
      const volInfo = await getClient().listUninstalledVolumes(repo, name)
      if (volInfo.has_uninstalled_volumes) {
        setVolumeReuseDialog({
          open: true,
          repo,
          name,
          version,
          importFromVersion,
          uninstalledVersions: volInfo.uninstalled_versions || [],
        })
        return
      }
      await handleInstall(repo, name, version, false, importFromVersion)
    } catch (err) {
      await handleInstall(repo, name, version, false, importFromVersion)
    }
  }

  async function handleInstall(repo, name, version, reuseVolumes = false, importFromVersion) {
    try {
      // Fetch questions for this specific package version.
      const questions = await getClient().getPackageQuestionsByIdentity(repo, name, version)

      // Get existing responses (from current install) to use as defaults.
      let existingResponses = {}
      try {
        existingResponses = await getClient().getResponses(repo, name, version)
      } catch {
        // no existing responses
      }

      // Get cached last responses (from previous uninstall) if no current responses.
      let lastResponses = {}
      if (!existingResponses || Object.keys(existingResponses).length === 0) {
        try {
          lastResponses = await getClient().getLastResponses(repo, name)
        } catch {
          // no last responses
        }
      }

      // Merge: current responses take precedence over last responses.
      const mergedResponses = { ...(lastResponses || {}), ...(existingResponses || {}) }

      if (questions && Object.keys(questions).length > 0) {
        setClearedCachedFields({})
        setQuestionsDialog({
          open: true,
          repo,
          name,
          version,
          questions,
          responses: mergedResponses,
          fieldErrors: {},
          clearedFields: {},
          reuseVolumes,
          importFromVersion,
        })
        return
      }

      // No questions — install directly.
      await getClient().installPackage(repo, name, version, {}, reuseVolumes, importFromVersion)
      toast.success(`Package "${name}" installed`)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleShowInfo(repo, name, version) {
    try {
      const info = await getClient().getInstalledInfo(repo, name, version)
      setInfoDialog({ open: true, repo, name, version, ...info })
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleInstallWithResponses(e) {
    e.preventDefault()
    const form = e.target.elements
    const responses = {}
    for (const key of Object.keys(questionsDialog.questions)) {
      responses[key] = form[key]?.value || ''
    }

    try {
      await getClient().installPackage(
        questionsDialog.repo,
        questionsDialog.name,
        questionsDialog.version,
        responses,
        questionsDialog.reuseVolumes || false,
        questionsDialog.importFromVersion,
      )
      toast.success(`Package "${questionsDialog.name}" installed`)
      setQuestionsDialog({ open: false })
      doRefresh()
    } catch (err) {
      // Check for per-field validation errors from the server.
      const verrs = err.problem?.validation_errors
      if (verrs && verrs.length > 0) {
        const fieldErrors = {}
        for (const ve of verrs) {
          fieldErrors[ve.name] = ve.error
        }
        setQuestionsDialog((prev) => ({ ...prev, fieldErrors }))
        toast.error('Please fix the highlighted fields')
      } else {
        toast.error(err.message)
      }
    }
  }

  async function handleUninstall() {
    try {
      await getClient().uninstallPackage(
        uninstallConfirm.repo,
        uninstallConfirm.name,
        uninstallConfirm.version,
        purgeVolumes,
      )
      toast.success(`Package "${uninstallConfirm.name}" uninstalled${purgeVolumes ? ' (volumes purged)' : ''}`)
      setUninstallConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setUninstallConfirm(null)
    }
  }

  async function handleAddRepo(e) {
    e.preventDefault()
    const name = e.target.elements.name.value
    const url = e.target.elements.url.value
    try {
      await getClient().addRepository(name, url)
      toast.success('Repository added')
      setRepoDialog(false)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleRemoveRepo() {
    try {
      await getClient().removeRepository(deleteRepoConfirm)
      toast.success(`Repository "${deleteRepoConfirm}" removed`)
      setDeleteRepoConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setDeleteRepoConfirm(null)
    }
  }

  const [refreshing, setRefreshing] = useState(false)

  async function handleRefreshRepos() {
    setRefreshing(true)
    try {
      const errs = await getClient().refreshRepositories()
      if (errs && Object.keys(errs).length > 0) {
        const names = Object.keys(errs).join(', ')
        toast.error(`Some repositories failed to refresh: ${names}`)
      } else {
        toast.success('Repositories refreshed')
      }
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    } finally {
      setRefreshing(false)
    }
  }

  async function handleMoveRepo(name, position) {
    try {
      await getClient().moveRepository(name, position)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  const packageColumns = [
    { key: 'repo', label: 'Repository' },
    {
      key: 'name',
      label: 'Name',
      transform: (v, row) => (
        <span className="inline-flex items-center gap-1">{v}{row.featured && <Star className="h-3 w-3 text-yellow-500 fill-yellow-500" />}</span>
      ),
    },
    {
      key: 'description',
      label: 'Description',
      sortable: false,
      transform: (v) => v ? <span className="text-sm text-muted-foreground">{v}</span> : null,
    },
    {
      key: 'version',
      label: 'Version',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: '_info',
      label: '',
      sortable: false,
      transform: (_, row) => {
        const instVer = installedVersion(row)
        if (instVer === null) return null
        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-6 w-6 p-0"
                onClick={() => handleShowInfo(row.repo, row.name, instVer || row.version)}
              >
                <Info className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">View configuration</TooltipContent>
          </Tooltip>
        )
      },
    },
    {
      key: '_status',
      label: 'Status',
      sortable: false,
      transform: (_, row) => {
        const instVer = installedVersion(row)
        const isInst = instVer !== null
        const hasUpgrade = isInst && instVer !== '' && instVer !== row.version
        return (
          <div className="flex items-center justify-end gap-1">
            {row.featured && (
              <Badge variant="outline" className="gap-1 text-yellow-600 border-yellow-600">
                <Star className="h-3 w-3" />
                Featured
              </Badge>
            )}
            {hasUpgrade && (
              <Tooltip>
                <TooltipTrigger>
                  <Badge
                    variant="outline"
                    className="cursor-pointer gap-1 text-blue-600 border-blue-600"
                    onClick={() => handleStartInstall(row.repo, row.name, row.version)}
                  >
                    <ArrowUpCircle className="h-3 w-3" />
                    Upgrade
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="right">
                  Upgrade from {instVer} to {row.version}
                </TooltipContent>
              </Tooltip>
            )}
            <Tooltip>
              <TooltipTrigger>
                <Badge
                  variant={isInst ? 'default' : 'secondary'}
                  className="cursor-pointer"
                  onClick={() => {
                    if (isInst) {
                      setPurgeVolumes(false)
                      setUninstallConfirm({
                        repo: row.repo,
                        name: row.name,
                        version: instVer || row.version,
                      })
                    } else {
                      handleStartInstall(row.repo, row.name, row.version)
                    }
                  }}
                >
                  {isInst
                    ? (hasUpgrade ? `Installed (${instVer})` : 'Installed')
                    : 'Not Installed'}
                </Badge>
              </TooltipTrigger>
              <TooltipContent side="right">
                Click to {isInst ? 'uninstall' : 'install'}
              </TooltipContent>
            </Tooltip>
          </div>
        )
      },
    },
  ]

  const repoColumns = [
    { key: 'name', label: 'Name' },
    {
      key: 'url',
      label: 'URL',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'error',
      label: 'Status',
      sortable: false,
      transform: (v) =>
        v ? (
          <Tooltip>
            <TooltipTrigger>
              <Badge variant="destructive" className="gap-1">
                <AlertCircle className="h-3 w-3" />
                Error
              </Badge>
            </TooltipTrigger>
            <TooltipContent className="max-w-md">
              <span className="font-mono text-xs">{v}</span>
            </TooltipContent>
          </Tooltip>
        ) : (
          <Badge variant="outline" className="gap-1 text-green-600 border-green-600">
            <CheckCircle2 className="h-3 w-3" />
            OK
          </Badge>
        ),
    },
    {
      key: '_move',
      label: '',
      sortable: false,
      transform: (_, row) => {
        const idx = repositories.findIndex((r) => r.name === row.name)
        return (
          <div className="flex items-center gap-0.5">
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              disabled={idx >= repositories.length - 1}
              onClick={() => handleMoveRepo(row.name, idx + 1)}
            >
              <ArrowUp className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              disabled={idx <= 0}
              onClick={() => handleMoveRepo(row.name, idx - 1)}
            >
              <ArrowDown className="h-3 w-3" />
            </Button>
          </div>
        )
      },
    },
    {
      key: '_delete',
      label: '',
      sortable: false,
      transform: (_, row) => (
        <Button
          variant="ghost"
          size="sm"
          className="text-destructive hover:text-destructive"
          onClick={() => setDeleteRepoConfirm(row.name)}
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      ),
    },
  ]

  const normalizedPackages = packages
    .map((pkg) => ({
      ...pkg,
      _key: `${pkg.repo}/${pkg.name}`,
    }))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Packages</h1>
        <p className="text-muted-foreground">
          Manage packages and repositories
        </p>
      </div>

      <Tabs defaultValue="packages">
        <TabsList>
          <TabsTrigger value="packages">Packages</TabsTrigger>
          <TabsTrigger value="repositories">Repositories</TabsTrigger>
        </TabsList>
        <TabsContent value="packages" className="mt-4 space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-center gap-4 pt-1">
              <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={groupByRepo}
                  onChange={(e) => setGroupByRepo(e.target.checked)}
                  className="rounded border-input"
                />
                Group by repository
              </label>
              <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={showInstalledOnly}
                  onChange={(e) => {
                    setShowInstalledOnly(e.target.checked)
                    setPkgPage(0)
                  }}
                  className="rounded border-input"
                />
                Installed only
              </label>
            </div>
            <div className="flex items-start gap-2">
              {groupByRepo && (
                <Input
                  placeholder="Search packages..."
                  className="max-w-xs"
                  value={pkgSearch}
                  onChange={(e) => {
                    setPkgSearch(e.target.value)
                    setPkgPage(0)
                  }}
                />
              )}
              {featuredGroups.length > 0 && featuredGroups.some((g) => g.packages.some((p) => !p.installed)) && (
                <Card className="bg-yellow-50/80 dark:bg-yellow-950/30 border-yellow-300 dark:border-yellow-700 py-3 max-w-sm" data-testid="featured-card">
                  <CardHeader className="pb-0 pt-0 px-4 gap-1">
                    <CardTitle className="text-sm flex items-center gap-1.5">
                      <Star className="h-4 w-4 text-yellow-500 fill-yellow-500" />
                      Featured Packages
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="px-4 pt-0">
                    <div className="space-y-2">
                      {featuredGroups.map((group) =>
                        group.packages.filter((pkg) => !pkg.installed).map((pkg) => (
                          <div key={`${pkg.repo}/${pkg.name}`} className="flex items-start justify-between gap-2">
                            <div className="min-w-0">
                              <div className="text-sm font-medium truncate">{pkg.name}</div>
                              {pkg.description && (
                                <div className="text-xs text-muted-foreground line-clamp-2">{pkg.description}</div>
                              )}
                            </div>
                            <Button
                              variant="outline"
                              size="sm"
                              className="shrink-0 h-7 text-xs gap-1"
                              onClick={() => handleStartInstall(pkg.repo, pkg.name, pkg.version)}
                            >
                              <Download className="h-3 w-3" />
                              Install
                            </Button>
                          </div>
                        ))
                      )}
                    </div>
                  </CardContent>
                </Card>
              )}
            </div>
          </div>
          {groupByRepo ? (
            <div className="space-y-2">
              {packagesByRepo.length === 0 && (
                <div className="text-center py-8 text-muted-foreground">No packages found</div>
              )}
              {packagesByRepo.map((group, groupIdx) => {
                const isExpanded = repoExpanded[group.repo] ?? (groupIdx === 0)
                return (
                  <div key={group.repo} className="rounded-md border">
                    <Table>
                      <TableBody>
                        <TableRow
                          className="cursor-pointer hover:bg-muted/50"
                          onClick={() => setRepoExpanded((prev) => ({
                            ...prev,
                            [group.repo]: !isExpanded,
                          }))}
                        >
                          <TableCell className="font-medium" colSpan={4}>
                            <div className="flex items-center gap-1">
                              {isExpanded
                                ? <ChevronDown className="h-4 w-4 text-muted-foreground" />
                                : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
                              <FolderGit2 className="h-4 w-4 text-muted-foreground" />
                              <span className="font-semibold">{group.repo}</span>
                              <span className="text-xs text-muted-foreground ml-2">
                                ({group.packages.length} package{group.packages.length !== 1 ? 's' : ''})
                              </span>
                            </div>
                          </TableCell>
                        </TableRow>
                        {isExpanded && group.packages.map((pkg) => {
                          const instVer = installedVersion(pkg)
                          const isInst = instVer !== null
                          const hasUpgrade = isInst && instVer !== '' && instVer !== pkg.version
                          const isFeatured = group.featured && group.featured.includes(pkg.name)
                          return (
                            <TableRow key={`${pkg.repo}/${pkg.name}`}>
                              <TableCell>
                                <span className="font-mono text-sm pl-6 inline-flex items-center gap-1">{pkg.name}{pkg.featured && <Star className="h-3 w-3 text-yellow-500 fill-yellow-500" />}</span>
                              </TableCell>
                              <TableCell>
                                <span className="font-mono text-sm">{pkg.version}</span>
                              </TableCell>
                              <TableCell>
                                {isInst && (
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 w-6 p-0"
                                    onClick={() => handleShowInfo(pkg.repo, pkg.name, instVer || pkg.version)}
                                  >
                                    <Info className="h-3.5 w-3.5" />
                                  </Button>
                                )}
                              </TableCell>
                              <TableCell className="text-right">
                                <div className="flex items-center justify-end gap-1">
                                  {isFeatured && (
                                    <Badge variant="outline" className="gap-1 text-yellow-600 border-yellow-600">
                                      <Star className="h-3 w-3" />
                                      Featured
                                    </Badge>
                                  )}
                                  {hasUpgrade && (
                                    <Badge
                                      variant="outline"
                                      className="cursor-pointer gap-1 text-blue-600 border-blue-600"
                                      onClick={() => handleStartInstall(pkg.repo, pkg.name, pkg.version)}
                                    >
                                      <ArrowUpCircle className="h-3 w-3" />
                                      Upgrade
                                    </Badge>
                                  )}
                                  <Badge
                                    variant={isInst ? 'default' : 'secondary'}
                                    className="cursor-pointer"
                                    onClick={() => {
                                      if (isInst) {
                                        setPurgeVolumes(false)
                                        setUninstallConfirm({
                                          repo: pkg.repo,
                                          name: pkg.name,
                                          version: instVer || pkg.version,
                                        })
                                      } else {
                                        handleStartInstall(pkg.repo, pkg.name, pkg.version)
                                      }
                                    }}
                                  >
                                    {isInst
                                      ? (hasUpgrade ? `Installed (${instVer})` : 'Installed')
                                      : 'Not Installed'}
                                  </Badge>
                                </div>
                              </TableCell>
                            </TableRow>
                          )
                        })}
                      </TableBody>
                    </Table>
                  </div>
                )
              })}
            </div>
          ) : (
            <>
              {pkgLoading && packages.length === 0 && (
                <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
              )}
              <DataTable
                data={normalizedPackages}
                columns={packageColumns}
                entryKey="_key"
                page={pkgPage}
                setPage={setPkgPage}
                hasMore={pkgData.has_more}
                totalPages={pkgData.total_pages}
                totalCount={pkgData.total_count}
                sortKey={pkgSortKey}
                sortDirection={pkgSortDirection}
                onSortChange={(key, dir) => {
                  setPkgSortKey(key)
                  setPkgSortDirection(dir)
                  setPkgPage(0)
                }}
                onReset={() => {
                  setPkgSortKey('name')
                  setPkgSortDirection('asc')
                  setPkgSearch('')
                  setPkgPage(0)
                }}
                onSearchChange={(s) => {
                  setPkgSearch(s)
                  setPkgPage(0)
                }}
              />
            </>
          )}
        </TabsContent>
        <TabsContent value="repositories" className="mt-4 space-y-4">
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={handleRefreshRepos} disabled={refreshing}>
              <RefreshCw className={`h-4 w-4 mr-1${refreshing ? ' animate-spin' : ''}`} />
              {refreshing ? 'Refreshing...' : 'Refresh'}
            </Button>
            <Button onClick={() => setRepoDialog(true)}>
              <Plus className="h-4 w-4 mr-1" />
              Add Repository
            </Button>
          </div>
          <p className="text-sm text-muted-foreground">
            The first repository has the highest priority. If the same package
            appears in multiple repositories, the one closest to the top is used.
            Use the arrow buttons to reorder.
          </p>
          {repoLoading && repositories.length === 0 && (
            <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
          )}
          <DataTable
            data={displayRepos}
            columns={repoColumns}
            entryKey="name"
            page={repoPage}
            setPage={setRepoPage}
            hasMore={repoData.has_more}
            totalPages={repoData.total_pages}
            totalCount={repoData.total_count}
            sortKey={repoSortKey}
            sortDirection={repoSortDirection}
            onSortChange={(key, dir) => {
              setRepoSortKey(key)
              setRepoSortDirection(dir)
              setRepoPage(0)
            }}
            onReset={() => {
              setRepoSortKey('')
              setRepoSortDirection('')
              setRepoSearch('')
              setRepoPage(0)
            }}
            onSearchChange={(s) => {
              setRepoSearch(s)
              setRepoPage(0)
            }}
          />
        </TabsContent>
      </Tabs>

      {/* Install Preview Dialog */}
      <Dialog
        open={previewDialog.open}
        onOpenChange={(v) => !v && setPreviewDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              Install {previewDialog.name} {previewDialog.version}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {previewDialog.upgrading_from && (
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="gap-1 text-blue-600 border-blue-600">
                  <ArrowUpCircle className="h-3 w-3" />
                  Upgrading from {previewDialog.upgrading_from}
                </Badge>
              </div>
            )}
            {previewDialog.description && (
              <p className="text-sm text-muted-foreground">{previewDialog.description}</p>
            )}
            <div className="text-sm">
              <span className="text-muted-foreground">Image: </span>
              <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">{previewDialog.image}</code>
            </div>
            {previewDialog.volumes?.length > 0 && (
              <div className="space-y-2">
                <h4 className="text-sm font-medium">Volumes</h4>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Mountpoint</TableHead>
                      <TableHead>Quota</TableHead>
                      <TableHead className="text-right"><div className="pr-2">Status</div></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {previewDialog.volumes.map((vol) => (
                      <TableRow key={vol.name}>
                        <TableCell className="font-mono text-xs">{vol.name}</TableCell>
                        <TableCell className="font-mono text-xs">{vol.mountpoint}</TableCell>
                        <TableCell className="font-mono text-xs">{vol.quota || '-'}</TableCell>
                        <TableCell className="text-right">
                          {vol.migrated ? (
                            <Badge variant="outline" className="text-blue-600 border-blue-600">Migrated</Badge>
                          ) : (
                            <Badge variant="secondary">New</Badge>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
            {previewDialog.external_ports?.length > 0 && (
              <div className="space-y-1">
                <h4 className="text-sm font-medium">External Ports</h4>
                <div className="text-sm text-muted-foreground">
                  {previewDialog.external_ports.map((p) => (
                    <span key={p.external} className="inline-block mr-3 font-mono text-xs">
                      {p.external} → {p.internal}
                    </span>
                  ))}
                </div>
              </div>
            )}
            {previewDialog.quota_exceeds_disk && (
              <div className="rounded-md border border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20 p-3">
                <p className="text-sm text-yellow-700 dark:text-yellow-400">
                  <AlertCircle className="h-4 w-4 inline mr-1" />
                  Total volume quotas may exceed available disk space.
                </p>
              </div>
            )}
            {previewDialog.has_questions && (
              <p className="text-xs text-muted-foreground">
                Configuration questions will follow on the next screen.
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setPreviewDialog({ open: false })}
            >
              Cancel
            </Button>
            <Button
              onClick={() => {
                const { repo, name, version, upgrading_from } = previewDialog
                const importFrom = upgrading_from || undefined
                setPreviewDialog({ open: false })
                handleCheckVolumes(repo, name, version, importFrom)
              }}
            >
              Continue
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Install Questions Dialog */}
      <Dialog
        open={questionsDialog.open}
        onOpenChange={(v) => !v && setQuestionsDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              Install {questionsDialog.name} {questionsDialog.version}
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleInstallWithResponses}>
            <div className="space-y-4 py-4">
              {questionsDialog.questions &&
                Object.entries(questionsDialog.questions).map(
                  ([key, question]) => {
                    const fieldError = questionsDialog.fieldErrors?.[key]
                    const cachedValue = questionsDialog.responses?.[key]
                    const isCleared = questionsDialog.clearedFields?.[key]
                    const hasCachedValue = !!cachedValue && !isCleared

                    // Build placeholder: show default or type hint
                    let placeholder
                    if (question.default) {
                      placeholder = `Default: ${question.default}`
                    } else if (question.type === 'duration') {
                      placeholder = 'e.g. 30s, 5m, 2h, 1d'
                    } else if (question.type === 'port') {
                      placeholder = 'Auto-assigned if empty'
                    } else if (question.type === 'hostname') {
                      placeholder = 'Auto-generated if empty'
                    }

                    return (
                      <div key={key} className="space-y-2">
                        <Label htmlFor={key}>{question.query}</Label>
                        {hasCachedValue ? (
                          <div className="flex items-center gap-2">
                            <div className="flex-1 rounded-md border bg-muted/50 px-3 py-2 text-sm font-mono">
                              {question.type === 'secret' ? '********' : cachedValue}
                            </div>
                            <input type="hidden" name={key} value={cachedValue} />
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="sm"
                                  className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                                  onClick={() => setQuestionsDialog((prev) => ({
                                    ...prev,
                                    clearedFields: { ...prev.clearedFields, [key]: true },
                                  }))}
                                >
                                  <X className="h-4 w-4" />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>Clear to enter a new value</TooltipContent>
                            </Tooltip>
                          </div>
                        ) : (
                          <Input
                            id={key}
                            name={key}
                            type={question.type === 'secret' ? 'password' : 'text'}
                            placeholder={placeholder}
                            defaultValue=""
                            className={fieldError ? 'border-destructive' : ''}
                          />
                        )}
                        {question.default && !hasCachedValue && (
                          <p className="text-xs text-muted-foreground">
                            Default: <span className="font-mono">{question.default}</span>
                          </p>
                        )}
                        {question.type === 'duration' && (
                          <p className="text-xs text-muted-foreground">
                            Duration format: use s (seconds), m (minutes), h (hours), or d (days)
                          </p>
                        )}
                        {fieldError && (
                          <p className="text-sm text-destructive">{fieldError}</p>
                        )}
                      </div>
                    )
                  },
                )}
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setQuestionsDialog({ open: false })}
              >
                Cancel
              </Button>
              <Button type="submit">Install</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Package Info Dialog */}
      <Dialog
        open={infoDialog.open}
        onOpenChange={(v) => !v && setInfoDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {infoDialog.name}@{infoDialog.version}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {infoDialog.questions && Object.keys(infoDialog.questions).length > 0 && (
              <div className="space-y-2">
                <h4 className="text-sm font-medium">Configuration</h4>
                <div className="space-y-1">
                  {Object.entries(infoDialog.questions).map(([key, question]) => (
                    <div key={key} className="flex justify-between gap-4 text-sm">
                      <span className="text-muted-foreground">{question.query}</span>
                      <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0">
                        {question.type === 'secret' ? '********' : (infoDialog.responses?.[key] || '-')}
                      </code>
                    </div>
                  ))}
                </div>
              </div>
            )}
            {infoDialog.notes && Object.keys(infoDialog.notes).length > 0 && (
              <>
                {infoDialog.questions && Object.keys(infoDialog.questions).length > 0 && (
                  <Separator />
                )}
                <div className="space-y-1">
                  {Object.entries(infoDialog.notes).map(([label, value]) => {
                    const noteType = infoDialog.note_types?.[label]
                    let display
                    if (noteType === 'url') {
                      display = (
                        <a href={value} target="_blank" rel="noopener noreferrer" className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0 underline text-primary">
                          {value}
                        </a>
                      )
                    } else if (noteType === 'email') {
                      display = (
                        <a href={`mailto:${value}`} className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0 underline text-primary">
                          {value}
                        </a>
                      )
                    } else if (noteType === 'phone') {
                      display = (
                        <a href={`tel:${value}`} className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0 underline text-primary">
                          {value}
                        </a>
                      )
                    } else {
                      display = (
                        <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0">
                          {value}
                        </code>
                      )
                    }
                    return (
                      <div key={label} className="flex justify-between gap-4 text-sm">
                        <span className="text-muted-foreground">{label}</span>
                        {display}
                      </div>
                    )
                  })}
                </div>
              </>
            )}
            {(!infoDialog.questions || Object.keys(infoDialog.questions).length === 0) &&
              (!infoDialog.notes || Object.keys(infoDialog.notes).length === 0) && (
              <p className="text-sm text-muted-foreground">No configuration for this package.</p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setInfoDialog({ open: false })}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add Repository Dialog */}
      <Dialog open={repoDialog} onOpenChange={setRepoDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <FolderGit2 className="h-4 w-4 inline mr-2" />
              Add Repository
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleAddRepo}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  name="name"
                  placeholder="my-repo"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="url">Repository URL</Label>
                <Input
                  id="url"
                  name="url"
                  placeholder="https://..."
                  required
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setRepoDialog(false)}
              >
                Cancel
              </Button>
              <Button type="submit">Add</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Version Select Dialog */}
      <Dialog
        open={versionSelectDialog.open}
        onOpenChange={(v) => !v && setVersionSelectDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Install {versionSelectDialog.name}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Version</Label>
              <Select
                value={versionSelectDialog.selectedVersion || ''}
                onValueChange={(v) =>
                  setVersionSelectDialog((prev) => ({ ...prev, selectedVersion: v }))
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Select version" />
                </SelectTrigger>
                <SelectContent>
                  {(versionSelectDialog.versions || []).map((v) => (
                    <SelectItem key={v} value={v}>
                      {v}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setVersionSelectDialog({ open: false })}
            >
              Cancel
            </Button>
            <Button
              onClick={() => {
                const { repo, name, selectedVersion } = versionSelectDialog
                setVersionSelectDialog({ open: false })
                handleShowPreview(repo, name, selectedVersion)
              }}
            >
              Install
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Volume Reuse Dialog */}
      <Dialog
        open={volumeReuseDialog.open}
        onOpenChange={(v) => !v && setVolumeReuseDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Existing Data Found</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <p className="text-sm text-muted-foreground">
              Previous data exists for{' '}
              <code className="font-mono text-sm bg-muted px-1 rounded">
                {volumeReuseDialog.name}
              </code>
              {volumeReuseDialog.uninstalledVersions?.length > 0 && (
                <span>
                  {' '}(versions: {volumeReuseDialog.uninstalledVersions.join(', ')})
                </span>
              )}
              . Would you like to reuse it or start fresh?
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setVolumeReuseDialog({ open: false })}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={async () => {
                const { repo, name, version, importFromVersion } = volumeReuseDialog
                setVolumeReuseDialog({ open: false })
                try {
                  await getClient().purgeUninstalledVolumes(repo, name)
                } catch (err) {
                  toast.error(err.message)
                  return
                }
                await handleInstall(repo, name, version, false, importFromVersion)
              }}
            >
              Start Fresh
            </Button>
            <Button
              onClick={() => {
                const { repo, name, version, importFromVersion } = volumeReuseDialog
                setVolumeReuseDialog({ open: false })
                handleInstall(repo, name, version, true, importFromVersion)
              }}
            >
              Reuse Data
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Uninstall Confirm */}
      <Dialog
        open={!!uninstallConfirm}
        onOpenChange={(v) => !v && setUninstallConfirm(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Uninstall Package</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <p className="text-sm text-muted-foreground">
              Uninstall{' '}
              <code className="font-mono text-sm bg-muted px-1 rounded">
                {uninstallConfirm?.name}@{uninstallConfirm?.version}
              </code>
              ?
            </p>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={purgeVolumes}
                onChange={(e) => setPurgeVolumes(e.target.checked)}
                className="rounded border-input"
              />
              Purge all volumes for this package
            </label>
            {purgeVolumes && (
              <p className="text-sm text-destructive">
                All data stored in this package&apos;s volumes will be permanently deleted.
              </p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setUninstallConfirm(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleUninstall}>
              Uninstall
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Remove Repo Confirm */}
      <ConfirmDialog
        open={!!deleteRepoConfirm}
        title="Remove Repository"
        onConfirm={handleRemoveRepo}
        onCancel={() => setDeleteRepoConfirm(null)}
        confirmLabel="Remove"
        variant="destructive"
      >
        Remove repository{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {deleteRepoConfirm}
        </code>
        ?
      </ConfirmDialog>

    </div>
  )
}

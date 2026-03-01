import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
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
  DialogDescription,
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
import InstallPreviewDialog from '@/components/packages/InstallPreviewDialog.jsx'
import InstallQuestionsDialog from '@/components/packages/InstallQuestionsDialog.jsx'
import PackageInfoDialog from '@/components/packages/PackageInfoDialog.jsx'
import VolumeReuseDialog from '@/components/packages/VolumeReuseDialog.jsx'

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
                aria-label="Package details"
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
              aria-label="Move repository up"
            >
              <ArrowUp className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              disabled={idx <= 0}
              onClick={() => handleMoveRepo(row.name, idx - 1)}
              aria-label="Move repository down"
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
          aria-label="Remove repository"
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
                                    aria-label="Package details"
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

      <InstallPreviewDialog
        dialog={previewDialog}
        onClose={() => setPreviewDialog({ open: false })}
        onContinue={(d) => {
          const importFrom = d.upgrading_from || undefined
          setPreviewDialog({ open: false })
          handleCheckVolumes(d.repo, d.name, d.version, importFrom)
        }}
      />

      <InstallQuestionsDialog
        dialog={questionsDialog}
        onClose={() => setQuestionsDialog({ open: false })}
        onSubmit={handleInstallWithResponses}
        onClearField={(key) => setQuestionsDialog((prev) => ({
          ...prev,
          clearedFields: { ...prev.clearedFields, [key]: true },
        }))}
      />

      <PackageInfoDialog
        dialog={infoDialog}
        onClose={() => setInfoDialog({ open: false })}
      />

      {/* Add Repository Dialog */}
      <Dialog open={repoDialog} onOpenChange={setRepoDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <FolderGit2 className="h-4 w-4 inline mr-2" />
              Add Repository
            </DialogTitle>
            <DialogDescription>Add a new package repository by name and URL.</DialogDescription>
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
            <DialogDescription>Select a version to install.</DialogDescription>
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

      <VolumeReuseDialog
        dialog={volumeReuseDialog}
        onClose={() => setVolumeReuseDialog({ open: false })}
        onStartFresh={async (d) => {
          setVolumeReuseDialog({ open: false })
          try {
            await getClient().purgeUninstalledVolumes(d.repo, d.name)
          } catch (err) {
            toast.error(err.message)
            return
          }
          await handleInstall(d.repo, d.name, d.version, false, d.importFromVersion)
        }}
        onReuse={(d) => {
          setVolumeReuseDialog({ open: false })
          handleInstall(d.repo, d.name, d.version, true, d.importFromVersion)
        }}
      />

      {/* Uninstall Confirm */}
      <Dialog
        open={!!uninstallConfirm}
        onOpenChange={(v) => !v && setUninstallConfirm(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Uninstall Package</DialogTitle>
            <DialogDescription>Remove this package and optionally purge its volumes.</DialogDescription>
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

import { useState, useEffect, useRef, useMemo } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
import DataTable from '@/components/DataTable.jsx'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Trash2, FolderGit2, AlertCircle, CheckCircle2, Info, ArrowUpCircle, ArrowUp, ArrowDown, ChevronRight, ChevronDown, X, Star, Download, MoreHorizontal, FileCode, Copy, Check, Loader2 } from 'lucide-react'
import { Separator } from '@/components/ui/separator'
import InstallPreviewDialog from '@/components/packages/InstallPreviewDialog.jsx'
import InstallQuestionsDialog from '@/components/packages/InstallQuestionsDialog.jsx'
import PackageInfoDialog from '@/components/packages/PackageInfoDialog.jsx'
import VolumeReuseDialog from '@/components/packages/VolumeReuseDialog.jsx'
import RepositoryTab from '@/components/packages/RepositoryTab.jsx'
import { readProgressStream } from '@/api/progress.js'
import RepositoryDialogs from '@/components/packages/RepositoryDialogs.jsx'

function ManifestCopyButton({ content, t }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      variant="outline"
      onClick={() => {
        function onSuccess() {
          setCopied(true)
          setTimeout(() => setCopied(false), 2000)
        }
        if (navigator.clipboard && window.isSecureContext) {
          navigator.clipboard.writeText(content).then(onSuccess)
          return
        }
        const ta = document.createElement('textarea')
        ta.value = content
        ta.style.position = 'fixed'
        ta.style.left = '-9999px'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.focus()
        ta.select()
        try {
          document.execCommand('copy')
          onSuccess()
        } finally {
          document.body.removeChild(ta)
        }
      }}
    >
      {copied ? <Check className="h-4 w-4 mr-1" /> : <Copy className="h-4 w-4 mr-1" />}
      {copied ? t('packages.manifest_copied') : t('packages.manifest_copy')}
    </Button>
  )
}

export default function PackageManagement() {
  const { t } = useI18n()
  useEffect(() => { document.title = t('packages.page_title') }, [t])
  const [refreshKey, setRefreshKey] = useState(0)

  // Package state
  const [uninstallConfirm, setUninstallConfirm] = useState(null)
  const [purgeVolumes, setPurgeVolumes] = useState(false)
  const [, setClearedCachedFields] = useState({})
  const [questionsDialog, setQuestionsDialog] = useState({ open: false })
  const [infoDialog, setInfoDialog] = useState({ open: false })
  const [manifestDialog, setManifestDialog] = useState({ open: false })
  const [versionSelectDialog, setVersionSelectDialog] = useState({ open: false })
  const [volumeReuseDialog, setVolumeReuseDialog] = useState({ open: false })
  const [previewDialog, setPreviewDialog] = useState({ open: false })
  const [progressDialog, setProgressDialog] = useState({ open: false, action: '', step: '' })

  // Repository state
  const [repoDialog, setRepoDialog] = useState(false)
  const [deleteRepoConfirm, setDeleteRepoConfirm] = useState(null)

  // Group by repository toggle
  const [groupByRepo, setGroupByRepo] = useState(() => localStorage.getItem('pkg_group_by_repo') === 'true')
  const [repoExpanded, setRepoExpanded] = useState({})
  const [showInstalledOnly, setShowInstalledOnly] = useState(() => localStorage.getItem('pkg_installed_only') === 'true')
  const [showFeaturedOnly, setShowFeaturedOnly] = useState(() => {
    const v = localStorage.getItem('pkg_featured_only')
    return v === null ? true : v === 'true'
  })


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
    () => getClient().listPackages(pkgSortKey, pkgSortDirection, PAGE_SIZE, pkgPage * PAGE_SIZE, pkgSearch || undefined, showInstalledOnly || undefined, showFeaturedOnly || undefined),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, pkgSortKey, pkgSortDirection, pkgPage, pkgSearch, showInstalledOnly, showFeaturedOnly],
  )
  const packages = useMemo(() => pkgData.entries || [], [pkgData.entries])

  // Auto-uncheck "installed only" when no installed packages exist
  const hasAutoUncheckedInstalled = useRef(false)
  useEffect(() => {
    if (!hasAutoUncheckedInstalled.current && showInstalledOnly && !pkgLoading && packages.length === 0) {
      hasAutoUncheckedInstalled.current = true
      setShowInstalledOnly(false)
      localStorage.setItem('pkg_installed_only', 'false')
    }
  }, [showInstalledOnly, pkgLoading, packages])

  const [byRepoData] = usePolling(
    () => groupByRepo ? getClient().listPackagesByRepo(pkgSearch || undefined, showFeaturedOnly || undefined) : Promise.resolve([]),
    [],
    [refreshKey, groupByRepo, pkgSearch, showFeaturedOnly],
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
    } catch {
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

      // No questions — install directly with progress.
      setProgressDialog({ open: true, action: 'install', step: '' })
      try {
        const resp = await getClient().installPackageStream(repo, name, version, {}, reuseVolumes, importFromVersion)
        await readProgressStream(resp, (step) => {
          setProgressDialog((prev) => ({ ...prev, step }))
        })
        toast.success(t('packages.toast_installed'))
        doRefresh()
      } finally {
        setProgressDialog({ open: false, action: '', step: '' })
      }
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

  async function handleShowManifest(repo, name, version) {
    setManifestDialog({ open: true, repo, name, version, content: null })
    try {
      const content = await getClient().getPackageManifest(repo, name, version)
      setManifestDialog((prev) => ({ ...prev, content }))
    } catch (err) {
      toast.error(err.message)
      setManifestDialog({ open: false })
    }
  }

  async function handleInstallWithResponses(e) {
    e.preventDefault()
    const form = e.target.elements
    const responses = {}
    for (const key of Object.keys(questionsDialog.questions)) {
      responses[key] = form[key]?.value || ''
    }

    // Save dialog state before closing so we can restore it on validation errors.
    const savedDialog = { ...questionsDialog }
    setQuestionsDialog({ open: false })
    setProgressDialog({ open: true, action: 'install', step: '' })

    try {
      const resp = await getClient().installPackageStream(
        savedDialog.repo,
        savedDialog.name,
        savedDialog.version,
        responses,
        savedDialog.reuseVolumes || false,
        savedDialog.importFromVersion,
      )
      await readProgressStream(resp, (step) => {
        setProgressDialog((prev) => ({ ...prev, step }))
      })
      toast.success(t('packages.toast_installed'))
      doRefresh()
    } catch (err) {
      // Check for per-field validation errors from the server.
      const verrs = err.problem?.validation_errors
      if (verrs && verrs.length > 0) {
        const fieldErrors = {}
        for (const ve of verrs) {
          fieldErrors[ve.name] = ve.error
        }
        setQuestionsDialog({ ...savedDialog, open: true, fieldErrors })
        toast.error(t('packages.toast_fix_fields'))
      } else {
        toast.error(err.message)
      }
    } finally {
      setProgressDialog({ open: false, action: '', step: '' })
    }
  }

  async function handleUninstall() {
    const { repo, name, version } = uninstallConfirm
    const shouldPurge = purgeVolumes
    setUninstallConfirm(null)
    setProgressDialog({ open: true, action: 'uninstall', step: '' })

    try {
      const resp = await getClient().uninstallPackageStream(repo, name, version, shouldPurge)
      await readProgressStream(resp, (step) => {
        setProgressDialog((prev) => ({ ...prev, step }))
      })
      toast.success(shouldPurge ? t('packages.toast_uninstalled_purged') : t('packages.toast_uninstalled'))
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    } finally {
      setProgressDialog({ open: false, action: '', step: '' })
    }
  }

  async function handleAddRepo(e) {
    e.preventDefault()
    const name = e.target.elements.name.value
    const url = e.target.elements.url.value
    try {
      await getClient().addRepository(name, url)
      toast.success(t('packages.toast_repo_added'))
      setRepoDialog(false)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleRemoveRepo() {
    try {
      await getClient().removeRepository(deleteRepoConfirm)
      toast.success(t('packages.toast_repo_removed'))
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
    setProgressDialog({ open: true, action: 'refresh', step: 'refreshing' })
    try {
      const resp = await getClient().refreshRepositoriesStream()
      await readProgressStream(resp, (step) => {
        setProgressDialog((prev) => ({ ...prev, step }))
      })
      toast.success(t('packages.toast_repos_refreshed'))
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    } finally {
      setRefreshing(false)
      setProgressDialog({ open: false, action: '', step: '' })
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
    {
      key: 'name',
      label: t('packages.col_name'),
      transform: (v, row) => (
        <span className="inline-flex items-center gap-1">{v}{row.featured && <Star className="h-3 w-3 text-yellow-500 fill-yellow-500" />}</span>
      ),
    },
    {
      key: 'description',
      label: t('packages.col_description'),
      sortable: false,
      transform: (v) => v ? <span className="text-sm text-muted-foreground">{v}</span> : null,
    },
    {
      key: '_actions',
      label: t('packages.col_actions'),
      sortable: false,
      transform: (_, row) => {
        const instVer = installedVersion(row)
        const isInst = instVer !== null
        return (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="h-6 w-6 p-0" aria-label={t('packages.actions_label')}>
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              {isInst && (
                <DropdownMenuItem onClick={() => handleShowInfo(row.repo, row.name, instVer || row.version)}>
                  <Info className="h-3 w-3 mr-2" />
                  {t('packages.action_info')}
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={() => handleShowManifest(row.repo, row.name, row.version)}>
                <FileCode className="h-3 w-3 mr-2" />
                {t('packages.action_manifest')}
              </DropdownMenuItem>
              <DropdownMenuItem disabled className="text-xs text-muted-foreground">
                <span className="font-mono">{row.version}</span>
                <span className="ml-1">({row.repo})</span>
              </DropdownMenuItem>
              {isInst && (
                <DropdownMenuItem
                  className="text-destructive focus:text-destructive"
                  onClick={() => {
                    setPurgeVolumes(false)
                    setUninstallConfirm({ repo: row.repo, name: row.name, version: instVer || row.version })
                  }}
                >
                  <Trash2 className="h-3 w-3 mr-2" />
                  {t('packages.action_uninstall')}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )
      },
    },
    {
      key: '_status',
      label: t('packages.col_status'),
      sortable: false,
      transform: (_, row) => {
        const instVer = installedVersion(row)
        const isInst = instVer !== null
        const hasUpgrade = isInst && instVer !== '' && instVer !== row.version
        return (
          <div className="flex items-center justify-end gap-1">
            {hasUpgrade && (
              <Tooltip>
                <TooltipTrigger>
                  <Badge
                    variant="outline"
                    className="cursor-pointer gap-1 text-blue-600 border-blue-600"
                    onClick={() => handleStartInstall(row.repo, row.name, row.version)}
                  >
                    <ArrowUpCircle className="h-3 w-3" />
                    {t('packages.badge_upgrade')}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="right">
                  {t('packages.badge_upgrade')} {instVer} → {row.version}
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
                    ? (hasUpgrade ? `${t('packages.status_installed')} (${instVer})` : t('packages.status_installed'))
                    : t('packages.status_not_installed')}
                </Badge>
              </TooltipTrigger>
              <TooltipContent side="right">
                {isInst ? t('packages.tooltip_uninstall') : t('packages.tooltip_install')}
              </TooltipContent>
            </Tooltip>
          </div>
        )
      },
    },
  ]

  const repoColumns = [
    { key: 'name', label: t('packages.col_repo_name') },
    {
      key: 'url',
      label: t('packages.col_repo_url'),
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'error',
      label: t('packages.col_repo_status'),
      sortable: false,
      transform: (v) =>
        v ? (
          <Tooltip>
            <TooltipTrigger>
              <Badge variant="destructive" className="gap-1">
                <AlertCircle className="h-3 w-3" />
                {t('packages.repo_status_error')}
              </Badge>
            </TooltipTrigger>
            <TooltipContent className="max-w-md">
              <span className="font-mono text-xs">{v}</span>
            </TooltipContent>
          </Tooltip>
        ) : (
          <Badge variant="outline" className="gap-1 text-green-600 border-green-600">
            <CheckCircle2 className="h-3 w-3" />
            {t('packages.repo_status_ok')}
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
              aria-label={t('packages.move_repo_up_label')}
            >
              <ArrowUp className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              disabled={idx <= 0}
              onClick={() => handleMoveRepo(row.name, idx - 1)}
              aria-label={t('packages.move_repo_down_label')}
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
          aria-label={t('packages.remove_repo_label')}
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
        <h1 className="text-3xl font-bold tracking-tight">{t('packages.title')}</h1>
        <p className="text-muted-foreground">
          {t('packages.description')}
        </p>
      </div>

      <Tabs defaultValue="packages">
        <TabsList>
          <TabsTrigger value="packages">{t('packages.tab_packages')}</TabsTrigger>
          <TabsTrigger value="repositories">{t('packages.tab_repositories')}</TabsTrigger>
        </TabsList>
        <TabsContent value="packages" className="mt-4 space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-center gap-4 pt-1">
              <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={groupByRepo}
                  onChange={(e) => {
                    setGroupByRepo(e.target.checked)
                    localStorage.setItem('pkg_group_by_repo', String(e.target.checked))
                  }}
                  className="rounded border-input"
                />
                {t('packages.group_by_repo')}
              </label>
              <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={showInstalledOnly}
                  onChange={(e) => {
                    setShowInstalledOnly(e.target.checked)
                    localStorage.setItem('pkg_installed_only', String(e.target.checked))
                    setPkgPage(0)
                  }}
                  className="rounded border-input"
                />
                {t('packages.installed_only')}
              </label>
              <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={showFeaturedOnly}
                  onChange={(e) => {
                    setShowFeaturedOnly(e.target.checked)
                    localStorage.setItem('pkg_featured_only', String(e.target.checked))
                    setPkgPage(0)
                  }}
                  className="rounded border-input"
                />
                {t('packages.featured_only')}
              </label>
            </div>
            {groupByRepo && (
              <Input
                placeholder={t('packages.search_placeholder')}
                className="max-w-xs"
                value={pkgSearch}
                onChange={(e) => {
                  setPkgSearch(e.target.value)
                  setPkgPage(0)
                }}
              />
            )}
          </div>
          {groupByRepo ? (
            <div className="space-y-2">
              {packagesByRepo.length === 0 && (
                <div className="text-center py-8 text-muted-foreground">{t('packages.no_packages')}</div>
              )}
              {packagesByRepo.map((group, groupIdx) => {
                const isExpanded = repoExpanded[group.repo] ?? (groupIdx === 0)
                return (
                  <div key={group.repo} className="rounded-md border">
                    <Table>
                      <TableHeader>
                        <TableRow
                          className="cursor-pointer hover:bg-muted/50"
                          onClick={() => setRepoExpanded((prev) => ({
                            ...prev,
                            [group.repo]: !isExpanded,
                          }))}
                        >
                          <TableHead colSpan={3}>
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
                          </TableHead>
                        </TableRow>
                        {isExpanded && <TableRow>
                          <TableHead>{t('packages.col_name')}</TableHead>
                          <TableHead>{t('packages.col_actions')}</TableHead>
                          <TableHead className="text-right"><div className="pr-2">{t('packages.col_status')}</div></TableHead>
                        </TableRow>}
                      </TableHeader>
                      <TableBody>
                        {isExpanded && group.packages.map((pkg) => {
                          const instVer = installedVersion(pkg)
                          const isInst = instVer !== null
                          const hasUpgrade = isInst && instVer !== '' && instVer !== pkg.version
                          return (
                            <TableRow key={`${pkg.repo}/${pkg.name}`}>
                              <TableCell>
                                <span className="font-mono text-sm pl-6 inline-flex items-center gap-1">{pkg.name}{pkg.featured && <Star className="h-3 w-3 text-yellow-500 fill-yellow-500" />}</span>
                              </TableCell>
                              <TableCell>
                                <DropdownMenu>
                                  <DropdownMenuTrigger asChild>
                                    <Button variant="ghost" size="sm" className="h-6 w-6 p-0" aria-label={t('packages.actions_label')}>
                                      <MoreHorizontal className="h-4 w-4" />
                                    </Button>
                                  </DropdownMenuTrigger>
                                  <DropdownMenuContent>
                                    {isInst && (
                                      <DropdownMenuItem onClick={() => handleShowInfo(pkg.repo, pkg.name, instVer || pkg.version)}>
                                        <Info className="h-3 w-3 mr-2" />
                                        {t('packages.action_info')}
                                      </DropdownMenuItem>
                                    )}
                                    <DropdownMenuItem onClick={() => handleShowManifest(pkg.repo, pkg.name, pkg.version)}>
                                      <FileCode className="h-3 w-3 mr-2" />
                                      {t('packages.action_manifest')}
                                    </DropdownMenuItem>
                                    <DropdownMenuItem disabled className="text-xs text-muted-foreground">
                                      <span className="font-mono">{pkg.version}</span>
                                      <span className="ml-1">({pkg.repo})</span>
                                    </DropdownMenuItem>
                                    {isInst && (
                                      <DropdownMenuItem
                                        className="text-destructive focus:text-destructive"
                                        onClick={() => {
                                          setPurgeVolumes(false)
                                          setUninstallConfirm({ repo: pkg.repo, name: pkg.name, version: instVer || pkg.version })
                                        }}
                                      >
                                        <Trash2 className="h-3 w-3 mr-2" />
                                        {t('packages.action_uninstall')}
                                      </DropdownMenuItem>
                                    )}
                                  </DropdownMenuContent>
                                </DropdownMenu>
                              </TableCell>
                              <TableCell className="text-right">
                                <div className="flex items-center justify-end gap-1">
                                  {hasUpgrade && (
                                    <Badge
                                      variant="outline"
                                      className="cursor-pointer gap-1 text-blue-600 border-blue-600"
                                      onClick={() => handleStartInstall(pkg.repo, pkg.name, pkg.version)}
                                    >
                                      <ArrowUpCircle className="h-3 w-3" />
                                      {t('packages.badge_upgrade')}
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
                                      ? (hasUpgrade ? `${t('packages.status_installed')} (${instVer})` : t('packages.status_installed'))
                                      : t('packages.status_not_installed')}
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
                <div className="text-center py-8 text-muted-foreground animate-pulse">{t('packages.loading')}</div>
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
          <RepositoryTab
            repositories={repositories}
            repoColumns={repoColumns}
            repoPage={repoPage}
            setRepoPage={setRepoPage}
            repoData={repoData}
            repoSortKey={repoSortKey}
            repoSortDirection={repoSortDirection}
            setRepoSortKey={setRepoSortKey}
            setRepoSortDirection={setRepoSortDirection}
            setRepoSearch={setRepoSearch}
            repoLoading={repoLoading}
            handleRefreshRepos={handleRefreshRepos}
            refreshing={refreshing}
            setRepoDialog={setRepoDialog}
            displayRepos={displayRepos}
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

      {/* Manifest Dialog */}
      <Dialog
        open={manifestDialog.open}
        onOpenChange={(v) => !v && setManifestDialog({ open: false })}
      >
        <DialogContent className="max-w-2xl max-h-[80vh]">
          <DialogHeader>
            <DialogTitle>{t('packages.manifest_title')}</DialogTitle>
            <DialogDescription>
              {manifestDialog.name}@{manifestDialog.version} ({manifestDialog.repo})
            </DialogDescription>
          </DialogHeader>
          <div className="overflow-auto max-h-[60vh] rounded border bg-muted p-4">
            {manifestDialog.content === null ? (
              <p className="text-sm text-muted-foreground animate-pulse">{t('packages.manifest_loading')}</p>
            ) : (
              <pre className="text-xs font-mono whitespace-pre-wrap break-words">{manifestDialog.content}</pre>
            )}
          </div>
          <DialogFooter>
            {manifestDialog.content && (
              <ManifestCopyButton content={manifestDialog.content} t={t} />
            )}
            <Button variant="outline" onClick={() => setManifestDialog({ open: false })}>
              {t('packages.manifest_close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <RepositoryDialogs
        repoDialog={repoDialog}
        setRepoDialog={setRepoDialog}
        handleAddRepo={handleAddRepo}
        deleteRepoConfirm={deleteRepoConfirm}
        setDeleteRepoConfirm={setDeleteRepoConfirm}
        handleRemoveRepo={handleRemoveRepo}
      />

      {/* Version Select Dialog */}
      <Dialog
        open={versionSelectDialog.open}
        onOpenChange={(v) => !v && setVersionSelectDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('packages.version_select_title')}</DialogTitle>
            <DialogDescription>{t('packages.version_select_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>{t('packages.version_label')}</Label>
              <Select
                value={versionSelectDialog.selectedVersion || ''}
                onValueChange={(v) =>
                  setVersionSelectDialog((prev) => ({ ...prev, selectedVersion: v }))
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t('packages.version_placeholder')} />
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
              {t('packages.cancel_btn')}
            </Button>
            <Button
              onClick={() => {
                const { repo, name, selectedVersion } = versionSelectDialog
                setVersionSelectDialog({ open: false })
                handleShowPreview(repo, name, selectedVersion)
              }}
            >
              {t('packages.install_btn')}
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
            <DialogTitle>{t('packages.uninstall_dialog_title')}</DialogTitle>
            <DialogDescription>{t('packages.uninstall_dialog_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <p className="text-sm text-muted-foreground">
              {t('packages.uninstall_btn')}{' '}
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
              {t('packages.purge_volumes_label')}
            </label>
            {purgeVolumes && (
              <p className="text-sm text-destructive">
                {t('packages.purge_volumes_warning')}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setUninstallConfirm(null)}>
              {t('packages.cancel_btn')}
            </Button>
            <Button variant="destructive" onClick={handleUninstall}>
              {t('packages.uninstall_btn')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Progress Dialog */}
      <Dialog open={progressDialog.open}>
        <DialogContent className="sm:max-w-md" onPointerDownOutside={(e) => e.preventDefault()}>
          <DialogHeader>
            <DialogTitle>
              {progressDialog.action === 'install' && t('progress.title_installing')}
              {progressDialog.action === 'uninstall' && t('progress.title_uninstalling')}
              {progressDialog.action === 'refresh' && t('progress.title_refreshing')}
            </DialogTitle>
            <DialogDescription>{t('progress.description')}</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-3 py-4">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            <span className="text-sm">
              {progressDialog.step ? t(`progress.${progressDialog.step}`) : t('progress.starting')}
            </span>
          </div>
        </DialogContent>
      </Dialog>

    </div>
  )
}

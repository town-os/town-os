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
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Plus, Trash2, FolderGit2, RefreshCw, AlertCircle, CheckCircle2 } from 'lucide-react'

export default function PackageManagement() {
  useEffect(() => { document.title = 'Town OS - Packages' }, [])
  const [refreshKey, setRefreshKey] = useState(0)

  // Package state
  const [installConfirm, setInstallConfirm] = useState(null)
  const [uninstallConfirm, setUninstallConfirm] = useState(null)
  const [questionsDialog, setQuestionsDialog] = useState({ open: false })

  // Repository state
  const [repoDialog, setRepoDialog] = useState(false)
  const [deleteRepoConfirm, setDeleteRepoConfirm] = useState(null)

  // Sort state for packages tab
  const [pkgSortKey, setPkgSortKey] = useState('name')
  const [pkgSortDirection, setPkgSortDirection] = useState('asc')

  // Sort state for repositories tab
  const [repoSortKey, setRepoSortKey] = useState('name')
  const [repoSortDirection, setRepoSortDirection] = useState('asc')

  const PAGE_SIZE = 20

  const [pkgPage, setPkgPage] = useState(0)
  const [repoPage, setRepoPage] = useState(0)
  const [pkgSearch, setPkgSearch] = useState('')
  const [repoSearch, setRepoSearch] = useState('')

  const [pkgData, , pkgLoading] = usePolling(
    () => getClient().listPackages(pkgSortKey, pkgSortDirection, PAGE_SIZE, pkgPage * PAGE_SIZE, pkgSearch || undefined),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, pkgSortKey, pkgSortDirection, pkgPage, pkgSearch],
  )
  const packages = pkgData.entries || []

  const [installedData, , installedLoading] = usePolling(
    () => getClient().listInstalled(pkgSortKey, pkgSortDirection, PAGE_SIZE, pkgPage * PAGE_SIZE, pkgSearch || undefined),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, pkgSortKey, pkgSortDirection, pkgPage, pkgSearch],
  )
  const installed = installedData.entries || []

  const [repoData, , repoLoading] = usePolling(
    () => getClient().listRepositories(repoSortKey, repoSortDirection, PAGE_SIZE, repoPage * PAGE_SIZE, repoSearch || undefined),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, repoSortKey, repoSortDirection, repoPage, repoSearch],
  )
  const repositories = repoData.entries || []

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  function isInstalled(name) {
    return (installed || []).some(
      (pkg) => pkg === name || pkg.startsWith(name + '@'),
    )
  }

  async function handleInstall(name, version) {
    try {
      // Fetch questions for this specific package version.
      const questions = await getClient().getPackageQuestionsByIdentity(name, version)

      // Get existing responses to use as defaults.
      let existingResponses = {}
      try {
        existingResponses = await getClient().getResponses(name, version)
      } catch {
        // no existing responses
      }

      if (questions && Object.keys(questions).length > 0) {
        setQuestionsDialog({
          open: true,
          name,
          version,
          questions,
          responses: existingResponses || {},
          fieldErrors: {},
        })
        return
      }

      // No questions — install directly.
      await getClient().installPackage(name, version, {})
      toast.success(`Package "${name}" installed`)
      doRefresh()
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
        questionsDialog.name,
        questionsDialog.version,
        responses,
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
        uninstallConfirm.name,
        uninstallConfirm.version,
      )
      toast.success(`Package "${uninstallConfirm.name}" uninstalled`)
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

  const packageColumns = [
    { key: 'name', label: 'Name' },
    {
      key: 'version',
      label: 'Version',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: '_status',
      label: 'Installation Status',
      sortable: false,
      className: 'text-right',
      transform: (_, row) => {
        const inst = isInstalled(row.name)
        return (
          <div className="flex items-center justify-end gap-1">
            <Tooltip>
              <TooltipTrigger>
                <Badge
                  variant={inst ? 'default' : 'secondary'}
                  className="cursor-pointer"
                  onClick={() => {
                    if (inst) {
                      handleInstall(row.name, row.version)
                    } else {
                      setInstallConfirm({
                        name: row.name,
                        version: row.version,
                      })
                    }
                  }}
                >
                  {inst ? 'Installed' : 'Not Installed'}
                </Badge>
              </TooltipTrigger>
              <TooltipContent side="right">
                Click to {inst ? 'reconfigure' : 'install'}
              </TooltipContent>
            </Tooltip>
            {inst && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0 text-destructive hover:text-destructive"
                    onClick={() =>
                      setUninstallConfirm({
                        name: row.name,
                        version: row.version,
                      })
                    }
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="right">Uninstall</TooltipContent>
              </Tooltip>
            )}
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

  // Normalize packages: the backend may return either strings like "name@version"
  // or objects with {name, version}. Handle both.
  const normalizedPackages = packages.map((pkg) => {
    if (typeof pkg === 'string') {
      const [name, version] = pkg.split('@')
      return { name, version: version || '' }
    }
    return pkg
  })

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
        <TabsContent value="packages" className="mt-4">
          {pkgLoading && packages.length === 0 && (
            <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
          )}
          <DataTable
            data={normalizedPackages}
            columns={packageColumns}
            entryKey="name"
            page={pkgPage}
            setPage={setPkgPage}
            hasMore={pkgData.has_more}
            totalPages={pkgData.total_pages}
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
          {repoLoading && repositories.length === 0 && (
            <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
          )}
          <DataTable
            data={repositories}
            columns={repoColumns}
            entryKey="name"
            page={repoPage}
            setPage={setRepoPage}
            hasMore={repoData.has_more}
            totalPages={repoData.total_pages}
            sortKey={repoSortKey}
            sortDirection={repoSortDirection}
            onSortChange={(key, dir) => {
              setRepoSortKey(key)
              setRepoSortDirection(dir)
              setRepoPage(0)
            }}
            onReset={() => {
              setRepoSortKey('name')
              setRepoSortDirection('asc')
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
                    return (
                      <div key={key} className="space-y-2">
                        <Label htmlFor={key}>{question.query}</Label>
                        <Input
                          id={key}
                          name={key}
                          type={question.type === 'password' ? 'password' : 'text'}
                          defaultValue={questionsDialog.responses?.[key] || ''}
                          className={fieldError ? 'border-destructive' : ''}
                        />
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

      {/* Install Confirm */}
      <ConfirmDialog
        open={!!installConfirm}
        title="Install Package"
        onConfirm={() => {
          const { name, version } = installConfirm
          setInstallConfirm(null)
          handleInstall(name, version)
        }}
        onCancel={() => setInstallConfirm(null)}
        confirmLabel="Install"
      >
        Install{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {installConfirm?.name}@{installConfirm?.version}
        </code>
        ?
      </ConfirmDialog>

      {/* Uninstall Confirm */}
      <ConfirmDialog
        open={!!uninstallConfirm}
        title="Uninstall Package"
        onConfirm={handleUninstall}
        onCancel={() => setUninstallConfirm(null)}
        confirmLabel="Uninstall"
        variant="destructive"
      >
        Uninstall{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {uninstallConfirm?.name}@{uninstallConfirm?.version}
        </code>
        ?
      </ConfirmDialog>

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

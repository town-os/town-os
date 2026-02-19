import { useState } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { Plus, Trash2, FolderGit2 } from 'lucide-react'

export default function PackageManagement() {
  const [error, setError] = useState(null)
  const [success, setSuccess] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)

  // Package state
  const [installConfirm, setInstallConfirm] = useState(null)
  const [uninstallConfirm, setUninstallConfirm] = useState(null)
  const [questionsDialog, setQuestionsDialog] = useState({ open: false })

  // Repository state
  const [repoDialog, setRepoDialog] = useState(false)
  const [deleteRepoConfirm, setDeleteRepoConfirm] = useState(null)

  const [packages] = usePolling(
    () => getClient().listPackages(),
    [],
    [refreshKey],
  )

  const [installed] = usePolling(
    () => getClient().listInstalled(),
    [],
    [refreshKey],
  )

  const [repositories] = usePolling(
    () => getClient().listRepositories(),
    [],
    [refreshKey],
  )

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  function isInstalled(name) {
    return installed.some((pkg) => pkg === name || pkg.startsWith(name + '@'))
  }

  async function handleInstall(name, version) {
    setError(null)
    setSuccess(null)
    try {
      // Check for questions first
      const questions = await getClient().getPackageQuestions(name)
      if (questions && Object.keys(questions).length > 0) {
        // Get existing responses
        let existingResponses = {}
        try {
          existingResponses = await getClient().getResponses(name, version)
        } catch {
          // no existing responses
        }
        setQuestionsDialog({
          open: true,
          name,
          version,
          questions,
          responses: existingResponses || {},
        })
        return
      }

      await getClient().installPackage(name, version, {})
      setSuccess(`Package "${name}" installed`)
      doRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  async function handleInstallWithResponses(e) {
    e.preventDefault()
    setError(null)
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
      setSuccess(`Package "${questionsDialog.name}" installed`)
      setQuestionsDialog({ open: false })
      doRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  async function handleUninstall() {
    setError(null)
    setSuccess(null)
    try {
      await getClient().uninstallPackage(
        uninstallConfirm.name,
        uninstallConfirm.version,
      )
      setSuccess(`Package "${uninstallConfirm.name}" uninstalled`)
      setUninstallConfirm(null)
      doRefresh()
    } catch (err) {
      setError(err.message)
      setUninstallConfirm(null)
    }
  }

  async function handleAddRepo(e) {
    e.preventDefault()
    setError(null)
    setSuccess(null)
    const name = e.target.elements.name.value
    const url = e.target.elements.url.value
    try {
      await getClient().addRepository(name, url)
      setSuccess('Repository added')
      setRepoDialog(false)
      doRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  async function handleRemoveRepo() {
    setError(null)
    try {
      await getClient().removeRepository(deleteRepoConfirm)
      setSuccess(`Repository "${deleteRepoConfirm}" removed`)
      setDeleteRepoConfirm(null)
      doRefresh()
    } catch (err) {
      setError(err.message)
      setDeleteRepoConfirm(null)
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
      label: 'Status',
      sortable: false,
      transform: (_, row) => {
        const inst = isInstalled(row.name)
        return (
          <Tooltip>
            <TooltipTrigger>
              <Badge
                variant={inst ? 'default' : 'secondary'}
                className="cursor-pointer"
                onClick={() => {
                  if (inst) {
                    setUninstallConfirm({
                      name: row.name,
                      version: row.version,
                    })
                  } else {
                    handleInstall(row.name, row.version)
                  }
                }}
              >
                {inst ? 'Installed' : 'Not Installed'}
              </Badge>
            </TooltipTrigger>
            <TooltipContent>
              Click to {inst ? 'uninstall' : 'install'}
            </TooltipContent>
          </Tooltip>
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

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {success && (
        <Alert>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      )}

      <Tabs defaultValue="packages">
        <TabsList>
          <TabsTrigger value="packages">Packages</TabsTrigger>
          <TabsTrigger value="repositories">Repositories</TabsTrigger>
        </TabsList>
        <TabsContent value="packages" className="mt-4">
          <DataTable
            data={normalizedPackages}
            columns={packageColumns}
            entryKey="name"
          />
        </TabsContent>
        <TabsContent value="repositories" className="mt-4 space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => setRepoDialog(true)}>
              <Plus className="h-4 w-4 mr-1" />
              Add Repository
            </Button>
          </div>
          <DataTable
            data={repositories}
            columns={repoColumns}
            entryKey="name"
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
                  ([key, question]) => (
                    <div key={key} className="space-y-2">
                      <Label htmlFor={key}>{question.query}</Label>
                      <Input
                        id={key}
                        name={key}
                        type={question.type === 'password' ? 'password' : 'text'}
                        defaultValue={questionsDialog.responses?.[key] || ''}
                      />
                    </div>
                  ),
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

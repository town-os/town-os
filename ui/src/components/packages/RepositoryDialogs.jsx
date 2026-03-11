import { useI18n } from '@/i18n/I18nContext.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { FolderGit2 } from 'lucide-react'

export default function RepositoryDialogs({
  repoDialog,
  setRepoDialog,
  handleAddRepo,
  deleteRepoConfirm,
  setDeleteRepoConfirm,
  handleRemoveRepo,
}) {
  const { t } = useI18n()

  return (
    <>
      {/* Add Repository Dialog */}
      <Dialog open={repoDialog} onOpenChange={setRepoDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <FolderGit2 className="h-4 w-4 inline mr-2" />
              {t('packages.add_repo_dialog_title')}
            </DialogTitle>
            <DialogDescription>{t('packages.add_repo_dialog_description')}</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleAddRepo}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">{t('packages.repo_name_label')}</Label>
                <Input
                  id="name"
                  name="name"
                  placeholder={t('packages.repo_name_placeholder')}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="url">{t('packages.repo_url_label')}</Label>
                <Input
                  id="url"
                  name="url"
                  placeholder={t('packages.repo_url_placeholder')}
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
                {t('packages.cancel_btn')}
              </Button>
              <Button type="submit">{t('packages.add_btn')}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Remove Repo Confirm */}
      <ConfirmDialog
        open={!!deleteRepoConfirm}
        title={t('packages.remove_repo_dialog_title')}
        onConfirm={handleRemoveRepo}
        onCancel={() => setDeleteRepoConfirm(null)}
        confirmLabel={t('packages.remove_confirm_btn')}
        variant="destructive"
      >
        {t('packages.remove_confirm_btn')}{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {deleteRepoConfirm}
        </code>
        ?
      </ConfirmDialog>
    </>
  )
}

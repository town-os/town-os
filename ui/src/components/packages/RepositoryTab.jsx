import { useI18n } from '@/i18n/I18nContext.jsx'
import DataTable from '@/components/DataTable.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { RefreshCw, Plus } from 'lucide-react'

export default function RepositoryTab({
  repositories,
  repoColumns,
  repoPage,
  setRepoPage,
  repoData,
  repoSortKey,
  repoSortDirection,
  setRepoSortKey,
  setRepoSortDirection,
  setRepoSearch,
  repoLoading,
  handleRefreshRepos,
  refreshing,
  setRepoDialog,
  displayRepos,
}) {
  const { t } = useI18n()

  return (
    <>
      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={handleRefreshRepos} disabled={refreshing}>
          <RefreshCw className={`h-4 w-4 mr-1${refreshing ? ' animate-spin' : ''}`} />
          {refreshing ? t('packages.refreshing_btn') : t('packages.refresh_btn')}
        </Button>
        <Button onClick={() => setRepoDialog(true)}>
          <Plus className="h-4 w-4 mr-1" />
          {t('packages.add_repo_btn')}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        {t('packages.repo_priority_hint')}
      </p>
      {repoLoading && repositories.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('packages.loading')}</div>
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
    </>
  )
}

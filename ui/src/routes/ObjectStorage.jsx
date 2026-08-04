import { useEffect } from 'react'
import { Database } from 'lucide-react'

import { useRequireAuth } from '@/lib/hooks.js'
import { useI18n } from '@/i18n/I18nContext.jsx'

import ObjectStoragePanel from './objects/ObjectStoragePanel.jsx'

/**
 * Object storage: one gfeh partition per network.
 *
 * The page is a heading over the shared panel, so this screen and the object
 * storage section of the services screen can never drift into showing different
 * things about the same partition.
 *
 * It keeps `?tab=` for its sub-tab — the panel's default — so links written
 * against this page go on working.
 */
export default function ObjectStorage() {
  const { t } = useI18n()
  const account = useRequireAuth()

  useEffect(() => {
    document.title = t('objects.page_title')
  }, [t])

  return (
    <div className="space-y-4">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          <Database className="h-6 w-6" />
          {t('objects.title')}
        </h1>
      </div>
      <ObjectStoragePanel account={account} />
    </div>
  )
}

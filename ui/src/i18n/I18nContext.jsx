import { createContext, useCallback, useContext, useState } from 'react'
import enUS from './en-US.js'

/** @type {Record<string, Record<string, string>>} */
const catalogs = {
  'en-US': enUS,
}

const defaultLocale = 'en-US'

const I18nContext = createContext({
  locale: defaultLocale,
  setLocale: /** @param {string} l */ () => {},
  t: /** @param {string} _k @param {Record<string, any>} [_p] @returns {string} */ (k, p) => translate(defaultLocale, k, p),
})

/**
 * Translate a message key using the current locale catalog.
 * Supports simple {name} interpolation for dynamic values.
 *
 * @param {string} locale - The current locale code (e.g. "en-US").
 * @param {string} key - The message key (e.g. "login.title").
 * @param {Record<string, any>} [params] - Optional interpolation values.
 * @returns {string} Translated string, or the key itself if not found.
 */
function translate(locale, key, params) {
  const catalog = catalogs[locale] || catalogs[defaultLocale]
  let msg = catalog?.[key]
  if (msg === undefined) {
    // Fall back to default locale, then to the key itself.
    msg = catalogs[defaultLocale]?.[key] ?? key
  }
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      msg = msg.replaceAll(`{${k}}`, String(v))
    }
  }
  return msg
}

/**
 * Provider that supplies locale state and the t() translation function
 * to the component tree. Wrap your app in this provider.
 *
 * @param {{ children: React.ReactNode, initialLocale?: string }} props
 */
export function I18nProvider({ children, initialLocale }) {
  const [locale, setLocale] = useState(initialLocale || defaultLocale)

  const t = useCallback(
    (key, params) => translate(locale, key, params),
    [locale],
  )

  return (
    <I18nContext.Provider value={{ locale, setLocale, t }}>
      {children}
    </I18nContext.Provider>
  )
}

/**
 * Hook to access the current locale, setLocale, and t() function.
 *
 * @returns {{ locale: string, setLocale: (locale: string) => void, t: (key: string, params?: Record<string, any>) => string }}
 */
// eslint-disable-next-line react-refresh/only-export-components
export function useI18n() {
  return useContext(I18nContext)
}

// eslint-disable-next-line react-refresh/only-export-components
export { defaultLocale, catalogs }

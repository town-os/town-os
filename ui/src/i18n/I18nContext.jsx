import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
import enUS from './en-US.js'
import arSA from './ar-SA.js'
import bnBD from './bn-BD.js'
import daDK from './da-DK.js'
import deDE from './de-DE.js'
import esES from './es-ES.js'
import fiFI from './fi-FI.js'
import frFR from './fr-FR.js'
import hiIN from './hi-IN.js'
import itIT from './it-IT.js'
import jaJP from './ja-JP.js'
import koKR from './ko-KR.js'
import nlNL from './nl-NL.js'
import plPL from './pl-PL.js'
import ptBR from './pt-BR.js'
import ruRU from './ru-RU.js'
import saIN from './sa-IN.js'
import svSE from './sv-SE.js'
import thTH from './th-TH.js'
import trTR from './tr-TR.js'
import ukUA from './uk-UA.js'
import viVN from './vi-VN.js'
import zhCN from './zh-CN.js'
import zhTW from './zh-TW.js'

/** @type {Record<string, Record<string, string>>} */
const catalogs = {
  'en-US': enUS,
  'ar-SA': arSA,
  'bn-BD': bnBD,
  'da-DK': daDK,
  'de-DE': deDE,
  'es-ES': esES,
  'fi-FI': fiFI,
  'fr-FR': frFR,
  'hi-IN': hiIN,
  'it-IT': itIT,
  'ja-JP': jaJP,
  'ko-KR': koKR,
  'nl-NL': nlNL,
  'pl-PL': plPL,
  'pt-BR': ptBR,
  'ru-RU': ruRU,
  'sa-IN': saIN,
  'sv-SE': svSE,
  'th-TH': thTH,
  'tr-TR': trTR,
  'uk-UA': ukUA,
  'vi-VN': viVN,
  'zh-CN': zhCN,
  'zh-TW': zhTW,
}

const defaultLocale = 'en-US'

// localStorage key holding an explicit, per-browser language choice. When set,
// it wins over browser auto-detection and over the server's global `locale`
// setting so a deliberate pick is not undone on the next load or ping.
const STORAGE_KEY = 'townos.locale'

/**
 * Match an ordered list of preferred locale tags against the available
 * catalog codes. Tries exact (case-insensitive) matches first, then falls
 * back to the primary language subtag (so `de-AT` resolves to `de-DE`).
 * Chinese is disambiguated by script/region so `zh-HK`/`zh-Hant` prefer
 * Traditional and `zh`/`zh-CN` prefer Simplified.
 *
 * @param {string[]} prefs - Ordered preferred locale tags (e.g. navigator.languages).
 * @param {Record<string, unknown>} available - Catalogs keyed by locale code.
 * @returns {string|null} The best matching catalog code, or null if none match.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function matchLocale(prefs, available) {
  const codes = Object.keys(available)
  const lower = codes.map((c) => [c, c.toLowerCase()])
  // 1. Exact match, in preference order.
  for (const pref of prefs || []) {
    const p = String(pref).toLowerCase()
    const hit = lower.find(([, l]) => l === p)
    if (hit) return hit[0]
  }
  // 2. Primary-subtag match, in preference order.
  for (const pref of prefs || []) {
    const p = String(pref).toLowerCase()
    const base = p.split('-')[0]
    if (base === 'zh') {
      const traditional = /hant/.test(p) || /(^|-)(tw|hk|mo)($|-)/.test(p)
      const want = traditional ? 'zh-tw' : 'zh-cn'
      const zh = lower.find(([, l]) => l === want) || lower.find(([, l]) => l.startsWith('zh'))
      if (zh) return zh[0]
      continue
    }
    const hit = lower.find(([, l]) => l.split('-')[0] === base)
    if (hit) return hit[0]
  }
  return null
}

/** Read the browser's ordered language preferences. */
function browserPrefs() {
  if (typeof navigator === 'undefined') return []
  if (navigator.languages && navigator.languages.length) return [...navigator.languages]
  return navigator.language ? [navigator.language] : []
}

/**
 * Detect the best available locale from the browser's language preferences.
 *
 * @param {Record<string, unknown>} [available] - Catalogs keyed by locale code.
 * @returns {string|null} The matching catalog code, or null if none match.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function detectBrowserLocale(available = catalogs) {
  return matchLocale(browserPrefs(), available)
}

function readStoredLocale() {
  try {
    if (typeof localStorage === 'undefined') return null
    const v = localStorage.getItem(STORAGE_KEY)
    return v && catalogs[v] ? v : null
  } catch {
    return null
  }
}

function writeStoredLocale(code) {
  try {
    if (typeof localStorage !== 'undefined') localStorage.setItem(STORAGE_KEY, code)
  } catch {
    // Storage unavailable (private mode, disabled) — non-fatal.
  }
}

/**
 * Resolve the locale to start with, and whether it is "pinned" (a hard choice
 * that the server's global setting must not override). Precedence:
 *   1. explicit prop (tests) or stored per-browser choice — pinned
 *   2. browser-detected language matched to a shipped catalog — pinned
 *   3. server global setting (applied later via syncServerLocale) / en-US — not pinned
 *
 * @param {string} [initialLocale]
 * @returns {{ locale: string, pinned: boolean }}
 */
function resolveInitialLocale(initialLocale) {
  if (initialLocale) return { locale: initialLocale, pinned: true }
  const stored = readStoredLocale()
  if (stored) return { locale: stored, pinned: true }
  const detected = detectBrowserLocale()
  if (detected) return { locale: detected, pinned: true }
  return { locale: defaultLocale, pinned: false }
}

const I18nContext = createContext({
  locale: defaultLocale,
  setLocale: /** @param {string} l */ () => {},
  syncServerLocale: /** @param {string} l */ () => {},
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
 * The active locale is chosen from the browser's own language preferences by
 * default; the server's global `locale` setting only applies as a fallback
 * when the browser's language is not one we ship a catalog for.
 *
 * @param {{ children: React.ReactNode, initialLocale?: string }} props
 */
export function I18nProvider({ children, initialLocale }) {
  const initial = useMemo(() => resolveInitialLocale(initialLocale), [initialLocale])
  const [locale, setLocaleState] = useState(initial.locale)
  // True once the locale is fixed by an explicit choice or a browser match,
  // so a background server-locale sync must not clobber it.
  const pinnedRef = useRef(initial.pinned)

  // Explicit, user-visible language choice (e.g. the settings dropdown):
  // pin it and remember it for this browser.
  const setLocale = useCallback((code) => {
    pinnedRef.current = true
    writeStoredLocale(code)
    setLocaleState(code)
  }, [])

  // Apply the server's global `locale` setting, but only while the locale is
  // not pinned by the browser's own preference or an explicit choice.
  const syncServerLocale = useCallback((code) => {
    if (pinnedRef.current || !code) return
    setLocaleState(code)
  }, [])

  const t = useCallback(
    (key, params) => translate(locale, key, params),
    [locale],
  )

  return (
    <I18nContext.Provider value={{ locale, setLocale, syncServerLocale, t }}>
      {children}
    </I18nContext.Provider>
  )
}

/**
 * Hook to access the current locale, setLocale, syncServerLocale, and t().
 *
 * @returns {{ locale: string, setLocale: (locale: string) => void, syncServerLocale: (locale: string) => void, t: (key: string, params?: Record<string, any>) => string }}
 */
// eslint-disable-next-line react-refresh/only-export-components
export function useI18n() {
  return useContext(I18nContext)
}

// eslint-disable-next-line react-refresh/only-export-components
export { defaultLocale, catalogs }

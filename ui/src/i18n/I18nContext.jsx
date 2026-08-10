import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
import enUS from './en-US.js'
import arAE from './ar-AE.js'
import arEG from './ar-EG.js'
import arSA from './ar-SA.js'
import bnBD from './bn-BD.js'
import bnIN from './bn-IN.js'
import csCZ from './cs-CZ.js'
import daDK from './da-DK.js'
import deAT from './de-AT.js'
import deCH from './de-CH.js'
import deDE from './de-DE.js'
import enAU from './en-AU.js'
import enCA from './en-CA.js'
import enGB from './en-GB.js'
import enIN from './en-IN.js'
import enNZ from './en-NZ.js'
import enZA from './en-ZA.js'
import esAR from './es-AR.js'
import esES from './es-ES.js'
import esMX from './es-MX.js'
import fiFI from './fi-FI.js'
import frBE from './fr-BE.js'
import frCA from './fr-CA.js'
import frCH from './fr-CH.js'
import frFR from './fr-FR.js'
import hiIN from './hi-IN.js'
import hrHR from './hr-HR.js'
import huHU from './hu-HU.js'
import itIT from './it-IT.js'
import jaJP from './ja-JP.js'
import koKR from './ko-KR.js'
import nlBE from './nl-BE.js'
import nlNL from './nl-NL.js'
import plPL from './pl-PL.js'
import ptBR from './pt-BR.js'
import ptPT from './pt-PT.js'
import roRO from './ro-RO.js'
import ruRU from './ru-RU.js'
import saIN from './sa-IN.js'
import skSK from './sk-SK.js'
import slSI from './sl-SI.js'
import svSE from './sv-SE.js'
import thTH from './th-TH.js'
import trTR from './tr-TR.js'
import ukUA from './uk-UA.js'
import viVN from './vi-VN.js'
import zhCN from './zh-CN.js'
import zhTW from './zh-TW.js'

/**
 * Every shipped catalog, keyed by locale code.
 *
 * Entries are of two kinds. A language catalog is a translation, written out in
 * full in its own file. A country catalog is built by `derive()` from the
 * language it belongs to plus the strings that country states differently — see
 * derive.js. Both kinds are selectable and both count as populated; only the
 * way the file is written differs.
 *
 * @type {Record<string, Record<string, string>>}
 */
const catalogs = {
  'en-US': enUS,
  'ar-AE': arAE,
  'ar-EG': arEG,
  'ar-SA': arSA,
  'bn-BD': bnBD,
  'bn-IN': bnIN,
  'cs-CZ': csCZ,
  'da-DK': daDK,
  'de-AT': deAT,
  'de-CH': deCH,
  'de-DE': deDE,
  'en-AU': enAU,
  'en-CA': enCA,
  'en-GB': enGB,
  'en-IN': enIN,
  'en-NZ': enNZ,
  'en-ZA': enZA,
  'es-AR': esAR,
  'es-ES': esES,
  'es-MX': esMX,
  'fi-FI': fiFI,
  'fr-BE': frBE,
  'fr-CA': frCA,
  'fr-CH': frCH,
  'fr-FR': frFR,
  'hi-IN': hiIN,
  'hr-HR': hrHR,
  'hu-HU': huHU,
  'it-IT': itIT,
  'ja-JP': jaJP,
  'ko-KR': koKR,
  'nl-BE': nlBE,
  'nl-NL': nlNL,
  'pl-PL': plPL,
  'pt-BR': ptBR,
  'pt-PT': ptPT,
  'ro-RO': roRO,
  'ru-RU': ruRU,
  'sa-IN': saIN,
  'sk-SK': skSK,
  'sl-SI': slSI,
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
 * The catalog a bare language tag — or a country we ship no catalog for —
 * should fall back to, keyed by primary subtag.
 *
 * This map exists because the fallback used to be "the first catalog whose
 * primary subtag matches", and that was only ever correct while each language
 * had exactly one catalog. Eight now ship more than one: a browser asking for
 * plain `en`, or for `en-PH`, would otherwise land on whichever English happens
 * to be declared first in `catalogs`, making the answer a property of import
 * order rather than a decision anyone made.
 *
 * Chinese is absent deliberately — it is resolved by script below, which is a
 * stronger signal than a default.
 */
const languageDefaults = {
  ar: 'ar-SA',
  bn: 'bn-BD',
  de: 'de-DE',
  en: 'en-US',
  es: 'es-ES',
  fr: 'fr-FR',
  nl: 'nl-NL',
  pt: 'pt-BR',
}

/**
 * Countries we ship no catalog for that belong with a variant rather than with
 * their language's default: Spanish-speaking Latin America reads American
 * Spanish, Lusophone Africa and Timor read European Portuguese, and the
 * Englishes of Ireland, Africa and South and Southeast Asia follow British
 * spelling. Without this, es-CO would get peninsular Spanish and en-IE would
 * get American.
 */
const regionDefaults = {
  es: [/^(ar|bo|cl|co|cr|cu|do|ec|gt|hn|mx|ni|pa|pe|pr|py|sv|uy|ve)$/, 'es-MX'],
  pt: [/^(ao|cv|gw|mz|st|tl)$/, 'pt-PT'],
  en: [/^(ie|gh|ke|lk|my|ng|pk|sg|tz|ug|zm|zw)$/, 'en-GB'],
}

/**
 * Resolve the catalog code a preference should fall back to when no catalog
 * matches it exactly.
 *
 * @param {string} pref - A lowercased preference tag (e.g. "en-ph").
 * @returns {string|null} A locale code to look for, or null if there is no
 *   better answer than "any catalog in this language".
 */
function preferredFallback(pref) {
  const [base, region] = pref.split('-')
  const regional = regionDefaults[base]
  if (regional && region && regional[0].test(region)) return regional[1]
  return languageDefaults[base] ?? null
}

/**
 * Match an ordered list of preferred locale tags against the available
 * catalog codes. Tries exact (case-insensitive) matches first, then falls back
 * to a named default for the primary language subtag (so `de-LU` resolves to
 * `de-DE` and `es-CO` to `es-MX`), and only then to any catalog sharing the
 * subtag. Chinese is disambiguated by script/region so `zh-HK`/`zh-Hant` prefer
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
    // Prefer the named default for this language before settling for whichever
    // catalog in it comes first.
    const fallback = preferredFallback(p)
    if (fallback) {
      const named = lower.find(([, l]) => l === fallback.toLowerCase())
      if (named) return named[0]
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

import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { useEffect } from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import {
  I18nProvider,
  useI18n,
  defaultLocale,
  catalogs,
  matchLocale,
  detectBrowserLocale,
  translateIn,
} from './I18nContext.jsx'

/** Override the browser's reported language preferences for a test. */
function setNavLanguages(langs) {
  Object.defineProperty(window.navigator, 'languages', { value: langs, configurable: true })
  Object.defineProperty(window.navigator, 'language', { value: langs[0], configurable: true })
}

/** Drives syncServerLocale, mimicking the Dashboard ping-sync effect. */
function ServerSync({ code }) {
  const { syncServerLocale, locale } = useI18n()
  useEffect(() => {
    syncServerLocale(code)
  }, [code, syncServerLocale])
  return <span data-testid="locale">{locale}</span>
}

function TranslateDisplay({ msgKey, params }) {
  const { t, locale } = useI18n()
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <span data-testid="msg">{t(msgKey, params)}</span>
    </div>
  )
}

function LocaleSwitcher() {
  const { locale, setLocale, t } = useI18n()
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <button onClick={() => setLocale('de-DE')}>switch</button>
      <span data-testid="msg">{t('login.title')}</span>
    </div>
  )
}

describe('I18nContext', () => {
  beforeEach(() => {
    // Each test starts with no stored per-browser choice and the jsdom
    // default language, so detection and persistence tests do not leak.
    try {
      localStorage.clear()
    } catch {
      // ignore
    }
    setNavLanguages(['en-US'])
  })

  afterEach(() => {
    setNavLanguages(['en-US'])
  })

  it('defaultLocale is en-US', () => {
    expect(defaultLocale).toBe('en-US')
  })

  it('catalogs contains en-US', () => {
    expect(catalogs['en-US']).toBeDefined()
    expect(typeof catalogs['en-US']).toBe('object')
  })

  it('provides default locale via context', () => {
    render(
      <I18nProvider>
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('en-US')
  })

  it('translates a known key', () => {
    render(
      <I18nProvider>
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe('Town OS')
  })

  it('returns key itself for unknown keys', () => {
    render(
      <I18nProvider>
        <TranslateDisplay msgKey="nonexistent.key" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe('nonexistent.key')
  })

  it('interpolates {name} parameters', () => {
    render(
      <I18nProvider>
        <TranslateDisplay
          msgKey="dashboard.stat_installed_count"
          params={{ count: 42 }}
        />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe('42 installed')
  })

  it('supports multiple parameters', () => {
    render(
      <I18nProvider>
        <TranslateDisplay
          msgKey="dashboard.stat_disk_usage"
          params={{ used: '10 GB', total: '50 GB' }}
        />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe('10 GB / 50 GB used')
  })

  it('respects initialLocale prop', () => {
    render(
      <I18nProvider initialLocale="de-DE">
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('de-DE')
  })

  it('falls back to en-US for unknown locale', () => {
    render(
      <I18nProvider initialLocale="xx-XX">
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    // Should fall back to en-US catalog
    expect(screen.getByTestId('msg').textContent).toBe('Town OS')
  })

  it('setLocale updates locale in context', () => {
    render(
      <I18nProvider>
        <LocaleSwitcher />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('en-US')
    fireEvent.click(screen.getByText('switch'))
    expect(screen.getByTestId('locale').textContent).toBe('de-DE')
  })

  it('translates plural suffix parameters', () => {
    render(
      <I18nProvider>
        <TranslateDisplay
          msgKey="dashboard.upgrade_available"
          params={{ count: 3, s: 's' }}
        />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe('3 package upgrades available')
  })

  it('translates singular suffix parameters', () => {
    render(
      <I18nProvider>
        <TranslateDisplay
          msgKey="dashboard.upgrade_available"
          params={{ count: 1, s: '' }}
        />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe('1 package upgrade available')
  })

  it('handles missing parameters gracefully', () => {
    render(
      <I18nProvider>
        <TranslateDisplay
          msgKey="dashboard.stat_installed_count"
        />
      </I18nProvider>,
    )
    // Without params, {count} remains in the string
    expect(screen.getByTestId('msg').textContent).toBe('{count} installed')
  })

  it('auto-detects the locale from the browser languages', () => {
    setNavLanguages(['de-DE', 'de', 'en-US'])
    render(
      <I18nProvider>
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('de-DE')
  })

  it('resolves a region variant we ship nothing for to its language (de-LU -> de-DE)', () => {
    // de-LU, not de-AT: Austria ships its own catalog now, so de-AT is an exact
    // match and proves nothing about folding. This has to stay a country we
    // genuinely do not ship, or the test passes for the wrong reason.
    setNavLanguages(['de-LU'])
    render(
      <I18nProvider>
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('de-DE')
  })

  it('resolves a region variant we do ship to that variant (de-AT stays de-AT)', () => {
    setNavLanguages(['de-AT'])
    render(
      <I18nProvider>
        <TranslateDisplay msgKey="login.title" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('de-AT')
  })

  it('a browser-detected locale is pinned and not overridden by the server setting', () => {
    setNavLanguages(['fr-FR'])
    render(
      <I18nProvider>
        <ServerSync code="ru-RU" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('fr-FR')
  })

  it('applies the server locale only when the browser language is not shipped', () => {
    setNavLanguages(['af-ZA']) // no Afrikaans catalog -> not pinned
    render(
      <I18nProvider>
        <ServerSync code="ru-RU" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('ru-RU')
  })

  it('persists an explicit choice and it wins over the browser on next load', () => {
    setNavLanguages(['de-DE'])
    function Picker() {
      const { locale, setLocale } = useI18n()
      return (
        <div>
          <span data-testid="locale">{locale}</span>
          <button onClick={() => setLocale('ja-JP')}>pick</button>
        </div>
      )
    }

    const first = render(
      <I18nProvider>
        <Picker />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('de-DE') // from browser
    fireEvent.click(screen.getByText('pick'))
    expect(screen.getByTestId('locale').textContent).toBe('ja-JP')
    expect(localStorage.getItem('townos.locale')).toBe('ja-JP')
    first.unmount()

    // Remount with the same browser language: the stored choice still wins.
    render(
      <I18nProvider>
        <Picker />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale').textContent).toBe('ja-JP')
  })
})

describe('matchLocale', () => {
  it('matches an exact code case-insensitively', () => {
    expect(matchLocale(['FR-fr'], catalogs)).toBe('fr-FR')
  })

  it('prefers an exact country catalog over its base language', () => {
    // de-CH used to fall back to de-DE because Switzerland shipped no catalog.
    // It has one now, and the exact match must win.
    expect(matchLocale(['de-CH', 'de'], catalogs)).toBe('de-CH')
  })

  it('falls back to the primary language subtag', () => {
    expect(matchLocale(['de-LU', 'de'], catalogs)).toBe('de-DE')
  })

  it('resolves a bare language tag to a named default, not to import order', () => {
    // Eight languages now ship more than one catalog. Without a named default
    // these answers would be whichever variant `catalogs` happens to declare
    // first — a property of the import block rather than a decision.
    expect(matchLocale(['en'], catalogs)).toBe('en-US')
    expect(matchLocale(['de'], catalogs)).toBe('de-DE')
    expect(matchLocale(['fr'], catalogs)).toBe('fr-FR')
    expect(matchLocale(['es'], catalogs)).toBe('es-ES')
    expect(matchLocale(['pt'], catalogs)).toBe('pt-BR')
    expect(matchLocale(['nl'], catalogs)).toBe('nl-NL')
    expect(matchLocale(['ar'], catalogs)).toBe('ar-SA')
    expect(matchLocale(['bn'], catalogs)).toBe('bn-BD')
  })

  it('routes an unshipped country to the variant it reads, not the language default', () => {
    // Latin America reads American Spanish, not peninsular.
    expect(matchLocale(['es-CO'], catalogs)).toBe('es-MX')
    expect(matchLocale(['es-CL'], catalogs)).toBe('es-MX')
    // Lusophone Africa reads European Portuguese, not Brazilian.
    expect(matchLocale(['pt-AO'], catalogs)).toBe('pt-PT')
    // Ireland and Kenya follow British spelling, not American.
    expect(matchLocale(['en-IE'], catalogs)).toBe('en-GB')
    expect(matchLocale(['en-KE'], catalogs)).toBe('en-GB')
    // The Philippines follow American spelling.
    expect(matchLocale(['en-PH'], catalogs)).toBe('en-US')
  })

  it('honors browser preference order', () => {
    expect(matchLocale(['xx-YY', 'ko-KR', 'de-DE'], catalogs)).toBe('ko-KR')
  })

  it('disambiguates Chinese by script/region', () => {
    expect(matchLocale(['zh-TW'], catalogs)).toBe('zh-TW')
    expect(matchLocale(['zh-HK'], catalogs)).toBe('zh-TW')
    expect(matchLocale(['zh-Hant'], catalogs)).toBe('zh-TW')
    expect(matchLocale(['zh'], catalogs)).toBe('zh-CN')
    expect(matchLocale(['zh-CN'], catalogs)).toBe('zh-CN')
  })

  it('returns null when nothing is shipped for the language', () => {
    // Deliberately reserved codes rather than real ones: this assertion used to
    // name af-ZA and zu-ZA, which meant it would start failing the day either
    // language got a catalog — turning "we ship no Afrikaans" into a test
    // guarding that we never do.
    expect(matchLocale(['qq-QQ', 'xx-YY'], catalogs)).toBeNull()
    expect(matchLocale([], catalogs)).toBeNull()
  })

  it('detectBrowserLocale reads navigator.languages', () => {
    setNavLanguages(['ja-JP'])
    expect(detectBrowserLocale()).toBe('ja-JP')
    setNavLanguages(['en-US'])
  })
})

// Translating in a locale that is not the active one.
//
// `t()` is bound to the render it came from, which is the right answer for
// everything on the page and the wrong one for the message that announces a
// language change: by the time it is read, the UI is in the new language and
// the toast would be the only thing still written in the old one.
describe('translateIn', () => {
  it('translates in the named locale, not the default', () => {
    expect(translateIn('de-DE', 'settings.toast_language_updated')).toBe('Sprache aktualisiert')
    expect(translateIn('es-ES', 'settings.toast_language_updated')).toBe('Idioma actualizado')
    expect(translateIn('en-US', 'settings.toast_language_updated')).toBe('Language updated')
  })

  it('agrees with the catalog it names', () => {
    for (const code of ['de-DE', 'es-ES', 'fr-FR', 'ja-JP', 'zh-CN']) {
      expect(translateIn(code, 'settings.toast_language_updated'))
        .toBe(catalogs[code]['settings.toast_language_updated'])
    }
  })

  // A derived country catalog inherits its base, so asking for one must not
  // silently drop to English.
  it('resolves a derived country locale through its base', () => {
    expect(translateIn('de-AT', 'settings.toast_language_updated'))
      .toBe(catalogs['de-AT']['settings.toast_language_updated'])
    expect(translateIn('de-AT', 'settings.toast_language_updated')).not.toBe('Language updated')
  })

  it('interpolates parameters', () => {
    expect(translateIn('en-US', 'dashboard.stat_installed_count', { count: 42 })).toBe('42 installed')
  })

  it('falls back to en-US for an unknown locale', () => {
    expect(translateIn('xx-XX', 'settings.toast_language_updated')).toBe('Language updated')
  })

  it('falls back to en-US for a key a catalog is missing', () => {
    // Fabricated key: no catalog carries it, so both the requested locale and
    // the en-US fallback miss and the key itself comes back.
    expect(translateIn('de-DE', 'nonexistent.key')).toBe('nonexistent.key')
  })

  it('does not depend on the active locale', () => {
    render(
      <I18nProvider initialLocale="ja-JP">
        <TranslateDisplay msgKey="settings.toast_language_updated" />
      </I18nProvider>,
    )
    expect(screen.getByTestId('msg').textContent).toBe(catalogs['ja-JP']['settings.toast_language_updated'])
    expect(translateIn('de-DE', 'settings.toast_language_updated')).toBe('Sprache aktualisiert')
  })
})

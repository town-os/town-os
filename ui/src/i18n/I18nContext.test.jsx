import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { I18nProvider, useI18n, defaultLocale, catalogs } from './I18nContext.jsx'

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
})

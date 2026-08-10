import { describe, it, expect } from 'vitest'
import derive from './derive.js'
import { catalogs } from './I18nContext.jsx'

import enUS from './en-US.js'
import arSA from './ar-SA.js'
import bnBD from './bn-BD.js'
import deDE from './de-DE.js'
import esES from './es-ES.js'
import frFR from './fr-FR.js'
import nlNL from './nl-NL.js'
import ptBR from './pt-BR.js'
import esLatam, { esLatamOverrides } from './es-latam.js'
import enGB, { enGBOverrides } from './en-GB.js'
import { enAUOverrides } from './en-AU.js'
import { enCAOverrides } from './en-CA.js'
import { enINOverrides } from './en-IN.js'
import { enNZOverrides } from './en-NZ.js'
import { enZAOverrides } from './en-ZA.js'
import { deATOverrides } from './de-AT.js'
import { deCHOverrides } from './de-CH.js'
import { frBEOverrides } from './fr-BE.js'
import { frCAOverrides } from './fr-CA.js'
import { frCHOverrides } from './fr-CH.js'
import { esAROverrides } from './es-AR.js'
import { esMXOverrides } from './es-MX.js'
import { ptPTOverrides } from './pt-PT.js'
import { nlBEOverrides } from './nl-BE.js'
import { arAEOverrides } from './ar-AE.js'
import { arEGOverrides } from './ar-EG.js'
import { bnINOverrides } from './bn-IN.js'

/**
 * Every country catalog and the pieces it was built from, so the rules below
 * apply to all of them without being restated once per locale.
 */
const variants = [
  { code: 'en-GB', base: enUS, overrides: enGBOverrides },
  { code: 'en-AU', base: enGB, overrides: enAUOverrides },
  { code: 'en-IN', base: enGB, overrides: enINOverrides },
  { code: 'en-NZ', base: enGB, overrides: enNZOverrides },
  { code: 'en-ZA', base: enGB, overrides: enZAOverrides },
  { code: 'en-CA', base: enUS, overrides: enCAOverrides },
  { code: 'de-AT', base: deDE, overrides: deATOverrides },
  { code: 'de-CH', base: deDE, overrides: deCHOverrides },
  { code: 'fr-BE', base: frFR, overrides: frBEOverrides },
  { code: 'fr-CA', base: frFR, overrides: frCAOverrides },
  { code: 'fr-CH', base: frFR, overrides: frCHOverrides },
  { code: 'es-MX', base: esLatam, overrides: esMXOverrides },
  { code: 'es-AR', base: esLatam, overrides: esAROverrides },
  { code: 'pt-PT', base: ptBR, overrides: ptPTOverrides },
  { code: 'nl-BE', base: nlNL, overrides: nlBEOverrides },
  { code: 'ar-AE', base: arSA, overrides: arAEOverrides },
  { code: 'ar-EG', base: arSA, overrides: arEGOverrides },
  { code: 'bn-IN', base: bnBD, overrides: bnINOverrides },
]

describe('derive', () => {
  it('applies overrides over the base', () => {
    const got = derive({ a: 'base-a', b: 'base-b' }, { b: 'over-b' })
    expect(got).toEqual({ a: 'base-a', b: 'over-b' })
  })

  it('does not mutate its arguments', () => {
    // The property the whole scheme rests on: every variant of a language
    // derives from the same base object, so a derive() that wrote through
    // would let the last country registered rewrite the language for everyone.
    const base = { a: 'base-a' }
    const overrides = { a: 'over-a' }
    derive(base, overrides)
    expect(base).toEqual({ a: 'base-a' })
    expect(overrides).toEqual({ a: 'over-a' })
  })

  it('copies the base when there are no overrides', () => {
    const base = { a: 'base-a', b: 'base-b' }
    expect(derive(base, {})).toEqual(base)
  })
})

describe('country catalogs', () => {
  it.each(variants)('$code overrides only keys its base defines', ({ base, overrides }) => {
    // The typo that would otherwise be silent: an override written against a
    // key the base does not have adds a message nothing asks for, and leaves
    // the string it meant to change untouched.
    //
    // Object.keys().toContain rather than toHaveProperty: every key here holds
    // dots, and toHaveProperty reads a dot as a path separator, so it would go
    // looking for a nested `dns.disabled_message` object that does not exist.
    const baseKeys = Object.keys(base)
    for (const key of Object.keys(overrides)) {
      expect(baseKeys, `unknown key ${key}`).toContain(key)
    }
  })

  it.each(variants)('$code overrides actually differ from the base', ({ base, overrides }) => {
    // A line that repeats the base verbatim is not a departure, it is
    // duplication that stops tracking the base the next time the base changes.
    for (const [key, value] of Object.entries(overrides)) {
      expect(value, `${key} repeats the base`).not.toBe(base[key])
    }
  })

  it.each(variants)('$code carries every key of its base', ({ code, base }) => {
    const catalog = catalogs[code]
    expect(catalog, `${code} is not registered in catalogs`).toBeDefined()
    expect(Object.keys(catalog).sort()).toEqual(Object.keys(base).sort())
  })

  it('registers every country catalog', () => {
    for (const { code } of variants) {
      expect(Object.keys(catalogs)).toContain(code)
    }
  })
})

describe('country catalog spot checks', () => {
  // Structural tests pass whatever the strings say. These pin the specific
  // departures each override file was written for.
  it.each([
    ['en-GB', 'dns.disabled_message', /initialise/],
    ['en-AU', 'dns.disabled_message', /initialise/],
    ['en-CA', 'dns.disabled_message', /initialize/],
    ['de-CH', 'packages.manifest_close', /^Schliessen$/],
    ['de-AT', 'packages.manifest_close', /^Schließen$/],
    ['fr-CA', 'packages.col_repository', /^Dépôt$/],
    ['fr-CA', 'archive.upload_btn', /^Téléverser$/],
    ['fr-FR', 'packages.col_repository', /^Repository$/],
    ['es-MX', 'nav.monitoring', /^Monitoreo$/],
    ['es-AR', 'dns.add_record_btn', /Agregar/],
    ['es-ES', 'nav.monitoring', /^Monitorización$/],
    ['pt-PT', 'common.loading', /^A carregar/],
    ['pt-PT', 'nav.users', /^Utilizadores$/],
    ['pt-BR', 'nav.users', /^Usuários$/],
    ['nl-BE', 'settings.dns_local_forwarders_description', /uw eigen netwerk/],
    ['ar-EG', 'archive.download_btn', /تحميل/],
    ['ar-AE', 'archive.download_btn', /تنزيل/],
  ])('%s %s', (code, key, pattern) => {
    expect(catalogs[code][key]).toMatch(pattern)
  })
})

describe('es-latam', () => {
  it('is not a selectable locale', () => {
    // It is a shared fragment that es-MX and es-AR build on, not a place
    // anyone speaks. Advertising it would offer a country code that is not one.
    expect(Object.keys(catalogs)).not.toContain('es-latam')
    expect(Object.keys(catalogs)).not.toContain('es-419')
  })

  it('departs from peninsular Spanish', () => {
    const esESKeys = Object.keys(esES)
    for (const key of Object.keys(esLatamOverrides)) {
      expect(esESKeys, `unknown key ${key}`).toContain(key)
      expect(esLatam[key]).not.toBe(esES[key])
    }
  })
})

import deDE from './de-DE.js'
import derive from './derive.js'

/**
 * Swiss Standard German abolished ß and writes ss in its place, everywhere,
 * without exception. That single rule is the whole of this file: these are the
 * ten strings in de-DE.js that contain a ß, rewritten.
 *
 * The rule is mechanical, but it cannot be applied at runtime with a replace —
 * ß is not always ss in other contexts, and a catalog that transformed itself
 * on the way to the screen would be impossible to grep for. Listing the ten
 * strings keeps them findable and keeps the next German string that arrives
 * from silently reaching Switzerland in the wrong orthography, because it
 * arrives in the base and only shows up here if someone adds a ß to it.
 *
 * (The backend catalog needed no such file: src/i18n/de_de.go happens to
 * contain no ß at all.)
 */
export const deCHOverrides = {
  'system.refresh_warning_1':
    'Dadurch werden die neuesten Container-Images abgerufen und ALLE Kerndienste einschliesslich des System-Controllers neu gestartet.',
  'packages.manifest_close': 'Schliessen',
  'settings.archive_size_title': 'Maximale Archivgrösse',
  'settings.archive_size_description': 'Die maximal zulässige Dateigrösse für Archiv-Uploads.',
  'settings.archive_size_label': 'Grösse',
  'settings.toast_archive_size_updated': 'Maximale Archivgrösse aktualisiert',
  'settings.error_invalid_archive_size': 'Ungültiger Archivgrössenwert',
  'settings.dns_resolution_description':
    'Wie Namen ausserhalb Ihrer lokalen Zonen aufgelöst werden. Automatisch fragt die Root-Server direkt ab und weicht nur auf verschlüsseltes DNS oder einen Upstream-Resolver aus, wenn das Netzwerk dies blockiert, sodass es privat bleibt, wo immer es möglich ist. Rekursiv verwendet ausschliesslich die Root-Server, was in Netzwerken, die direktes DNS blockieren, komplett fehlschlägt. Weiterleiten verwendet immer die Upstream-Resolver.',
  'networks.close': 'Schliessen',
  'package_info.close_btn': 'Schliessen',
}

/** German (Switzerland) — de-DE with ß written as ss. */
const deCH = derive(deDE, deCHOverrides)

export default deCH

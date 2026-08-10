import derive from './derive.js'
import esES from './es-ES.js'

/**
 * Departures from peninsular Spanish that every American variety shares.
 *
 * This is not a locale — nothing registers `es-latam` in the catalogs map — but
 * es-MX and es-AR both start from it, because the choices below are American
 * Spanish generally rather than Mexican or Argentine in particular. Stating
 * them once keeps the two country files honest about what is actually theirs.
 *
 * - **inválido**, not *no válido*. Spain's style guides prefer the analytic
 *   form; American Spanish uses the adjective directly. In a console full of
 *   validation errors this is the most visible tell there is.
 * - **agregar**, not *añadir*. Both are Spanish, but *añadir* is markedly
 *   peninsular in software and *agregar* is the American default. It appears
 *   on nearly every screen with an Add button.
 * - **monitoreo**, not *monitorización*. Including the nav item, so it is the
 *   first word of this catalog a user sees.
 *
 * What is deliberately *not* changed is the register. es-ES.js addresses the
 * reader as *usted* (`Seleccione`, `Consulte`), and formal address is equally
 * standard in American software; switching it would be a style preference
 * wearing a locale's clothes.
 */
export const esLatamOverrides = {
  // inválido, not "no válido".
  'login.error_invalid_credentials': 'Nombre de usuario o contraseña inválidos',
  'settings.error_invalid_peer_ttl': 'Valor de TTL de par inválido',
  'settings.error_invalid_quota': 'Valor de cuota inválido',
  'settings.error_invalid_archive_size': 'Valor de tamaño de archivo inválido',
  'settings.error_invalid_timeout': 'Valor de tiempo de espera inválido',

  // agregar, not añadir.
  'packages.add_repo_btn': 'Agregar repositorio',
  'packages.add_repo_dialog_title': 'Agregar repositorio',
  'packages.add_repo_dialog_description': 'Agregue un nuevo repositorio de paquetes por nombre y URL.',
  'packages.add_btn': 'Agregar',
  'packages.toast_repo_added': 'Repositorio agregado',
  'pages.create_dialog_description':
    'Agregue un nuevo sitio estático desde un archivo, una imagen de contenedor o un repositorio git.',
  'networks.peers_description':
    'Dispositivos permitidos en esta red. Agregar un par genera una configuración que puede importar en el dispositivo.',
  'networks.add_peer': 'Agregar par',
  'networks.toast_peer_added': 'Par agregado',
  'archive.filename_hint':
    'Nombre base para el archivo descargado. La extensión del archivo se agrega automáticamente.',
  'dns.add_record_btn': 'Agregar registro',
  'dns.add_dialog_title': 'Agregar registro DNS',
  'dns.add_submit': 'Agregar registro',
  'dns.toast_record_added': 'Registro DNS agregado',
  'dns.bl.add_zone': 'Agregar zona',
  'dns.bl.add_entry': 'Agregar entrada',
  'dns.bl.add_entry_title': 'Agregar entrada de lista de bloqueo',
  'dns.bl.entry_added': 'Entrada de lista de bloqueo agregada',
  'dns.al.add_entry': 'Agregar entrada',
  'dns.al.add_entry_title': 'Agregar entrada de lista de permitidos',
  'dns.al.entry_added': 'Entrada de lista de permitidos agregada',
  'objects.add_user': 'Agregar usuario',
  'objects.add_user_title': 'Agregar un usuario a esta partición',
  'objects.add_user_description':
    'Proyecta una cuenta de Town OS en esta partición. Empieza sin permisos; agregue uno después.',
  'objects.no_users': 'No se han agregado usuarios a esta partición.',
  'objects.add_grant': 'Agregar permiso',
  'objects.toast_user_added': 'Usuario agregado',
  'objects.toast_grant_added': 'Permiso agregado',

  // monitoreo, not monitorización.
  'nav.monitoring': 'Monitoreo',
  'settings.monitoring_title': 'Panel de monitoreo',
  'settings.monitoring_description':
    'Seleccione qué frontend de monitoreo usar. uPlot es un renderizador de gráficos integrado y ligero. Grafana ofrece paneles completos con un uso de recursos adicional.',
  'settings.toast_monitoring_updated':
    'Backend de monitoreo actualizado. El cambio surtirá efecto en el próximo reinicio del servicio.',
  'settings.toast_monitoring_restarting': 'Reiniciando la interfaz de monitoreo...',
  'settings.toast_monitoring_ready': 'Interfaz de monitoreo reiniciada ({backend})',
  'settings.toast_monitoring_timeout':
    'El backend de monitoreo se guardó, pero la interfaz de monitoreo no volvió a estar en línea a tiempo. Consulte los registros del servicio.',
}

/**
 * es-ES with the shared American departures applied. The base for es-MX and
 * es-AR; not itself a selectable locale.
 */
const esLatam = derive(esES, esLatamOverrides)

export default esLatam

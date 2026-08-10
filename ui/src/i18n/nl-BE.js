import derive from './derive.js'
import nlNL from './nl-NL.js'

/**
 * The difference that matters between Belgian and Netherlandic Dutch in
 * software is register. The Netherlands has largely moved to the informal *je*
 * when a program addresses its user; Flanders keeps the formal *u*, and *je* in
 * an admin console reads as too familiar there.
 *
 * That difference can only surface in text that addresses the reader, and this
 * catalog almost never does — it labels controls and states conditions. One
 * string is the exception, and it is the one string here.
 */
export const nlBEOverrides = {
  'settings.dns_local_forwarders_description':
    'Gebruik de DNS-servers die uw eigen netwerk aanbiedt in plaats van de publieke. Een netwerk dat extern DNS blokkeert — een hotel, een captive portal, sommige providers — beantwoordt nog steeds vragen aan de resolver die het zelf uitdeelt, en daardoor blijven namen daar oplosbaar. Laat het uit staan waar direct DNS werkt: de resolver van uw netwerk ziet elke naam die uw huishouden opzoekt.',
}

/** Dutch (Belgium) — nl-NL in the formal register. */
const nlBE = derive(nlNL, nlBEOverrides)

export default nlBE

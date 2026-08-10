import derive from './derive.js'
import esLatam from './es-latam.js'

/**
 * Empty on top of es-latam.js, which is where the work for this locale actually
 * happened. Mexican Spanish is the closest thing American Spanish has to a
 * neutral standard — it is what "Latin American Spanish" localisations are
 * usually written in — so once the shared American departures are applied,
 * there is nothing specifically Mexican left to say about volumes, peers and
 * audit entries.
 */
export const esMXOverrides = {}

/** Spanish (Mexico) — es-ES by way of the shared American departures. */
const esMX = derive(esLatam, esMXOverrides)

export default esMX

import derive from './derive.js'
import esLatam from './es-latam.js'

/**
 * Empty on top of es-latam.js.
 *
 * What makes Argentine Spanish immediately recognisable is voseo —
 * *seleccioná* where Spain writes *selecciona*. But voseo is an alternative to
 * *tuteo*, and this catalog never uses *tú*: es-ES.js addresses the reader as
 * *usted* (`Seleccione`, `Consulte`), and *usted* is identical in Buenos Aires
 * and Madrid. There is no second-person familiar form here for voseo to
 * replace.
 *
 * Writing voseo anyway would mean first switching the whole catalog from formal
 * to familiar address — a register change dressed up as a locale, and one that
 * would make es-AR the only Spanish that speaks to the reader differently.
 */
export const esAROverrides = {}

/** Spanish (Argentina) — es-ES by way of the shared American departures. */
const esAR = derive(esLatam, esAROverrides)

export default esAR

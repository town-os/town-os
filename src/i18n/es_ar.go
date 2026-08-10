package i18n

// esAROverrides is empty on top of esLatamOverrides.
//
// What makes Argentine Spanish immediately recognisable is voseo — seleccioná
// where Spain writes selecciona — and voseo is an alternative to tuteo, to the
// familiar tú. This catalog never uses it: it is written the way error messages
// are, infinitives for the audit log ("crear cuenta") and impersonal statements
// for the rest ("la partición ya existe"). It never addresses the reader at
// all, so there is no familiar form here for voseo to replace. The frontend
// does address the reader, but in the formal usted register, which is identical
// in Buenos Aires and Madrid — so es-AR.js is empty for the same reason.
var esAROverrides = map[string]string{}

// esARMessages is es-ES by way of the shared American departures.
var esARMessages = derive(esLatamMessages, esAROverrides)

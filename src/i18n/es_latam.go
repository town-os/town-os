package i18n

// esLatamOverrides holds the departures from peninsular Spanish that every
// American variety shares. It is not a locale of its own — nothing registers
// it in catalogs — but es-MX and es-AR both start from it, because the choices
// below are American Spanish generally rather than Mexican or Argentine in
// particular, and stating them once keeps the two country files honest about
// what is actually theirs.
//
// Three things are going on:
//
//   - inválido, not "no válido". Spain's style guides prefer the analytic
//     "no válido"; American Spanish uses the adjective directly, and this is
//     the single most visible tell in a catalog full of validation errors.
//   - agregar, not añadir. Both are Spanish, but añadir is markedly
//     peninsular in software and agregar is the American default.
//   - Straight quotes, not angle ones. « » is Spain's typographic convention;
//     the Americas use " ". This one is invisible until it isn't.
//
// Plus a handful of lexical swaps — verificar for comprobar, monitoreo for
// monitorización, administrador for gestor — that fall the same way.
var esLatamOverrides = map[string]string{
	// inválido rather than "no válido".
	MsgAuthInvalidSession:     "sesión inválida",
	MsgAuthInvalidCredentials: "credenciales inválidas",
	MsgSettingInvalidBytes:    "valor de bytes inválido para %q: %v",
	MsgUnitInvalidLines:       "parámetro de líneas inválido",
	MsgUnitInvalidPriority:    "parámetro de prioridad inválido",
	MsgRepoInvalidURL:         "url inválida",

	// inválido, and straight quotes instead of angle ones.
	MsgUnitInvalidSince: `parámetro "since" inválido`,
	MsgUnitInvalidUntil: `parámetro "until" inválido`,

	// agregar rather than añadir.
	MsgAuditAddRepository:    "agregar repositorio",
	MsgAuditAddDNSRecord:     "agregar registro dns",
	MsgAuditAddNetworkPeer:   "agregar par de red",
	MsgAuditAddGfehPrincipal: "agregar usuario de almacenamiento de objetos",
	MsgAuditAddGfehGrant:     "agregar permiso de almacenamiento de objetos",

	// Lexical swaps that fall the same way across the Americas.
	MsgAccountCheckSessions:    "verificar sesiones de administrador activas",
	MsgMonitoringNotConfigured: "el monitoreo no está configurado",
	MsgSettingsMgrMissing:      "el administrador de configuración no está disponible",
	MsgUpgradeSettingsMissing:  "el administrador de configuración no está disponible",
}

// esLatamMessages is es-ES with the shared American departures applied. It is
// the base for es-MX and es-AR and is not itself a selectable locale.
var esLatamMessages = derive(esESMessages, esLatamOverrides)

package i18n

// esMXOverrides is empty on top of esLatamOverrides, which is where the work
// for this locale actually happened. Mexican Spanish is the closest thing
// American Spanish has to a neutral standard — it is what "Latin American
// Spanish" localizations are usually written in — so once the shared American
// departures are applied there is nothing specifically Mexican left to say
// about tokens, subvolumes and audit entries.
var esMXOverrides = map[string]string{}

// esMXMessages is es-ES by way of the shared American departures.
var esMXMessages = derive(esLatamMessages, esMXOverrides)

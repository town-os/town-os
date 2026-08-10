package i18n

// deCHOverrides is empty in the backend catalog, which is itself the result.
//
// The one systematic difference between Swiss Standard German and the German
// of Germany is orthographic: Switzerland abolished ß and writes ss for it
// everywhere. That rule is mechanical, and it applies wherever a ß appears —
// but no message in de_de.go contains one, so there is nothing here for it to
// change. The frontend catalog is a different story: de-CH.js carries real
// overrides, because de-DE.js does use ß.
var deCHOverrides = map[string]string{}

// deCHMessages is de-DE unchanged; see deCHOverrides for why.
var deCHMessages = derive(deDEMessages, deCHOverrides)

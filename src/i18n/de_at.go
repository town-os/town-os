package i18n

// deATOverrides is empty. Austrian German differs from the German of Germany
// in month names (Jänner), in food and administrative vocabulary, and in the
// gender of a handful of nouns — none of which this catalog contains. Its
// computing vocabulary is the shared one: Datei, Sitzung, Berechtigung,
// Speicher. There is no Austrian word for a btrfs subvolume.
var deATOverrides = map[string]string{}

// deATMessages is de-DE unchanged, reviewed for Austrian usage.
var deATMessages = derive(deDEMessages, deATOverrides)

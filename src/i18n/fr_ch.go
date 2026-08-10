package i18n

// frCHOverrides is empty, for the same reason as frBEOverrides: Swiss French
// departs from the French of France in its numerals (septante, huitante,
// nonante) and in cantonal administrative vocabulary, neither of which this
// catalog contains.
var frCHOverrides = map[string]string{}

// frCHMessages is fr-FR unchanged, reviewed for Swiss usage.
var frCHMessages = derive(frFRMessages, frCHOverrides)

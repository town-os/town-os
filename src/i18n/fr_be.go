package i18n

// frBEOverrides is empty. Belgian French is best known for septante and
// nonante, and for a small set of institutional terms; this catalog spells no
// number as a word and names no institution. Unlike Canadian French, Belgian
// French does not systematically avoid English computing loans, so the
// vocabulary of fr-FR stands as written.
var frBEOverrides = map[string]string{}

// frBEMessages is fr-FR unchanged, reviewed for Belgian usage.
var frBEMessages = derive(frFRMessages, frBEOverrides)

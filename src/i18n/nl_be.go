package i18n

// nlBEOverrides is empty in the backend catalog. The difference that matters
// between Belgian and Netherlandic Dutch in software is register: Flanders
// keeps the formal u where the Netherlands has largely moved to je. That
// difference only exists in text that addresses the reader, and the backend
// catalog does not — it states conditions ("sessie ongeldig") and names audit
// actions in the infinitive. The frontend does address the reader, and
// nl-BE.js carries the overrides for it.
var nlBEOverrides = map[string]string{}

// nlBEMessages is nl-NL unchanged; see nlBEOverrides for why.
var nlBEMessages = derive(nlNLMessages, nlBEOverrides)

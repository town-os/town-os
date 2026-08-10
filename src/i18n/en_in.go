package i18n

// enINOverrides is empty. Indian English follows British spelling, which
// inheriting en-GB supplies. Where Indian English is genuinely distinctive —
// lakh and crore for large numbers, its own register of official phrasing —
// this catalog has no numbers spelled as words and no official register to
// depart from.
var enINOverrides = map[string]string{}

// enINMessages is en-GB unchanged, reviewed for Indian usage.
var enINMessages = derive(enGBMessages, enINOverrides)

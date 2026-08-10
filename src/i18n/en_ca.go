package i18n

// enCAOverrides is empty, and en-CA inherits en-US rather than en-GB.
//
// Canadian English is a split decision: it keeps the British -our and -re
// endings but the American -ize ones. This catalog contains no -our or -re
// word at all, so the only rule that reaches it is the -ize rule — and on
// that rule Canada agrees with the United States. "authorization" is correct
// Canadian spelling, so en-CA is en-US here, exactly.
var enCAOverrides = map[string]string{}

// enCAMessages is en-US unchanged, reviewed for Canadian usage.
var enCAMessages = derive(enUSMessages, enCAOverrides)

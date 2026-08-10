package i18n

// enAUOverrides is empty, and that is the finding rather than a gap.
//
// Australian English follows British spelling, which is why en-AU inherits
// en-GB rather than en-US — that alone gets "authorisation" right. Beyond
// spelling, the words Australian English is actually distinctive about are
// everyday nouns, and a control panel that talks about subvolumes, quotas and
// systemd units never reaches for one. There is nothing here to change that
// would not be an invention.
var enAUOverrides = map[string]string{}

// enAUMessages is en-GB unchanged, reviewed for Australian usage.
var enAUMessages = derive(enGBMessages, enAUOverrides)

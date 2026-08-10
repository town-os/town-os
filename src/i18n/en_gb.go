package i18n

// enGBOverrides holds the strings where British English departs from the
// American English of en_us.go. Everything else is inherited unchanged.
//
// The backend catalog is short technical prose, and almost none of it touches
// the vocabulary the two Englishes disagree about — there is no colour, no
// licence, no catalogue and no centre anywhere in it. What is left is the
// -ise/-ize split, which reaches exactly one message.
var enGBOverrides = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	MsgAuthMissingToken: "missing authorisation token",
}

// enGBMessages is en-US with the British departures applied. It is also the
// base for the Englishes that follow British spelling: en-AU, en-IN, en-NZ
// and en-ZA.
var enGBMessages = derive(enUSMessages, enGBOverrides)

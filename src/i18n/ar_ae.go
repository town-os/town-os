package i18n

// arAEOverrides is empty. Emirati written Arabic is Gulf-standard Modern
// Standard Arabic, which is what ar_sa.go already is — including تنزيل for
// download, the one point where regional written usage genuinely splits (see
// arEGOverrides for the Egyptian side of it). The UAE sits on the same side of
// that split as Saudi Arabia, so ar-AE has nothing to change.
var arAEOverrides = map[string]string{}

// arAEMessages is ar-SA unchanged, reviewed for Emirati usage.
var arAEMessages = derive(arSAMessages, arAEOverrides)

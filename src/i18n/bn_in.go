package i18n

// bnINOverrides is empty. Bengali is one language across the border: West
// Bengal and Bangladesh share an orthography, and the divergence between them
// is a matter of register — Bangladesh's formal writing carries more
// Perso-Arabic vocabulary, India's more Sanskritic — which surfaces in
// literary and administrative prose rather than in software. The computing
// terms this catalog needs are the same borrowed English ones on both sides
// (ফাইলসিস্টেম, সেটিংস, আর্কাইভ), so bn-IN is bn-BD here.
var bnINOverrides = map[string]string{}

// bnINMessages is bn-BD unchanged, reviewed for Indian usage.
var bnINMessages = derive(bnBDMessages, bnINOverrides)

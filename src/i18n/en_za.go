package i18n

// enZAOverrides is empty. South African English follows British spelling,
// supplied by inheriting en-GB. Its distinctive vocabulary is borrowed from
// Afrikaans and the other official languages and belongs to daily life, not
// to a message set about btrfs subvolumes and systemd units.
var enZAOverrides = map[string]string{}

// enZAMessages is en-GB unchanged, reviewed for South African usage.
var enZAMessages = derive(enGBMessages, enZAOverrides)

package i18n

// enNZOverrides is empty for the same reason as enAUOverrides: New Zealand
// English follows British spelling, which inheriting en-GB already supplies,
// and its distinctive vocabulary lies outside anything a storage and service
// control panel says.
var enNZOverrides = map[string]string{}

// enNZMessages is en-GB unchanged, reviewed for New Zealand usage.
var enNZMessages = derive(enGBMessages, enNZOverrides)

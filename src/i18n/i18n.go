// Package i18n provides internationalization support for Town OS.
//
// It uses a message catalog approach where all user-facing strings are
// keyed by a dot-separated identifier and resolved at runtime based on
// the active locale. The default locale is en-US.
//
// Usage:
//
//	msg := i18n.T("en-US", i18n.MsgAuthMissingToken)
//	msg := i18n.T("en-US", i18n.MsgSettingNotFound, "default_quota")
package i18n

import (
	"fmt"
	"slices"
)

// Locale represents a supported locale with its BCP 47 code,
// native language name (displayed in the language's own script),
// and English name.
type Locale struct {
	// Code is the BCP 47 language tag (e.g. "en-US", "de-DE").
	Code string `json:"code"`

	// NativeName is the language name written in its own script
	// (e.g. "English", "Deutsch", "中文").
	NativeName string `json:"native_name"`

	// EnglishName is the language name in English (e.g. "German", "Chinese").
	EnglishName string `json:"english_name"`
}

// DefaultLocale is the fallback locale when none is configured.
const DefaultLocale = "en-US"

// CommonLanguages is a curated list of widely spoken languages presented
// in their own scripts. This list is shown to users for initial language
// selection. Each entry maps to a set of country-specific locale codes.
//

var CommonLanguages = []Locale{
	{Code: "ar-SA", NativeName: "العربية", EnglishName: "Arabic"},
	{Code: "bn-BD", NativeName: "বাংলা", EnglishName: "Bengali"},
	{Code: "de-DE", NativeName: "Deutsch", EnglishName: "German"},
	{Code: "en-US", NativeName: "English", EnglishName: "English"},
	{Code: "es-ES", NativeName: "Español", EnglishName: "Spanish"},
	{Code: "fr-FR", NativeName: "Français", EnglishName: "French"},
	{Code: "hi-IN", NativeName: "हिन्दी", EnglishName: "Hindi"},
	{Code: "it-IT", NativeName: "Italiano", EnglishName: "Italian"},
	{Code: "ja-JP", NativeName: "日本語", EnglishName: "Japanese"},
	{Code: "ko-KR", NativeName: "한국어", EnglishName: "Korean"},
	{Code: "nl-NL", NativeName: "Nederlands", EnglishName: "Dutch"},
	{Code: "pl-PL", NativeName: "Polski", EnglishName: "Polish"},
	{Code: "pt-BR", NativeName: "Português", EnglishName: "Portuguese"},
	{Code: "ru-RU", NativeName: "Русский", EnglishName: "Russian"},
	{Code: "sa-IN", NativeName: "संस्कृतम्", EnglishName: "Sanskrit"},
	{Code: "sv-SE", NativeName: "Svenska", EnglishName: "Swedish"},
	{Code: "th-TH", NativeName: "ไทย", EnglishName: "Thai"},
	{Code: "tr-TR", NativeName: "Türkçe", EnglishName: "Turkish"},
	{Code: "uk-UA", NativeName: "Українська", EnglishName: "Ukrainian"},
	{Code: "vi-VN", NativeName: "Tiếng Việt", EnglishName: "Vietnamese"},
	{Code: "zh-CN", NativeName: "中文", EnglishName: "Chinese"},
}

// ExtendedLocales is a comprehensive list of country-specific locale codes
// for users whose language needs are not met by the common languages list.
//

var ExtendedLocales = []Locale{
	{Code: "af-ZA", NativeName: "Afrikaans", EnglishName: "Afrikaans"},
	{Code: "am-ET", NativeName: "አማርኛ", EnglishName: "Amharic"},
	{Code: "ar-AE", NativeName: "العربية (الإمارات)", EnglishName: "Arabic (UAE)"},
	{Code: "ar-EG", NativeName: "العربية (مصر)", EnglishName: "Arabic (Egypt)"},
	{Code: "ar-SA", NativeName: "العربية (السعودية)", EnglishName: "Arabic (Saudi Arabia)"},
	{Code: "bg-BG", NativeName: "Български", EnglishName: "Bulgarian"},
	{Code: "bn-BD", NativeName: "বাংলা (বাংলাদেশ)", EnglishName: "Bengali (Bangladesh)"},
	{Code: "bn-IN", NativeName: "বাংলা (ভারত)", EnglishName: "Bengali (India)"},
	{Code: "ca-ES", NativeName: "Català", EnglishName: "Catalan"},
	{Code: "cs-CZ", NativeName: "Čeština", EnglishName: "Czech"},
	{Code: "cy-GB", NativeName: "Cymraeg", EnglishName: "Welsh"},
	{Code: "da-DK", NativeName: "Dansk", EnglishName: "Danish"},
	{Code: "de-AT", NativeName: "Deutsch (Österreich)", EnglishName: "German (Austria)"},
	{Code: "de-CH", NativeName: "Deutsch (Schweiz)", EnglishName: "German (Switzerland)"},
	{Code: "de-DE", NativeName: "Deutsch (Deutschland)", EnglishName: "German (Germany)"},
	{Code: "el-GR", NativeName: "Ελληνικά", EnglishName: "Greek"},
	{Code: "en-AU", NativeName: "English (Australia)", EnglishName: "English (Australia)"},
	{Code: "en-CA", NativeName: "English (Canada)", EnglishName: "English (Canada)"},
	{Code: "en-GB", NativeName: "English (UK)", EnglishName: "English (United Kingdom)"},
	{Code: "en-IN", NativeName: "English (India)", EnglishName: "English (India)"},
	{Code: "en-NZ", NativeName: "English (New Zealand)", EnglishName: "English (New Zealand)"},
	{Code: "en-US", NativeName: "English (US)", EnglishName: "English (United States)"},
	{Code: "en-ZA", NativeName: "English (South Africa)", EnglishName: "English (South Africa)"},
	{Code: "es-AR", NativeName: "Español (Argentina)", EnglishName: "Spanish (Argentina)"},
	{Code: "es-ES", NativeName: "Español (España)", EnglishName: "Spanish (Spain)"},
	{Code: "es-MX", NativeName: "Español (México)", EnglishName: "Spanish (Mexico)"},
	{Code: "et-EE", NativeName: "Eesti", EnglishName: "Estonian"},
	{Code: "eu-ES", NativeName: "Euskara", EnglishName: "Basque"},
	{Code: "fa-IR", NativeName: "فارسی", EnglishName: "Persian"},
	{Code: "fi-FI", NativeName: "Suomi", EnglishName: "Finnish"},
	{Code: "fr-BE", NativeName: "Français (Belgique)", EnglishName: "French (Belgium)"},
	{Code: "fr-CA", NativeName: "Français (Canada)", EnglishName: "French (Canada)"},
	{Code: "fr-CH", NativeName: "Français (Suisse)", EnglishName: "French (Switzerland)"},
	{Code: "fr-FR", NativeName: "Français (France)", EnglishName: "French (France)"},
	{Code: "ga-IE", NativeName: "Gaeilge", EnglishName: "Irish"},
	{Code: "gl-ES", NativeName: "Galego", EnglishName: "Galician"},
	{Code: "gu-IN", NativeName: "ગુજરાતી", EnglishName: "Gujarati"},
	{Code: "he-IL", NativeName: "עברית", EnglishName: "Hebrew"},
	{Code: "hi-IN", NativeName: "हिन्दी", EnglishName: "Hindi"},
	{Code: "hr-HR", NativeName: "Hrvatski", EnglishName: "Croatian"},
	{Code: "hu-HU", NativeName: "Magyar", EnglishName: "Hungarian"},
	{Code: "hy-AM", NativeName: "Հայերեն", EnglishName: "Armenian"},
	{Code: "id-ID", NativeName: "Bahasa Indonesia", EnglishName: "Indonesian"},
	{Code: "is-IS", NativeName: "Íslenska", EnglishName: "Icelandic"},
	{Code: "it-IT", NativeName: "Italiano", EnglishName: "Italian"},
	{Code: "ja-JP", NativeName: "日本語", EnglishName: "Japanese"},
	{Code: "ka-GE", NativeName: "ქართული", EnglishName: "Georgian"},
	{Code: "kk-KZ", NativeName: "Қазақша", EnglishName: "Kazakh"},
	{Code: "km-KH", NativeName: "ខ្មែរ", EnglishName: "Khmer"},
	{Code: "kn-IN", NativeName: "ಕನ್ನಡ", EnglishName: "Kannada"},
	{Code: "ko-KR", NativeName: "한국어", EnglishName: "Korean"},
	{Code: "lo-LA", NativeName: "ລາວ", EnglishName: "Lao"},
	{Code: "lt-LT", NativeName: "Lietuvių", EnglishName: "Lithuanian"},
	{Code: "lv-LV", NativeName: "Latviešu", EnglishName: "Latvian"},
	{Code: "mk-MK", NativeName: "Македонски", EnglishName: "Macedonian"},
	{Code: "ml-IN", NativeName: "മലയാളം", EnglishName: "Malayalam"},
	{Code: "mn-MN", NativeName: "Монгол", EnglishName: "Mongolian"},
	{Code: "mr-IN", NativeName: "मराठी", EnglishName: "Marathi"},
	{Code: "ms-MY", NativeName: "Bahasa Melayu", EnglishName: "Malay"},
	{Code: "my-MM", NativeName: "မြန်မာ", EnglishName: "Burmese"},
	{Code: "nb-NO", NativeName: "Norsk Bokmål", EnglishName: "Norwegian Bokmål"},
	{Code: "ne-NP", NativeName: "नेपाली", EnglishName: "Nepali"},
	{Code: "nl-BE", NativeName: "Nederlands (België)", EnglishName: "Dutch (Belgium)"},
	{Code: "nl-NL", NativeName: "Nederlands (Nederland)", EnglishName: "Dutch (Netherlands)"},
	{Code: "pa-IN", NativeName: "ਪੰਜਾਬੀ", EnglishName: "Punjabi"},
	{Code: "pl-PL", NativeName: "Polski", EnglishName: "Polish"},
	{Code: "pt-BR", NativeName: "Português (Brasil)", EnglishName: "Portuguese (Brazil)"},
	{Code: "pt-PT", NativeName: "Português (Portugal)", EnglishName: "Portuguese (Portugal)"},
	{Code: "ro-RO", NativeName: "Română", EnglishName: "Romanian"},
	{Code: "ru-RU", NativeName: "Русский", EnglishName: "Russian"},
	{Code: "sa-IN", NativeName: "संस्कृतम्", EnglishName: "Sanskrit"},
	{Code: "si-LK", NativeName: "සිංහල", EnglishName: "Sinhala"},
	{Code: "sk-SK", NativeName: "Slovenčina", EnglishName: "Slovak"},
	{Code: "sl-SI", NativeName: "Slovenščina", EnglishName: "Slovenian"},
	{Code: "sq-AL", NativeName: "Shqip", EnglishName: "Albanian"},
	{Code: "sr-RS", NativeName: "Српски", EnglishName: "Serbian"},
	{Code: "sv-SE", NativeName: "Svenska", EnglishName: "Swedish"},
	{Code: "sw-KE", NativeName: "Kiswahili", EnglishName: "Swahili"},
	{Code: "ta-IN", NativeName: "தமிழ்", EnglishName: "Tamil"},
	{Code: "te-IN", NativeName: "తెలుగు", EnglishName: "Telugu"},
	{Code: "th-TH", NativeName: "ไทย", EnglishName: "Thai"},
	{Code: "tr-TR", NativeName: "Türkçe", EnglishName: "Turkish"},
	{Code: "uk-UA", NativeName: "Українська", EnglishName: "Ukrainian"},
	{Code: "ur-PK", NativeName: "اردو", EnglishName: "Urdu"},
	{Code: "uz-UZ", NativeName: "Oʻzbekcha", EnglishName: "Uzbek"},
	{Code: "vi-VN", NativeName: "Tiếng Việt", EnglishName: "Vietnamese"},
	{Code: "zh-CN", NativeName: "中文 (简体)", EnglishName: "Chinese (Simplified)"},
	{Code: "zh-TW", NativeName: "中文 (繁體)", EnglishName: "Chinese (Traditional)"},
	{Code: "zu-ZA", NativeName: "IsiZulu", EnglishName: "Zulu"},
}

// populatedLocales lists every locale code that has a catalog, in the order
// the API reports them: the default first, then alphabetically. It is derived
// from catalogs at init so the two can never disagree — a catalog registered
// without being advertised here was the one drift this list could develop.
var populatedLocales = buildPopulatedLocales()

// buildPopulatedLocales reads the catalog map into a sorted list with the
// default locale pinned to the front.
//
// Returns the locale codes in API order.
func buildPopulatedLocales() []string {
	codes := make([]string, 0, len(catalogs))
	for code := range catalogs {
		if code == DefaultLocale {
			continue
		}
		codes = append(codes, code)
	}
	slices.Sort(codes)

	return append([]string{DefaultLocale}, codes...)
}

// PopulatedLocales returns the list of locale codes that have translations
// available.
//
// Returns a copy, so a caller sorting or truncating the result cannot disturb
// what the next caller sees.
func PopulatedLocales() []string {
	return slices.Clone(populatedLocales)
}

// IsPopulated reports whether the given locale code has translations available.
func IsPopulated(code string) bool {
	_, ok := catalogs[code]
	return ok
}

// T returns the localized message for the given key and locale.
// If the locale has no translation for the key, en-US is used as fallback.
// If the key is not found at all, the key itself is returned.
//
// Optional args are applied via fmt.Sprintf when the message contains
// format verbs (e.g. %s, %d).
//
// Parameters:
//   - locale: BCP 47 locale code (e.g. "en-US"). Falls back to DefaultLocale
//     if empty or not found.
//   - key: dot-separated message key (e.g. "auth.missing_token").
//   - args: optional format arguments applied via fmt.Sprintf.
//
// Returns the localized message string.
func T(locale, key string, args ...any) string {
	if locale == "" {
		locale = DefaultLocale
	}

	// Try the requested locale first.
	if msgs, ok := catalogs[locale]; ok {
		if msg, ok := msgs[key]; ok {
			if len(args) > 0 {
				return fmt.Sprintf(msg, args...)
			}
			return msg
		}
	}

	// Fall back to default locale.
	if locale != DefaultLocale {
		if msgs, ok := catalogs[DefaultLocale]; ok {
			if msg, ok := msgs[key]; ok {
				if len(args) > 0 {
					return fmt.Sprintf(msg, args...)
				}
				return msg
			}
		}
	}

	// Key not found; return it as-is.
	return key
}

// catalogs holds all message translations keyed by locale code.
//
// Entries fall into two kinds. A language catalog is a translation, written
// out in full in its own file. A country catalog is built by derive() from the
// language it belongs to plus the strings that country states differently —
// see derive.go for why that is not the same as copying the base and editing
// it. Both kinds are selectable and both count as populated; the distinction
// is only in how the file is written.
var catalogs = map[string]map[string]string{
	DefaultLocale: enUSMessages,
	"ar-AE":       arAEMessages,
	"ar-EG":       arEGMessages,
	"ar-SA":       arSAMessages,
	"bn-BD":       bnBDMessages,
	"bn-IN":       bnINMessages,
	"cs-CZ":       csCZMessages,
	"da-DK":       daDKMessages,
	"de-AT":       deATMessages,
	"de-CH":       deCHMessages,
	"de-DE":       deDEMessages,
	"en-AU":       enAUMessages,
	"en-CA":       enCAMessages,
	"en-GB":       enGBMessages,
	"en-IN":       enINMessages,
	"en-NZ":       enNZMessages,
	"en-ZA":       enZAMessages,
	"es-AR":       esARMessages,
	"es-ES":       esESMessages,
	"es-MX":       esMXMessages,
	"fi-FI":       fiFIMessages,
	"fr-BE":       frBEMessages,
	"fr-CA":       frCAMessages,
	"fr-CH":       frCHMessages,
	"fr-FR":       frFRMessages,
	"hi-IN":       hiINMessages,
	"hr-HR":       hrHRMessages,
	"hu-HU":       huHUMessages,
	"it-IT":       itITMessages,
	"ja-JP":       jaJPMessages,
	"ko-KR":       koKRMessages,
	"nl-BE":       nlBEMessages,
	"nl-NL":       nlNLMessages,
	"pl-PL":       plPLMessages,
	"pt-BR":       ptBRMessages,
	"pt-PT":       ptPTMessages,
	"ro-RO":       roROMessages,
	"ru-RU":       ruRUMessages,
	"sa-IN":       saINMessages,
	"sk-SK":       skSKMessages,
	"sl-SI":       slSIMessages,
	"sv-SE":       svSEMessages,
	"th-TH":       thTHMessages,
	"tr-TR":       trTRMessages,
	"uk-UA":       ukUAMessages,
	"vi-VN":       viVNMessages,
	"zh-CN":       zhCNMessages,
	"zh-TW":       zhTWMessages,
}

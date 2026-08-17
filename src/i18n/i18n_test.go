// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package i18n

import (
	"slices"
	"strings"
	"testing"
)

func TestTReturnsEnUSMessage(t *testing.T) {
	got := T("en-US", MsgAuthMissingToken)
	want := "missing authorization token"
	if got != want {
		t.Errorf("T(en-US, MsgAuthMissingToken) = %q, want %q", got, want)
	}
}

func TestTFallsBackToEnUSForUnknownLocale(t *testing.T) {
	got := T("xx-XX", MsgAuthMissingToken)
	want := "missing authorization token"
	if got != want {
		t.Errorf("T(xx-XX, MsgAuthMissingToken) = %q, want %q", got, want)
	}
}

func TestTReturnsKeyForUnknownMessage(t *testing.T) {
	got := T("en-US", "nonexistent.key")
	want := "nonexistent.key"
	if got != want {
		t.Errorf("T(en-US, nonexistent.key) = %q, want %q", got, want)
	}
}

func TestTFormatsArgs(t *testing.T) {
	got := T("en-US", MsgSettingNotFound, "default_quota")
	want := `setting "default_quota" not found`
	if got != want {
		t.Errorf("T(en-US, MsgSettingNotFound, default_quota) = %q, want %q", got, want)
	}
}

func TestTEmptyLocaleDefaultsToEnUS(t *testing.T) {
	got := T("", MsgAuthAdminRequired)
	want := "admin access required"
	if got != want {
		t.Errorf("T('', MsgAuthAdminRequired) = %q, want %q", got, want)
	}
}

func TestDefaultLocaleIsEnUS(t *testing.T) {
	if DefaultLocale != "en-US" {
		t.Errorf("DefaultLocale = %q, want %q", DefaultLocale, "en-US")
	}
}

func TestPopulatedLocalesContainsEnUS(t *testing.T) {
	if !slices.Contains(PopulatedLocales(), "en-US") {
		t.Error("PopulatedLocales() does not contain en-US")
	}
}

func TestIsPopulated(t *testing.T) {
	if !IsPopulated("en-US") {
		t.Error("IsPopulated(en-US) = false, want true")
	}
	if IsPopulated("xx-XX") {
		t.Error("IsPopulated(xx-XX) = true, want false")
	}
}

func TestCommonLanguagesNotEmpty(t *testing.T) {
	if len(CommonLanguages) == 0 {
		t.Error("CommonLanguages is empty")
	}
}

func TestExtendedLocalesNotEmpty(t *testing.T) {
	if len(ExtendedLocales) == 0 {
		t.Error("ExtendedLocales is empty")
	}
}

func TestAllMessageKeysHaveEnUSTranslation(t *testing.T) {
	keys := []string{
		MsgAuthMissingToken,
		MsgAuthInvalidSession,
		MsgAuthAdminRequired,
		MsgAuthInvalidCredentials,
		MsgAuthNotConfigured,
		MsgAccountAdminStatusImmutable,
		MsgAccountListError,
		MsgAccountCheckSessions,
		MsgAccountCreateFailed,
		MsgSettingNotFound,
		MsgSettingKeyRequired,
		MsgSettingInvalidBytes,
		MsgSettingsMgrMissing,
		MsgAuditNotConfigured,
		MsgUnitEnableDisableNotAllowed,
		MsgUnitCannotStopController,
		MsgUnitInvalidLines,
		MsgUnitInvalidSince,
		MsgUnitInvalidUntil,
		MsgUnitInvalidPriority,
		MsgRepoInvalidURL,
		MsgPagesNotConfigured,
		MsgPagesGitNotConfigured,
		MsgInstallNoRepoRoot,
		MsgInstallSummaryUpgrade,
		MsgInstallSummaryInstall,
		MsgInstallSummaryImage,
		MsgInstallSummaryVolumes,
		MsgInstallSummaryNewVols,
		MsgInstallSummaryMigrated,
		MsgInstallSummaryNoVols,
		MsgInstallSummaryPorts,
		MsgInstallSummaryConfig,
		MsgInstallSummaryVMImage,
		MsgManifestFieldsRequired,
		MsgManifestNotFound,
		MsgRebuildFieldsRequired,
		MsgRebuildRepoNotConfigured,
		MsgRebuildGitNotConfigured,
		MsgArchiveSubvolumeRequired,
		MsgArchiveFileRequired,
		MsgArchiveUnsupportedFormat,
		MsgArchiveUnpackSuccess,
		MsgPagesDirNotConfigured,
		MsgPagesNameRequired,
		MsgPagesUploadArchiveOnly,
		MsgPagesArchiveRebuildRequired,
		MsgMonitoringNotConfigured,
		MsgUpgradeSettingsMissing,
		MsgAuditCreateFilesystem,
		MsgAuditModifyFilesystem,
		MsgAuditRemoveFilesystem,
		MsgAuditAddRepository,
		MsgAuditRemoveRepository,
		MsgAuditMoveRepository,
		MsgAuditRefreshRepositories,
		MsgAuditInstallPackage,
		MsgAuditUninstallPackage,
		MsgAuditPurgeUninstalledVolumes,
		MsgAuditPurgeVolumes,
		MsgAuditDisablePackage,
		MsgAuditEnablePackage,
		MsgAuditSetUnitStatus,
		MsgAuditCreateAccount,
		MsgAuditUpdateAccount,
		MsgAuditDisableAccount,
		MsgAuditAuthenticate,
		MsgAuditRevokeSession,
		MsgAuditUpdateSetting,
		MsgAuditDismissUpgrades,
		MsgAuditUploadArchive,
		MsgAuditDownloadArchive,
		MsgAuditCreatePage,
		MsgAuditUpdatePage,
		MsgAuditRemovePage,
		MsgAuditRebuildPage,
		MsgAuditUploadPageArchive,
		MsgAuditEnableAccount,
		MsgAuditRebuildGit,
		MsgAuditUploadVMImage,
		MsgAuditDeleteVMImage,
		MsgAuditAddDNSRecord,
		MsgAuditRemoveDNSRecord,
		MsgAuditSetDNSTLD,
		MsgAuditSetupDNS,
		MsgAuditRemovePackageVolume,
		MsgAuditRemovePackageVolumeGroup,
		MsgAuditClearLastResponses,
		MsgAuditSetSystemServiceStatus,
		MsgAuditRefreshSystemServices,
		MsgIngressUnavailableTitle,
		MsgIngressUnavailableBody,
		MsgIngressUnavailableRetry,
		MsgIngressUnavailableFooter,
	}

	for _, key := range keys {
		msg := T("en-US", key)
		if msg == key {
			t.Errorf("message key %q has no en-US translation", key)
		}
	}
}

// The retry page's heading carries the service name and its retry sentence
// carries the interval, both as format verbs, because neither goes in the same
// place in every language. A translation that dropped one renders
// "%!s(MISSING)" to the people reading that language and to nobody else — the
// exact class of breakage a test run in English cannot see.
//
// The body and the footer take no arguments, so a stray verb in one of them is
// the same bug pointed the other way: T does not Sprintf a message it was given
// no arguments for, and the verb reaches the page as literal text.
func TestIngressRetryPageKeepsItsFormatVerbs(t *testing.T) {
	for _, code := range PopulatedLocales() {
		for key, verb := range map[string]string{
			MsgIngressUnavailableTitle: "%s",
			MsgIngressUnavailableRetry: "%d",
		} {
			msg := T(code, key)
			if !strings.Contains(msg, verb) {
				t.Errorf("%s: %q carries no %s: %q", code, key, verb, msg)
			}
			if n := strings.Count(msg, "%"); n != 1 {
				t.Errorf("%s: %q has %d format verbs, want exactly one (%s): %q", code, key, n, verb, msg)
			}
		}
		for _, key := range []string{MsgIngressUnavailableBody, MsgIngressUnavailableFooter} {
			if msg := T(code, key); strings.Contains(msg, "%") {
				t.Errorf("%s: %q takes no arguments but carries a format verb: %q", code, key, msg)
			}
		}
	}
}

func TestNewLocalesArePopulated(t *testing.T) {
	for _, code := range []string{"vi-VN"} {
		if !IsPopulated(code) {
			t.Errorf("IsPopulated(%q) = false, want true", code)
		}
		if _, ok := catalogs[code]; !ok {
			t.Errorf("catalogs is missing an entry for %q", code)
		}
	}
}

// TestPopulatedLocalesCoverEnUSKeys guards against a translated catalog
// drifting from en-US: every locale advertised by PopulatedLocales() must
// define exactly the same key set as the English source of truth, otherwise
// T() silently falls back to English for the missing keys.
func TestPopulatedLocalesCoverEnUSKeys(t *testing.T) {
	for _, code := range PopulatedLocales() {
		msgs, ok := catalogs[code]
		if !ok {
			t.Errorf("PopulatedLocales() lists %q but catalogs has no entry for it", code)
			continue
		}
		for key := range enUSMessages {
			if _, ok := msgs[key]; !ok {
				t.Errorf("locale %q is missing translation for key %q", code, key)
			}
		}
		for key := range msgs {
			if _, ok := enUSMessages[key]; !ok {
				t.Errorf("locale %q has key %q not present in en-US", code, key)
			}
		}
	}
}

// TestLocaleCodesAreRegionQualified keeps the locale lists to the `xx-YY` shape
// every consumer assumes. Sumerian ("sux") was the one entry that broke it — a
// bare ISO 639-3 code with no region subtag — and it is gone: no font a browser
// is likely to have covers the cuneiform block, so the catalog rendered as rows
// of tofu boxes rather than as a language.
func TestLocaleCodesAreRegionQualified(t *testing.T) {
	for name, list := range map[string][]Locale{"CommonLanguages": CommonLanguages, "ExtendedLocales": ExtendedLocales} {
		for _, l := range list {
			if !strings.Contains(l.Code, "-") {
				t.Errorf("%s contains %q, which has no region subtag", name, l.Code)
			}
		}
	}
}

func TestTMultipleFormatArgs(t *testing.T) {
	got := T("en-US", MsgInstallSummaryUpgrade, "myapp", "1.0", "2.0")
	want := "Upgrade myapp from 1.0 to 2.0"
	if got != want {
		t.Errorf("T with multiple args = %q, want %q", got, want)
	}
}

func TestCommonLanguagesHaveRequiredFields(t *testing.T) {
	for _, l := range CommonLanguages {
		if l.Code == "" {
			t.Error("CommonLanguages entry has empty Code")
		}
		if l.NativeName == "" {
			t.Errorf("CommonLanguages entry %q has empty NativeName", l.Code)
		}
		if l.EnglishName == "" {
			t.Errorf("CommonLanguages entry %q has empty EnglishName", l.Code)
		}
	}
}

func TestExtendedLocalesHaveRequiredFields(t *testing.T) {
	for _, l := range ExtendedLocales {
		if l.Code == "" {
			t.Error("ExtendedLocales entry has empty Code")
		}
		if l.NativeName == "" {
			t.Errorf("ExtendedLocales entry %q has empty NativeName", l.Code)
		}
		if l.EnglishName == "" {
			t.Errorf("ExtendedLocales entry %q has empty EnglishName", l.Code)
		}
	}
}

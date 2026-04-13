// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package i18n

import (
	"slices"
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
		MsgAuditClearLastResponses,
		MsgAuditSetSystemServiceStatus,
		MsgAuditRefreshSystemServices,
	}

	for _, key := range keys {
		msg := T("en-US", key)
		if msg == key {
			t.Errorf("message key %q has no en-US translation", key)
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

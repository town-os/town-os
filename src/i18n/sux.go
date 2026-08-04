package i18n

// suxMessages contains Sumerian translations. Sumerian is an extinct language
// with no native vocabulary for modern computing, so these strings are an
// intentional approximation: each value pairs cuneiform glyphs with a
// romanized transliteration in parentheses, reusing a small core lexicon
// (dub = tablet/record, e₂-dub = tablet-house/repository, lugal = king/admin,
// niĝ-šid = reckoning/account, kur = land/network, and so on).
var suxMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "𒁾 𒉡𒅅 (dub nu-ĝal₂)",
	MsgAuthInvalidSession: "𒁺 𒉡𒍣 (tuš nu-zid)",
	MsgAuthAdminRequired:  "𒈗 𒃶 (lugal he₂)",

	// Authentication.
	MsgAuthInvalidCredentials: "𒈬 𒉡𒍣 (mu nu-zid)",

	// Account management.
	MsgAccountAdminStatusImmutable: "𒈗 𒉆 𒉡𒆐 (lugal nam nu-kur₂)",
	MsgAccountListError:            "𒃻𒉏 𒉺𒌅 (niĝ-šid pad₃)",
	MsgAccountCheckSessions:        "𒈗 𒁺 𒅆 (lugal tuš igi)",
	MsgAccountCreateFailed:         "𒃻𒉏 𒁶 𒉡𒅅 (niĝ-šid dim₂ nu-ĝal₂)",

	// Settings.
	MsgSettingNotFound:     "𒄑𒄯 %q 𒉡𒉺𒌅 (ĝeš-hur %q nu-pad₃)",
	MsgSettingKeyRequired:  "𒈬 𒃶 (mu he₂)",
	MsgSettingInvalidBytes: "𒃻 𒉡𒍣 %q: %v (niĝ nu-zid %q: %v)",
	MsgSettingsMgrMissing:  "𒄑𒄯 𒇽 𒉡𒅅 (ĝeš-hur lu₂ nu-ĝal₂)",

	// Audit.
	MsgAuditNotConfigured: "𒁾𒉏 𒉡𒃻 (dub-šid nu-ĝar)",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "𒅅 𒉡𒅅 𒉡𒋧 (ĝal₂ nu-ĝal₂ nu-šum₂)",
	MsgUnitCannotStopController:    "systemcontroller 𒉡𒄄 (nu-gi₄)",
	MsgUnitInvalidLines:            "lines 𒉡𒍣 (nu-zid)",
	MsgUnitInvalidSince:            "since 𒉡𒍣 (nu-zid)",
	MsgUnitInvalidUntil:            "until 𒉡𒍣 (nu-zid)",
	MsgUnitInvalidPriority:         "priority 𒉡𒍣 (nu-zid)",

	// Repository management.
	MsgRepoInvalidURL: "url 𒉡𒍣 (nu-zid)",

	// Pages management.
	MsgPagesNotConfigured:    "𒅎 𒉡𒃻 (im nu-ĝar)",
	MsgPagesGitNotConfigured: "git 𒅇 𒅎𒆠 𒉡𒃻 (git u₃ im-ki nu-ĝar)",

	// Package installation.
	MsgInstallNoRepoRoot:      "𒂍𒁾 𒊕 𒉡𒃻 (e-dub saĝ nu-ĝar)",
	MsgInstallSummaryUpgrade:  "𒄈 (gibil) %s: %s → %s",
	MsgInstallSummaryInstall:  "𒃻 (ĝar) %s %s",
	MsgInstallSummaryImage:    "𒀩 (alan): %s",
	MsgInstallSummaryVolumes:  "%d 𒆠𒃻 (ki-ĝar)",
	MsgInstallSummaryNewVols:  "%d 𒄈 (gibil)",
	MsgInstallSummaryMigrated: "%d 𒁺 (ĝen)",
	MsgInstallSummaryNoVols:   "𒆠𒃻 𒉡 (ki-ĝar nu)",
	MsgInstallSummaryPorts:    "𒅗 𒁇 (ka bar): %s",
	MsgInstallSummaryConfig:   "𒄑𒄯 𒃶 (ĝeš-hur he₂)",
	MsgInstallSummaryVMImage:  "VM 𒀩 (alan): %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name, version 𒃶 (he₂)",
	MsgManifestNotFound:       "𒃻 𒁾 𒉡𒉺𒌅 (niĝ dub nu-pad₃): %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, name, version 𒃶 (he₂)",
	MsgRebuildRepoNotConfigured: "𒂍𒁾 𒊕 𒉡𒃻 (e-dub saĝ nu-ĝar)",
	MsgRebuildGitNotConfigured:  "git 𒉡𒃻 (git nu-ĝar)",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume 𒃶 (he₂)",
	MsgArchiveFileRequired:      "𒉑𒁾 𒃶 (pisaĝ-dub he₂): %v",
	MsgArchiveUnsupportedFormat: "𒋗𒋾 𒄑𒄯 𒉡𒍣 (šu-ti ĝeš-hur nu-zid): %s",
	MsgArchiveUnpackSuccess:     "𒉑𒁾 𒁁 𒍣 (pisaĝ-dub bad zid)",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "𒅎𒆠 𒉡𒃻 (im-ki nu-ĝar)",
	MsgPagesNameRequired:           "name 𒃶 (he₂)",
	MsgPagesUploadArchiveOnly:      "𒅍 𒉑𒁾 𒅎 𒁹 (il₂ pisaĝ-dub im aš)",
	MsgPagesArchiveRebuildRequired: "𒉑𒁾 𒅎 𒄈 𒁶: 𒅍 𒉑𒁾 𒄈 /pages/upload (pisaĝ-dub im gibil dim₂: il₂ pisaĝ-dub gibil)",

	// Monitoring.
	MsgMonitoringNotConfigured: "𒅆 𒉡𒃻 (igi nu-ĝar)",

	// Upgrades.
	MsgUpgradeSettingsMissing: "𒄑𒄯 𒇽 𒉡𒅅 (ĝeš-hur lu₂ nu-ĝal₂)",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "𒁶 𒆠𒁾 (dim₂ ki-dub)",
	MsgAuditModifyFilesystem:         "𒆐 𒆠𒁾 (kur₂ ki-dub)",
	MsgAuditRemoveFilesystem:         "𒄢 𒆠𒁾 (gul ki-dub)",
	MsgAuditAddRepository:            "𒁕 𒂍𒁾 (dah e-dub)",
	MsgAuditRemoveRepository:         "𒄢 𒂍𒁾 (gul e-dub)",
	MsgAuditMoveRepository:           "𒁺 𒂍𒁾 (ĝen e-dub)",
	MsgAuditRefreshRepositories:      "𒄈 𒂍𒁾 (gibil e-dub)",
	MsgAuditInstallPackage:           "𒁺 𒃻 (gub niĝ)",
	MsgAuditUninstallPackage:         "𒍣 𒃻 (zi niĝ)",
	MsgAuditPurgeUninstalledVolumes:  "𒄢 𒆠𒃻 𒍣𒀀 (gul ki-ĝar zi-a)",
	MsgAuditPurgeVolumes:             "𒄢 𒆠𒃻 (gul ki-ĝar)",
	MsgAuditDisablePackage:           "𒉡𒅅 𒃻 (nu-ĝal₂ niĝ)",
	MsgAuditEnablePackage:            "𒅅 𒃻 (ĝal₂ niĝ)",
	MsgAuditSetUnitStatus:            "𒃻 𒉆 (ĝar nam)",
	MsgAuditCreateAccount:            "𒁶 𒃻𒉏 (dim₂ niĝ-šid)",
	MsgAuditUpdateAccount:            "𒆐 𒃻𒉏 (kur₂ niĝ-šid)",
	MsgAuditDisableAccount:           "𒉡𒅅 𒃻𒉏 (nu-ĝal₂ niĝ-šid)",
	MsgAuditAuthenticate:             "𒍣 (zid)",
	MsgAuditRevokeSession:            "𒍣 𒁺 (zi tuš)",
	MsgAuditUpdateSetting:            "𒆐 𒄑𒄯 (kur₂ ĝeš-hur)",
	MsgAuditDismissUpgrades:          "𒄢 𒄈 (gul gibil)",
	MsgAuditUploadArchive:            "𒅍 𒉑𒁾 (il₂ pisaĝ-dub)",
	MsgAuditDownloadArchive:          "𒋗𒋾 𒉑𒁾 (šu-ti pisaĝ-dub)",
	MsgAuditCreatePage:               "𒁶 𒅎 (dim₂ im)",
	MsgAuditUpdatePage:               "𒆐 𒅎 (kur₂ im)",
	MsgAuditRemovePage:               "𒄢 𒅎 (gul im)",
	MsgAuditRebuildPage:              "𒄈 𒅎 (gibil im)",
	MsgAuditUploadPageArchive:        "𒅍 𒅎 𒉑𒁾 (il₂ im pisaĝ-dub)",
	MsgAuditEnableAccount:            "𒅅 𒃻𒉏 (ĝal₂ niĝ-šid)",
	MsgAuditRebuildGit:               "𒄈 git (gibil git)",
	MsgAuditUploadVMImage:            "𒅍 VM 𒀩 (il₂ VM alan)",
	MsgAuditDeleteVMImage:            "𒄢 VM 𒀩 (gul VM alan)",
	MsgAuditAddDNSRecord:             "𒁕 DNS 𒈬 (dah DNS mu)",
	MsgAuditRemoveDNSRecord:          "𒄢 DNS 𒈬 (gul DNS mu)",
	MsgAuditSetDNSTLD:                "𒃻 DNS TLD (ĝar DNS TLD)",
	MsgAuditSetupDNS:                 "𒃻 DNS (ĝar DNS)",
	MsgAuditRemovePackageVolume:      "𒄢 𒆠𒃻 (gul ki-ĝar)",
	MsgAuditRemovePackageVolumeGroup: "𒄢 𒆠𒃻 𒂗 (gul ki-ĝar erin₂)",
	MsgAuditClearLastResponses:       "𒄢 𒄄 (gul gi₄)",
	MsgAuditSetSystemServiceStatus:   "𒃻 𒀴 𒉆 (ĝar arad nam)",
	MsgAuditRefreshSystemServices:    "𒄈 𒀴 (gibil arad)",
	MsgAuditCreateNetwork:            "𒁶 𒆳 (dim₂ kur)",
	MsgAuditRemoveNetwork:            "𒄢 𒆳 (gul kur)",
	MsgAuditEnableNetwork:            "𒅅 𒆳 (ĝal₂ kur)",
	MsgAuditDisableNetwork:           "𒉡𒅅 𒆳 (nu-ĝal₂ kur)",
	MsgAuditAddNetworkPeer:           "𒁕 𒇽 𒆳 (dah lu₂ kur)",
	MsgAuditRemoveNetworkPeer:        "𒄢 𒇽 𒆳 (gul lu₂ kur)",
	MsgAuditRefreshNetworkPeer:       "𒄈 𒇽 𒆳 (gibil lu₂ kur)",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "𒃻𒉏 𒆳 𒂍𒃻 𒁹 (niĝ-šid-e kur e-ĝar aš)",
	MsgAuthNetworkOnlyNetworkDenied: "𒃻𒉏 𒆳𒁀 𒉡𒋧 (niĝ-šid kur-ba nu-šum₂)",
	MsgAuthWireGuardPeerNotOwned:  "𒃻𒉏 𒇽𒉌 𒄈 (niĝ-šid lu₂-ni gibil)",
	MsgAuthObjectStorageRequired:  "𒈗 𒂍𒃻 𒃶 (lugal e-ĝar he₂)",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "𒉑𒁾 𒉡 𒂍𒃻 (pisaĝ-dub nu e-ĝar)",
	MsgGfehNotConfigured:         "𒂍𒃻 𒉡𒃻 (e-ĝar nu-ĝar)",
	MsgGfehNameRequired:          "𒈬 𒃶 (mu he₂)",
	MsgGfehPartitionExists:       "𒁀 𒅅𒀀 (ba ĝal₂-a)",
	MsgGfehPartitionNotFound:     "𒁀 𒉡𒅅 (ba nu-ĝal₂)",
	MsgGfehNetworkRequired:       "𒆳 𒃶 (kur he₂)",
	MsgGfehPrincipalRequired:     "𒇽 𒃶 (lu₂ he₂)",
	MsgGfehPathRequired:          "𒆠 𒃶 (ki he₂)",
	MsgGfehUnknownAccount:        "𒃻𒉏 𒉡𒅅 (niĝ-šid nu-ĝal₂)",
	MsgAuditCreateGfehPartition:  "𒁶 𒁀 𒂍𒃻 (dim₂ ba e-ĝar)",
	MsgAuditModifyGfehPartition:  "𒆐 𒁀 𒂍𒃻 (kur₂ ba e-ĝar)",
	MsgAuditRemoveGfehPartition:  "𒄢 𒁀 𒂍𒃻 (gul ba e-ĝar)",
	MsgAuditAddGfehPrincipal:     "𒁕 𒇽 𒂍𒃻 (dah lu₂ e-ĝar)",
	MsgAuditRemoveGfehPrincipal:  "𒄢 𒇽 𒂍𒃻 (gul lu₂ e-ĝar)",
	MsgAuditAddGfehGrant:         "𒁕 𒋗 𒂍𒃻 (dah šu e-ĝar)",
	MsgAuditRevokeGfehGrant:      "𒄢 𒋗 𒂍𒃻 (gul šu e-ĝar)",
	MsgAuditWithdrawGfehExposure: "𒄢 𒁾 𒂍𒃻 (gul dub e-ĝar)",
}

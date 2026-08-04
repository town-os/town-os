package i18n

// daDKMessages contains all Danish translations.
var daDKMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "manglende autorisationstoken",
	MsgAuthInvalidSession: "ugyldig session",
	MsgAuthAdminRequired:  "administratoradgang kræves",

	// Authentication.
	MsgAuthInvalidCredentials: "ugyldige loginoplysninger",

	// Account management.
	MsgAccountAdminStatusImmutable: "administratorstatus kan ikke ændres efter oprettelse af konto",
	MsgAccountListError:            "vis konti",
	MsgAccountCheckSessions:        "tjek aktive administratorsessioner",
	MsgAccountCreateFailed:         "oprettelse af konto mislykkedes",

	// Settings.
	MsgSettingNotFound:     "indstilling %q blev ikke fundet",
	MsgSettingKeyRequired:  "nøgle er påkrævet",
	MsgSettingInvalidBytes: "ugyldig byteværdi for %q: %v",
	MsgSettingsMgrMissing:  "indstillingshåndtering ikke tilgængelig",

	// Audit.
	MsgAuditNotConfigured: "revisionslogning er ikke konfigureret",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "aktivér/deaktivér er ikke tilladt",
	MsgUnitCannotStopController:    "kan ikke stoppe systemcontroller",
	MsgUnitInvalidLines:            "ugyldig lines-parameter",
	MsgUnitInvalidSince:            "ugyldig since-parameter",
	MsgUnitInvalidUntil:            "ugyldig until-parameter",
	MsgUnitInvalidPriority:         "ugyldig priority-parameter",

	// Repository management.
	MsgRepoInvalidURL: "ugyldig url",

	// Pages management.
	MsgPagesNotConfigured:    "pages er ikke konfigureret",
	MsgPagesGitNotConfigured: "git-klient eller pages-mappe er ikke konfigureret",

	// Package installation.
	MsgInstallNoRepoRoot:      "ingen repository-rod er konfigureret",
	MsgInstallSummaryUpgrade:  "Opgradér %s fra %s til %s",
	MsgInstallSummaryInstall:  "Installér %s %s",
	MsgInstallSummaryImage:    "Image: %s",
	MsgInstallSummaryVolumes:  "%d volume(r)",
	MsgInstallSummaryNewVols:  "%d nye",
	MsgInstallSummaryMigrated: "%d migreret",
	MsgInstallSummaryNoVols:   "Ingen volumes",
	MsgInstallSummaryPorts:    "Eksterne porte: %s",
	MsgInstallSummaryConfig:   "Konfiguration påkrævet",
	MsgInstallSummaryVMImage:  "VM-image: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, navn og version er påkrævet",
	MsgManifestNotFound:       "pakkemanifest blev ikke fundet: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repo, navn og version er påkrævet",
	MsgRebuildRepoNotConfigured: "repository-rod er ikke konfigureret",
	MsgRebuildGitNotConfigured:  "git-klient er ikke konfigureret",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume-feltet er påkrævet",
	MsgArchiveFileRequired:      "arkivfil påkrævet: %v",
	MsgArchiveUnsupportedFormat: "ikke-understøttet downloadformat: %s",
	MsgArchiveUnpackSuccess:     "arkiv udpakket korrekt",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "pages-mappe er ikke konfigureret",
	MsgPagesNameRequired:           "navnefeltet er påkrævet",
	MsgPagesUploadArchiveOnly:      "upload er kun tilladt for pages af arkivtype",
	MsgPagesArchiveRebuildRequired: "arkiv-pages skal genopbygges ved at uploade et nyt arkiv via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "overvågning er ikke konfigureret",

	// Upgrades.
	MsgUpgradeSettingsMissing: "indstillingshåndtering ikke tilgængelig",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "opret filsystem",
	MsgAuditModifyFilesystem:        "ændr filsystem",
	MsgAuditRemoveFilesystem:        "fjern filsystem",
	MsgAuditAddRepository:           "tilføj repository",
	MsgAuditRemoveRepository:        "fjern repository",
	MsgAuditMoveRepository:          "flyt repository",
	MsgAuditRefreshRepositories:     "opdatér repositories",
	MsgAuditInstallPackage:          "installér pakke",
	MsgAuditUninstallPackage:        "afinstallér pakke",
	MsgAuditPurgeUninstalledVolumes: "slet afinstallerede volumes",
	MsgAuditPurgeVolumes:            "slet volumes",
	MsgAuditDisablePackage:          "deaktivér pakke",
	MsgAuditEnablePackage:           "aktivér pakke",
	MsgAuditSetUnitStatus:           "sæt unit-status",
	MsgAuditCreateAccount:           "opret konto",
	MsgAuditUpdateAccount:           "opdatér konto",
	MsgAuditDisableAccount:          "deaktivér konto",
	MsgAuditAuthenticate:            "godkend",
	MsgAuditRevokeSession:           "tilbagekald session",
	MsgAuditUpdateSetting:           "opdatér indstilling",
	MsgAuditDismissUpgrades:         "afvis pakkeopgraderinger",
	MsgAuditUploadArchive:           "upload arkiv",
	MsgAuditDownloadArchive:         "download arkiv",
	MsgAuditCreatePage:              "opret page",
	MsgAuditUpdatePage:              "opdatér page",
	MsgAuditRemovePage:              "fjern page",
	MsgAuditRebuildPage:             "genopbyg page",
	MsgAuditUploadPageArchive:       "upload page-arkiv",
	MsgAuditEnableAccount:           "aktivér konto",
	MsgAuditRebuildGit:              "genopbyg git",
	MsgAuditUploadVMImage:           "upload vm-image",
	MsgAuditDeleteVMImage:           "slet vm-image",
	MsgAuditAddDNSRecord:            "tilføj dns-post",
	MsgAuditRemoveDNSRecord:         "fjern dns-post",
	MsgAuditSetDNSTLD:               "sæt dns-tld",
	MsgAuditSetupDNS:                "opsæt dns",
	MsgAuditRemovePackageVolume:     "fjern pakkevolume",
	MsgAuditRemovePackageVolumeGroup: "fjern pakkevolumegruppe",
	MsgAuditClearLastResponses:      "ryd cachelagrede installationssvar",
	MsgAuditSetSystemServiceStatus:  "sæt systemtjenestestatus",
	MsgAuditRefreshSystemServices:   "opdatér systemtjenester",
	MsgAuditCreateNetwork:           "opret netværk",
	MsgAuditRemoveNetwork:           "fjern netværk",
	MsgAuditEnableNetwork:           "aktivér netværk",
	MsgAuditDisableNetwork:          "deaktivér netværk",
	MsgAuditAddNetworkPeer:          "tilføj netværkspeer",
	MsgAuditRemoveNetworkPeer:       "fjern netværkspeer",
	MsgAuditRefreshNetworkPeer:      "opdatér netværkspeer",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "denne konto må kun bruge endepunkter for netværkstilmelding og objektlagring",
	MsgAuthNetworkOnlyNetworkDenied: "denne konto har ikke tilladelse på det netværk",
	MsgAuthWireGuardPeerNotOwned:  "denne konto må kun opdatere peers, den selv har tilmeldt",
	MsgAuthObjectStorageRequired:  "administrator- eller objektlageradgang kræves",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "arkivupload og -download kan ikke adressere en objektlagerpartition",
	MsgGfehNotConfigured:         "objektlager er ikke konfigureret",
	MsgGfehNameRequired:          "navnefeltet er påkrævet",
	MsgGfehPartitionExists:       "partitionen findes allerede",
	MsgGfehPartitionNotFound:     "partitionen blev ikke fundet",
	MsgGfehNetworkRequired:       "netværksfeltet er påkrævet",
	MsgGfehPrincipalRequired:     "principalfeltet er påkrævet",
	MsgGfehPathRequired:          "stifeltet er påkrævet",
	MsgGfehUnknownAccount:        "ingen sådan konto",
	MsgAuditCreateGfehPartition:  "opret objektlagerpartition",
	MsgAuditModifyGfehPartition:  "ændr objektlagerpartition",
	MsgAuditRemoveGfehPartition:  "fjern objektlagerpartition",
	MsgAuditAddGfehPrincipal:     "tilføj objektlagerbruger",
	MsgAuditRemoveGfehPrincipal:  "fjern objektlagerbruger",
	MsgAuditAddGfehGrant:         "tilføj objektlagertilladelse",
	MsgAuditRevokeGfehGrant:      "tilbagekald objektlagertilladelse",
	MsgAuditWithdrawGfehExposure: "tilbagekald objektlagerlink",
}

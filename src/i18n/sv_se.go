package i18n

// svSEMessages contains all Swedish translations.
var svSEMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "auktoriseringstoken saknas",
	MsgAuthInvalidSession: "ogiltig session",
	MsgAuthAdminRequired:  "administratörsåtkomst krävs",

	// Authentication.
	MsgAuthInvalidCredentials: "ogiltiga inloggningsuppgifter",

	// Account management.
	MsgAccountAdminStatusImmutable: "administratörsstatus kan inte ändras efter att kontot skapats",
	MsgAccountListError:            "lista konton",
	MsgAccountCheckSessions:        "kontrollera aktiva administratörssessioner",
	MsgAccountCreateFailed:         "det gick inte att skapa kontot",

	// Settings.
	MsgSettingNotFound:     "inställningen %q hittades inte",
	MsgSettingKeyRequired:  "nyckel krävs",
	MsgSettingInvalidBytes: "ogiltigt bytevärde för %q: %v",
	MsgSettingsMgrMissing:  "inställningshanteraren är inte tillgänglig",

	// Audit.
	MsgAuditNotConfigured: "granskningsloggning är inte konfigurerad",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "aktivera/inaktivera tillåts inte",
	MsgUnitCannotStopController:    "kan inte stoppa systemcontroller",
	MsgUnitInvalidLines:            "ogiltig lines-parameter",
	MsgUnitInvalidSince:            "ogiltig since-parameter",
	MsgUnitInvalidUntil:            "ogiltig until-parameter",
	MsgUnitInvalidPriority:         "ogiltig priority-parameter",

	// Repository management.
	MsgRepoInvalidURL: "ogiltig url",

	// Pages management.
	MsgPagesNotConfigured:    "sidor är inte konfigurerade",
	MsgPagesGitNotConfigured: "git-klient eller sidkatalog är inte konfigurerad",

	// Package installation.
	MsgInstallNoRepoRoot:      "ingen förvaringsplatsrot konfigurerad",
	MsgInstallSummaryUpgrade:  "Uppgradera %s från %s till %s",
	MsgInstallSummaryInstall:  "Installera %s %s",
	MsgInstallSummaryImage:    "Avbild: %s",
	MsgInstallSummaryVolumes:  "%d volym(er)",
	MsgInstallSummaryNewVols:  "%d nya",
	MsgInstallSummaryMigrated: "%d migrerade",
	MsgInstallSummaryNoVols:   "Inga volymer",
	MsgInstallSummaryPorts:    "Externa portar: %s",
	MsgInstallSummaryConfig:   "Konfiguration krävs",
	MsgInstallSummaryVMImage:  "VM-avbild: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, namn och version krävs",
	MsgManifestNotFound:       "paketmanifest hittades inte: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, namn och version krävs",
	MsgRebuildRepoNotConfigured: "förvaringsplatsrot är inte konfigurerad",
	MsgRebuildGitNotConfigured:  "git-klient är inte konfigurerad",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "fältet subvolume krävs",
	MsgArchiveFileRequired:      "arkivfil krävs: %v",
	MsgArchiveUnsupportedFormat: "nedladdningsformat som inte stöds: %s",
	MsgArchiveUnpackSuccess:     "arkivet packades upp korrekt",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "sidkatalog är inte konfigurerad",
	MsgPagesNameRequired:           "fältet namn krävs",
	MsgPagesUploadArchiveOnly:      "uppladdning tillåts endast för sidor av arkivtyp",
	MsgPagesArchiveRebuildRequired: "arkivsidor måste byggas om genom att ladda upp ett nytt arkiv via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "övervakning är inte konfigurerad",

	// Upgrades.
	MsgUpgradeSettingsMissing: "inställningshanteraren är inte tillgänglig",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "skapa filsystem",
	MsgAuditModifyFilesystem:         "ändra filsystem",
	MsgAuditRemoveFilesystem:         "ta bort filsystem",
	MsgAuditAddRepository:            "lägg till förvaringsplats",
	MsgAuditRemoveRepository:         "ta bort förvaringsplats",
	MsgAuditMoveRepository:           "flytta förvaringsplats",
	MsgAuditRefreshRepositories:      "uppdatera förvaringsplatser",
	MsgAuditInstallPackage:           "installera paket",
	MsgAuditUninstallPackage:         "avinstallera paket",
	MsgAuditPurgeUninstalledVolumes:  "rensa avinstallerade volymer",
	MsgAuditPurgeVolumes:             "rensa volymer",
	MsgAuditDisablePackage:           "inaktivera paket",
	MsgAuditEnablePackage:            "aktivera paket",
	MsgAuditSetUnitStatus:            "ange enhetsstatus",
	MsgAuditCreateAccount:            "skapa konto",
	MsgAuditUpdateAccount:            "uppdatera konto",
	MsgAuditDisableAccount:           "inaktivera konto",
	MsgAuditAuthenticate:             "autentisera",
	MsgAuditRevokeSession:            "återkalla session",
	MsgAuditUpdateSetting:            "uppdatera inställning",
	MsgAuditDismissUpgrades:          "avfärda paketuppgraderingar",
	MsgAuditUploadArchive:            "ladda upp arkiv",
	MsgAuditDownloadArchive:          "ladda ner arkiv",
	MsgAuditCreatePage:               "skapa sida",
	MsgAuditUpdatePage:               "uppdatera sida",
	MsgAuditRemovePage:               "ta bort sida",
	MsgAuditRebuildPage:              "bygg om sida",
	MsgAuditUploadPageArchive:        "ladda upp sidarkiv",
	MsgAuditEnableAccount:            "aktivera konto",
	MsgAuditRebuildGit:               "bygg om git",
	MsgAuditUploadVMImage:            "ladda upp vm-avbild",
	MsgAuditDeleteVMImage:            "ta bort vm-avbild",
	MsgAuditAddDNSRecord:             "lägg till dns-post",
	MsgAuditRemoveDNSRecord:          "ta bort dns-post",
	MsgAuditSetDNSTLD:                "ange dns-tld",
	MsgAuditSetupDNS:                 "konfigurera dns",
	MsgAuditRemovePackageVolume:      "ta bort paketvolym",
	MsgAuditRemovePackageVolumeGroup: "ta bort paketvolymgrupp",
	MsgAuditClearLastResponses:       "rensa cachade installationssvar",
	MsgAuditSetSystemServiceStatus:   "ange systemtjänststatus",
	MsgAuditRefreshSystemServices:    "uppdatera systemtjänster",
	MsgAuditCreateNetwork:            "skapa nätverk",
	MsgAuditRemoveNetwork:            "ta bort nätverk",
	MsgAuditEnableNetwork:            "aktivera nätverk",
	MsgAuditDisableNetwork:           "inaktivera nätverk",
	MsgAuditAddNetworkPeer:           "lägg till nätverkspeer",
	MsgAuditRemoveNetworkPeer:        "ta bort nätverkspeer",
	MsgAuditRefreshNetworkPeer:       "uppdatera nätverkspeer",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "detta konto får endast använda slutpunkter för nätverksregistrering och objektlagring",
	MsgAuthNetworkOnlyNetworkDenied: "detta konto är inte tillåtet på det nätverket",
	MsgAuthWireGuardPeerNotOwned:    "detta konto får endast uppdatera peers som det registrerat",
	MsgAuthSessionNotOwned:          "detta konto får endast återkalla sina egna sessioner",
	MsgAuthObjectStorageRequired:    "administratörs- eller objektlagringsåtkomst krävs",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "arkivuppladdningar och -nedladdningar kan inte adressera en objektlagringspartition",
	MsgGfehNotConfigured:         "objektlagring är inte konfigurerad",
	MsgGfehNameRequired:          "namnfältet krävs",
	MsgGfehPartitionExists:       "partitionen finns redan",
	MsgGfehPartitionNotFound:     "partitionen hittades inte",
	MsgGfehNetworkRequired:       "nätverksfältet krävs",
	MsgGfehPrincipalRequired:     "principalfältet krävs",
	MsgGfehPathRequired:          "sökvägsfältet krävs",
	MsgGfehUnknownAccount:        "kontot finns inte",
	MsgAuditCreateGfehPartition:  "skapa objektlagringspartition",
	MsgAuditModifyGfehPartition:  "ändra objektlagringspartition",
	MsgAuditRemoveGfehPartition:  "ta bort objektlagringspartition",
	MsgAuditAddGfehPrincipal:     "lägg till objektlagringsanvändare",
	MsgAuditRemoveGfehPrincipal:  "ta bort objektlagringsanvändare",
	MsgAuditAddGfehGrant:         "lägg till objektlagringsbehörighet",
	MsgAuditRevokeGfehGrant:      "återkalla objektlagringsbehörighet",
	MsgAuditWithdrawGfehExposure: "dra tillbaka objektlagringslänk",
}

package i18n

// skSKMessages contains all Slovak translations.
var skSKMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "chýba autorizačný token",
	MsgAuthInvalidSession: "neplatná relácia",
	MsgAuthAdminRequired:  "vyžaduje sa prístup správcu",

	// Authentication.
	MsgAuthInvalidCredentials: "neplatné prihlasovacie údaje",
	MsgAuthNotConfigured:      "overovanie nie je nakonfigurované",

	// Account management.
	MsgAccountAdminStatusImmutable: "stav správcu nie je možné po vytvorení účtu zmeniť",
	MsgAccountListError:            "vypísať účty",
	MsgAccountCheckSessions:        "skontrolovať aktívne relácie správcov",
	MsgAccountCreateFailed:         "vytvorenie účtu zlyhalo",

	// Settings.
	MsgSettingNotFound:     "nastavenie %q sa nenašlo",
	MsgSettingKeyRequired:  "kľúč je povinný",
	MsgSettingInvalidBytes: "neplatná bajtová hodnota pre %q: %v",
	MsgSettingsMgrMissing:  "správca nastavení nie je k dispozícii",

	// Audit.
	MsgAuditNotConfigured: "protokolovanie auditu nie je nakonfigurované",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "povolenie/zakázanie nie je dovolené",
	MsgUnitCannotStopController:    "systemcontroller nie je možné zastaviť",
	MsgUnitInvalidLines:            "neplatný parameter lines",
	MsgUnitInvalidSince:            "neplatný parameter since",
	MsgUnitInvalidUntil:            "neplatný parameter until",
	MsgUnitInvalidPriority:         "neplatný parameter priority",

	// Repository management.
	MsgRepoInvalidURL: "neplatná url",

	// Pages management.
	MsgPagesNotConfigured:    "pages nie sú nakonfigurované",
	MsgPagesGitNotConfigured: "git klient alebo adresár pages nie sú nakonfigurované",

	// Package installation.
	MsgInstallNoRepoRoot:      "nie je nakonfigurovaný žiadny koreň repozitára",
	MsgInstallSummaryUpgrade:  "Aktualizovať %s z %s na %s",
	MsgInstallSummaryInstall:  "Nainštalovať %s %s",
	MsgInstallSummaryImage:    "Obraz: %s",
	MsgInstallSummaryVolumes:  "%d zväzkov",
	MsgInstallSummaryNewVols:  "%d nových",
	MsgInstallSummaryMigrated: "%d migrovaných",
	MsgInstallSummaryNoVols:   "Žiadne zväzky",
	MsgInstallSummaryPorts:    "Externé porty: %s",
	MsgInstallSummaryConfig:   "Vyžaduje sa konfigurácia",
	MsgInstallSummaryVMImage:  "Obraz VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, názov a verzia sú povinné",
	MsgManifestNotFound:       "manifest balíka sa nenašiel: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, názov a verzia sú povinné",
	MsgRebuildRepoNotConfigured: "koreň repozitára nie je nakonfigurovaný",
	MsgRebuildGitNotConfigured:  "git klient nie je nakonfigurovaný",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "pole subvolume je povinné",
	MsgArchiveFileRequired:      "vyžaduje sa súbor archívu: %v",
	MsgArchiveUnsupportedFormat: "nepodporovaný formát sťahovania: %s",
	MsgArchiveUnpackSuccess:     "archív bol úspešne rozbalený",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "adresár pages nie je nakonfigurovaný",
	MsgPagesNameRequired:           "pole názov je povinné",
	MsgPagesUploadArchiveOnly:      "nahrávanie je povolené iba pre stránky typu archív",
	MsgPagesArchiveRebuildRequired: "stránky typu archív je potrebné znovu zostaviť nahraním nového archívu cez /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitorovanie nie je nakonfigurované",

	// Upgrades.
	MsgUpgradeSettingsMissing: "správca nastavení nie je k dispozícii",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "vytvoriť súborový systém",
	MsgAuditModifyFilesystem:         "upraviť súborový systém",
	MsgAuditRemoveFilesystem:         "odstrániť súborový systém",
	MsgAuditAddRepository:            "pridať repozitár",
	MsgAuditRemoveRepository:         "odstrániť repozitár",
	MsgAuditMoveRepository:           "presunúť repozitár",
	MsgAuditRefreshRepositories:      "obnoviť repozitáre",
	MsgAuditInstallPackage:           "nainštalovať balík",
	MsgAuditUninstallPackage:         "odinštalovať balík",
	MsgAuditPurgeUninstalledVolumes:  "vyčistiť odinštalované zväzky",
	MsgAuditPurgeVolumes:             "vyčistiť zväzky",
	MsgAuditDisablePackage:           "zakázať balík",
	MsgAuditEnablePackage:            "povoliť balík",
	MsgAuditSetUnitStatus:            "nastaviť stav jednotky",
	MsgAuditCreateAccount:            "vytvoriť účet",
	MsgAuditUpdateAccount:            "aktualizovať účet",
	MsgAuditDisableAccount:           "zakázať účet",
	MsgAuditAuthenticate:             "overiť",
	MsgAuditRevokeSession:            "odvolať reláciu",
	MsgAuditUpdateSetting:            "aktualizovať nastavenie",
	MsgAuditDismissUpgrades:          "zamietnuť aktualizácie balíkov",
	MsgAuditUploadArchive:            "nahrať archív",
	MsgAuditDownloadArchive:          "stiahnuť archív",
	MsgAuditCreatePage:               "vytvoriť stránku",
	MsgAuditUpdatePage:               "aktualizovať stránku",
	MsgAuditRemovePage:               "odstrániť stránku",
	MsgAuditRebuildPage:              "znovu zostaviť stránku",
	MsgAuditUploadPageArchive:        "nahrať archív stránky",
	MsgAuditEnableAccount:            "povoliť účet",
	MsgAuditRebuildGit:               "znovu zostaviť git",
	MsgAuditUploadVMImage:            "nahrať obraz vm",
	MsgAuditDeleteVMImage:            "odstrániť obraz vm",
	MsgAuditAddDNSRecord:             "pridať dns záznam",
	MsgAuditRemoveDNSRecord:          "odstrániť dns záznam",
	MsgAuditSetDNSTLD:                "nastaviť dns tld",
	MsgAuditSetupDNS:                 "nastaviť dns",
	MsgAuditRemovePackageVolume:      "odstrániť zväzok balíka",
	MsgAuditRemovePackageVolumeGroup: "odstrániť skupinu zväzkov balíka",
	MsgAuditClearLastResponses:       "vymazať uložené odpovede inštalácie",
	MsgAuditSetSystemServiceStatus:   "nastaviť stav systémovej služby",
	MsgAuditRefreshSystemServices:    "obnoviť systémové služby",
	MsgAuditCreateNetwork:            "vytvoriť sieť",
	MsgAuditRemoveNetwork:            "odstrániť sieť",
	MsgAuditEnableNetwork:            "povoliť sieť",
	MsgAuditDisableNetwork:           "zakázať sieť",
	MsgAuditAddNetworkPeer:           "pridať partnera siete",
	MsgAuditRemoveNetworkPeer:        "odstrániť partnera siete",
	MsgAuditRefreshNetworkPeer:       "obnoviť partnera siete",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "tento účet môže používať iba koncové body na pripojenie k sieti a objektové úložisko",
	MsgAuthNetworkOnlyNetworkDenied: "tento účet nemá v tejto sieti povolenie",
	MsgAuthWireGuardPeerNotOwned:    "tento účet môže obnovovať iba partnerov, ktorých sám zaregistroval",
	MsgAuthSessionNotOwned:          "tento účet môže odvolať iba vlastné relácie",
	MsgAuthObjectStorageRequired:    "vyžaduje sa prístup správcu alebo objektového úložiska",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "nahrávanie a sťahovanie archívov nemôže smerovať na oddiel objektového úložiska",
	MsgGfehNotConfigured:         "objektové úložisko nie je nakonfigurované",
	MsgGfehNameRequired:          "pole názov je povinné",
	MsgGfehPartitionExists:       "oddiel už existuje",
	MsgGfehPartitionNotFound:     "oddiel sa nenašiel",
	MsgGfehNetworkRequired:       "pole sieť je povinné",
	MsgGfehPrincipalRequired:     "pole principal je povinné",
	MsgGfehPathRequired:          "pole cesta je povinné",
	MsgGfehUnknownAccount:        "taký účet neexistuje",
	MsgAuditCreateGfehPartition:  "vytvoriť oddiel objektového úložiska",
	MsgAuditModifyGfehPartition:  "upraviť oddiel objektového úložiska",
	MsgAuditRemoveGfehPartition:  "odstrániť oddiel objektového úložiska",
	MsgAuditAddGfehPrincipal:     "pridať používateľa objektového úložiska",
	MsgAuditRemoveGfehPrincipal:  "odstrániť používateľa objektového úložiska",
	MsgAuditAddGfehGrant:         "pridať oprávnenie objektového úložiska",
	MsgAuditRevokeGfehGrant:      "odvolať oprávnenie objektového úložiska",
	MsgAuditWithdrawGfehExposure: "stiahnuť odkaz objektového úložiska",
}

package i18n

// csCZMessages contains all Czech translations.
var csCZMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "chybí autorizační token",
	MsgAuthInvalidSession: "neplatná relace",
	MsgAuthAdminRequired:  "je vyžadován přístup správce",

	// Authentication.
	MsgAuthInvalidCredentials: "neplatné přihlašovací údaje",
	MsgAuthNotConfigured:      "ověřování není nakonfigurováno",

	// Account management.
	MsgAccountAdminStatusImmutable: "stav správce nelze po vytvoření účtu změnit",
	MsgAccountListError:            "vypsat účty",
	MsgAccountCheckSessions:        "zkontrolovat aktivní relace správců",
	MsgAccountCreateFailed:         "vytvoření účtu selhalo",

	// Settings.
	MsgSettingNotFound:     "nastavení %q nebylo nalezeno",
	MsgSettingKeyRequired:  "klíč je povinný",
	MsgSettingInvalidBytes: "neplatná bajtová hodnota pro %q: %v",
	MsgSettingsMgrMissing:  "správce nastavení není k dispozici",

	// Audit.
	MsgAuditNotConfigured: "protokolování auditu není nakonfigurováno",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "povolení/zakázání není dovoleno",
	MsgUnitCannotStopController:    "systemcontroller nelze zastavit",
	MsgUnitInvalidLines:            "neplatný parametr lines",
	MsgUnitInvalidSince:            "neplatný parametr since",
	MsgUnitInvalidUntil:            "neplatný parametr until",
	MsgUnitInvalidPriority:         "neplatný parametr priority",

	// Repository management.
	MsgRepoInvalidURL: "neplatná url",

	// Pages management.
	MsgPagesNotConfigured:    "pages nejsou nakonfigurovány",
	MsgPagesGitNotConfigured: "git klient nebo adresář pages nejsou nakonfigurovány",

	// Package installation.
	MsgInstallNoRepoRoot:      "není nakonfigurován žádný kořen repozitáře",
	MsgInstallSummaryUpgrade:  "Aktualizovat %s z %s na %s",
	MsgInstallSummaryInstall:  "Nainstalovat %s %s",
	MsgInstallSummaryImage:    "Obraz: %s",
	MsgInstallSummaryVolumes:  "%d svazků",
	MsgInstallSummaryNewVols:  "%d nových",
	MsgInstallSummaryMigrated: "%d migrováno",
	MsgInstallSummaryNoVols:   "Žádné svazky",
	MsgInstallSummaryPorts:    "Externí porty: %s",
	MsgInstallSummaryConfig:   "Je vyžadována konfigurace",
	MsgInstallSummaryVMImage:  "Obraz VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, název a verze jsou povinné",
	MsgManifestNotFound:       "manifest balíčku nebyl nalezen: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, název a verze jsou povinné",
	MsgRebuildRepoNotConfigured: "kořen repozitáře není nakonfigurován",
	MsgRebuildGitNotConfigured:  "git klient není nakonfigurován",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "pole subvolume je povinné",
	MsgArchiveFileRequired:      "je vyžadován soubor archivu: %v",
	MsgArchiveUnsupportedFormat: "nepodporovaný formát stahování: %s",
	MsgArchiveUnpackSuccess:     "archiv byl úspěšně rozbalen",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "adresář pages není nakonfigurován",
	MsgPagesNameRequired:           "pole název je povinné",
	MsgPagesUploadArchiveOnly:      "nahrávání je povoleno pouze pro stránky typu archiv",
	MsgPagesArchiveRebuildRequired: "stránky typu archiv je nutné znovu sestavit nahráním nového archivu přes /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitorování není nakonfigurováno",

	// Upgrades.
	MsgUpgradeSettingsMissing: "správce nastavení není k dispozici",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "vytvořit souborový systém",
	MsgAuditModifyFilesystem:         "upravit souborový systém",
	MsgAuditRemoveFilesystem:         "odebrat souborový systém",
	MsgAuditAddRepository:            "přidat repozitář",
	MsgAuditRemoveRepository:         "odebrat repozitář",
	MsgAuditMoveRepository:           "přesunout repozitář",
	MsgAuditRefreshRepositories:      "obnovit repozitáře",
	MsgAuditInstallPackage:           "nainstalovat balíček",
	MsgAuditUninstallPackage:         "odinstalovat balíček",
	MsgAuditPurgeUninstalledVolumes:  "vyčistit odinstalované svazky",
	MsgAuditPurgeVolumes:             "vyčistit svazky",
	MsgAuditDisablePackage:           "zakázat balíček",
	MsgAuditEnablePackage:            "povolit balíček",
	MsgAuditSetUnitStatus:            "nastavit stav jednotky",
	MsgAuditCreateAccount:            "vytvořit účet",
	MsgAuditUpdateAccount:            "aktualizovat účet",
	MsgAuditDisableAccount:           "zakázat účet",
	MsgAuditAuthenticate:             "ověřit",
	MsgAuditRevokeSession:            "odvolat relaci",
	MsgAuditUpdateSetting:            "aktualizovat nastavení",
	MsgAuditDismissUpgrades:          "zamítnout aktualizace balíčků",
	MsgAuditUploadArchive:            "nahrát archiv",
	MsgAuditDownloadArchive:          "stáhnout archiv",
	MsgAuditCreatePage:               "vytvořit stránku",
	MsgAuditUpdatePage:               "aktualizovat stránku",
	MsgAuditRemovePage:               "odebrat stránku",
	MsgAuditRebuildPage:              "znovu sestavit stránku",
	MsgAuditUploadPageArchive:        "nahrát archiv stránky",
	MsgAuditEnableAccount:            "povolit účet",
	MsgAuditRebuildGit:               "znovu sestavit git",
	MsgAuditUploadVMImage:            "nahrát obraz vm",
	MsgAuditDeleteVMImage:            "smazat obraz vm",
	MsgAuditAddDNSRecord:             "přidat dns záznam",
	MsgAuditRemoveDNSRecord:          "odebrat dns záznam",
	MsgAuditSetDNSTLD:                "nastavit dns tld",
	MsgAuditSetupDNS:                 "nastavit dns",
	MsgAuditRemovePackageVolume:      "odebrat svazek balíčku",
	MsgAuditRemovePackageVolumeGroup: "odebrat skupinu svazků balíčku",
	MsgAuditClearLastResponses:       "vymazat uložené odpovědi instalace",
	MsgAuditSetSystemServiceStatus:   "nastavit stav systémové služby",
	MsgAuditRefreshSystemServices:    "obnovit systémové služby",
	MsgAuditCreateNetwork:            "vytvořit síť",
	MsgAuditRemoveNetwork:            "odebrat síť",
	MsgAuditEnableNetwork:            "povolit síť",
	MsgAuditDisableNetwork:           "zakázat síť",
	MsgAuditAddNetworkPeer:           "přidat protějšek sítě",
	MsgAuditRemoveNetworkPeer:        "odebrat protějšek sítě",
	MsgAuditRefreshNetworkPeer:       "obnovit protějšek sítě",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "tento účet může používat pouze koncové body pro připojení k síti a objektové úložiště",
	MsgAuthNetworkOnlyNetworkDenied: "tento účet nemá v této síti oprávnění",
	MsgAuthWireGuardPeerNotOwned:    "tento účet může obnovovat pouze protějšky, které sám zaregistroval",
	MsgAuthSessionNotOwned:          "tento účet může odvolat pouze vlastní relace",
	MsgAuthObjectStorageRequired:    "je vyžadován přístup správce nebo objektového úložiště",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "nahrávání a stahování archivů nemůže směřovat na oddíl objektového úložiště",
	MsgGfehNotConfigured:         "objektové úložiště není nakonfigurováno",
	MsgGfehNameRequired:          "pole název je povinné",
	MsgGfehPartitionExists:       "oddíl již existuje",
	MsgGfehPartitionNotFound:     "oddíl nebyl nalezen",
	MsgGfehNetworkRequired:       "pole síť je povinné",
	MsgGfehPrincipalRequired:     "pole principal je povinné",
	MsgGfehPathRequired:          "pole cesta je povinné",
	MsgGfehUnknownAccount:        "takový účet neexistuje",
	MsgAuditCreateGfehPartition:  "vytvořit oddíl objektového úložiště",
	MsgAuditModifyGfehPartition:  "upravit oddíl objektového úložiště",
	MsgAuditRemoveGfehPartition:  "odebrat oddíl objektového úložiště",
	MsgAuditAddGfehPrincipal:     "přidat uživatele objektového úložiště",
	MsgAuditRemoveGfehPrincipal:  "odebrat uživatele objektového úložiště",
	MsgAuditAddGfehGrant:         "přidat oprávnění objektového úložiště",
	MsgAuditRevokeGfehGrant:      "odvolat oprávnění objektového úložiště",
	MsgAuditWithdrawGfehExposure: "stáhnout odkaz objektového úložiště",

	// The ingress retry page.
	MsgIngressUnavailableTitle:  "%s není dostupná",
	MsgIngressUnavailableBody:   "Town OS tuto adresu stále směruje, ale služba za ní neodpovídá. Nejspíš se právě spouští, restartuje po aktualizaci nebo je krátce přetížená.",
	MsgIngressUnavailableRetry:  "Není třeba nic dělat: tato stránka to zkouší znovu každých %d sekund a jakmile služba odpoví, zobrazí ji.",
	MsgIngressUnavailableFooter: "Ingress Town OS",
}

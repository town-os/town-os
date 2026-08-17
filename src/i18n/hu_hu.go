package i18n

// huHUMessages contains all Hungarian translations.
//
// The audit action descriptions are nominalised ("fájlrendszer létrehozása",
// the creation of a filesystem) rather than put in the infinitive. Hungarian
// log entries read as a list of events, and the infinitive would read as a list
// of commands being given.
var huHUMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "hiányzó engedélyezési token",
	MsgAuthInvalidSession: "érvénytelen munkamenet",
	MsgAuthAdminRequired:  "rendszergazdai hozzáférés szükséges",

	// Authentication.
	MsgAuthInvalidCredentials: "érvénytelen hitelesítő adatok",
	MsgAuthNotConfigured:      "a hitelesítés nincs beállítva",

	// Account management.
	MsgAccountAdminStatusImmutable: "a rendszergazdai állapot a fiók létrehozása után nem módosítható",
	MsgAccountListError:            "fiókok listázása",
	MsgAccountCheckSessions:        "aktív rendszergazdai munkamenetek ellenőrzése",
	MsgAccountCreateFailed:         "a fiók létrehozása sikertelen",

	// Settings.
	MsgSettingNotFound:     "a(z) %q beállítás nem található",
	MsgSettingKeyRequired:  "a kulcs megadása kötelező",
	MsgSettingInvalidBytes: "érvénytelen bájtérték ehhez: %q: %v",
	MsgSettingsMgrMissing:  "a beállításkezelő nem érhető el",

	// Audit.
	MsgAuditNotConfigured: "az auditnaplózás nincs beállítva",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "az engedélyezés/letiltás nem megengedett",
	MsgUnitCannotStopController:    "a systemcontroller nem állítható le",
	MsgUnitInvalidLines:            "érvénytelen lines paraméter",
	MsgUnitInvalidSince:            "érvénytelen since paraméter",
	MsgUnitInvalidUntil:            "érvénytelen until paraméter",
	MsgUnitInvalidPriority:         "érvénytelen priority paraméter",

	// Repository management.
	MsgRepoInvalidURL: "érvénytelen url",

	// Pages management.
	MsgPagesNotConfigured:    "a pages nincs beállítva",
	MsgPagesGitNotConfigured: "a git kliens vagy a pages könyvtár nincs beállítva",

	// Package installation.
	MsgInstallNoRepoRoot:      "nincs beállítva adattár-gyökér",
	MsgInstallSummaryUpgrade:  "%s frissítése erről: %s erre: %s",
	MsgInstallSummaryInstall:  "%s %s telepítése",
	MsgInstallSummaryImage:    "Lemezkép: %s",
	MsgInstallSummaryVolumes:  "%d kötet",
	MsgInstallSummaryNewVols:  "%d új",
	MsgInstallSummaryMigrated: "%d áttelepítve",
	MsgInstallSummaryNoVols:   "Nincsenek kötetek",
	MsgInstallSummaryPorts:    "Külső portok: %s",
	MsgInstallSummaryConfig:   "Konfiguráció szükséges",
	MsgInstallSummaryVMImage:  "VM lemezkép: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "a repo, a név és a verzió megadása kötelező",
	MsgManifestNotFound:       "a csomag jegyzékfájlja nem található: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "a repo, a név és a verzió megadása kötelező",
	MsgRebuildRepoNotConfigured: "az adattár-gyökér nincs beállítva",
	MsgRebuildGitNotConfigured:  "a git kliens nincs beállítva",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "a subvolume mező megadása kötelező",
	MsgArchiveFileRequired:      "archívumfájl szükséges: %v",
	MsgArchiveUnsupportedFormat: "nem támogatott letöltési formátum: %s",
	MsgArchiveUnpackSuccess:     "az archívum sikeresen ki lett csomagolva",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "a pages könyvtár nincs beállítva",
	MsgPagesNameRequired:           "a név mező megadása kötelező",
	MsgPagesUploadArchiveOnly:      "a feltöltés csak archívum típusú oldalaknál engedélyezett",
	MsgPagesArchiveRebuildRequired: "az archívum típusú oldalakat új archívum feltöltésével kell újraépíteni a /pages/upload végponton",

	// Monitoring.
	MsgMonitoringNotConfigured: "a megfigyelés nincs beállítva",

	// Upgrades.
	MsgUpgradeSettingsMissing: "a beállításkezelő nem érhető el",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "fájlrendszer létrehozása",
	MsgAuditModifyFilesystem:         "fájlrendszer módosítása",
	MsgAuditRemoveFilesystem:         "fájlrendszer eltávolítása",
	MsgAuditAddRepository:            "adattár hozzáadása",
	MsgAuditRemoveRepository:         "adattár eltávolítása",
	MsgAuditMoveRepository:           "adattár áthelyezése",
	MsgAuditRefreshRepositories:      "adattárak frissítése",
	MsgAuditInstallPackage:           "csomag telepítése",
	MsgAuditUninstallPackage:         "csomag eltávolítása",
	MsgAuditPurgeUninstalledVolumes:  "eltávolított kötetek törlése",
	MsgAuditPurgeVolumes:             "kötetek törlése",
	MsgAuditDisablePackage:           "csomag letiltása",
	MsgAuditEnablePackage:            "csomag engedélyezése",
	MsgAuditSetUnitStatus:            "egység állapotának beállítása",
	MsgAuditCreateAccount:            "fiók létrehozása",
	MsgAuditUpdateAccount:            "fiók frissítése",
	MsgAuditDisableAccount:           "fiók letiltása",
	MsgAuditAuthenticate:             "hitelesítés",
	MsgAuditRevokeSession:            "munkamenet visszavonása",
	MsgAuditUpdateSetting:            "beállítás frissítése",
	MsgAuditDismissUpgrades:          "csomagfrissítések elvetése",
	MsgAuditUploadArchive:            "archívum feltöltése",
	MsgAuditDownloadArchive:          "archívum letöltése",
	MsgAuditCreatePage:               "oldal létrehozása",
	MsgAuditUpdatePage:               "oldal frissítése",
	MsgAuditRemovePage:               "oldal eltávolítása",
	MsgAuditRebuildPage:              "oldal újraépítése",
	MsgAuditUploadPageArchive:        "oldalarchívum feltöltése",
	MsgAuditEnableAccount:            "fiók engedélyezése",
	MsgAuditRebuildGit:               "git újraépítése",
	MsgAuditUploadVMImage:            "vm lemezkép feltöltése",
	MsgAuditDeleteVMImage:            "vm lemezkép törlése",
	MsgAuditAddDNSRecord:             "dns rekord hozzáadása",
	MsgAuditRemoveDNSRecord:          "dns rekord eltávolítása",
	MsgAuditSetDNSTLD:                "dns tld beállítása",
	MsgAuditSetupDNS:                 "dns beállítása",
	MsgAuditRemovePackageVolume:      "csomagkötet eltávolítása",
	MsgAuditRemovePackageVolumeGroup: "csomagkötet-csoport eltávolítása",
	MsgAuditClearLastResponses:       "gyorsítótárazott telepítési válaszok törlése",
	MsgAuditSetSystemServiceStatus:   "rendszerszolgáltatás állapotának beállítása",
	MsgAuditRefreshSystemServices:    "rendszerszolgáltatások frissítése",
	MsgAuditCreateNetwork:            "hálózat létrehozása",
	MsgAuditRemoveNetwork:            "hálózat eltávolítása",
	MsgAuditEnableNetwork:            "hálózat engedélyezése",
	MsgAuditDisableNetwork:           "hálózat letiltása",
	MsgAuditAddNetworkPeer:           "hálózati partner hozzáadása",
	MsgAuditRemoveNetworkPeer:        "hálózati partner eltávolítása",
	MsgAuditRefreshNetworkPeer:       "hálózati partner frissítése",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "ez a fiók csak a hálózati regisztrációs és objektumtároló végpontokat használhatja",
	MsgAuthNetworkOnlyNetworkDenied: "ez a fiók nem jogosult ezen a hálózaton",
	MsgAuthWireGuardPeerNotOwned:    "ez a fiók csak az általa regisztrált partnereket frissítheti",
	MsgAuthSessionNotOwned:          "ez a fiók csak a saját munkameneteit vonhatja vissza",
	MsgAuthObjectStorageRequired:    "rendszergazdai vagy objektumtároló-hozzáférés szükséges",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "az archívumok feltöltése és letöltése nem címezhet objektumtároló-partíciót",
	MsgGfehNotConfigured:         "az objektumtároló nincs beállítva",
	MsgGfehNameRequired:          "a név mező megadása kötelező",
	MsgGfehPartitionExists:       "a partíció már létezik",
	MsgGfehPartitionNotFound:     "a partíció nem található",
	MsgGfehNetworkRequired:       "a hálózat mező megadása kötelező",
	MsgGfehPrincipalRequired:     "a principal mező megadása kötelező",
	MsgGfehPathRequired:          "az útvonal mező megadása kötelező",
	MsgGfehUnknownAccount:        "nincs ilyen fiók",
	MsgAuditCreateGfehPartition:  "objektumtároló-partíció létrehozása",
	MsgAuditModifyGfehPartition:  "objektumtároló-partíció módosítása",
	MsgAuditRemoveGfehPartition:  "objektumtároló-partíció eltávolítása",
	MsgAuditAddGfehPrincipal:     "objektumtároló-felhasználó hozzáadása",
	MsgAuditRemoveGfehPrincipal:  "objektumtároló-felhasználó eltávolítása",
	MsgAuditAddGfehGrant:         "objektumtároló-jogosultság hozzáadása",
	MsgAuditRevokeGfehGrant:      "objektumtároló-jogosultság visszavonása",
	MsgAuditWithdrawGfehExposure: "objektumtároló-hivatkozás visszavonása",

	// The ingress retry page.
	MsgIngressUnavailableTitle:  "A(z) %s nem érhető el",
	MsgIngressUnavailableBody:   "A Town OS továbbra is útválasztja ezt a címet, de a mögötte lévő szolgáltatás nem válaszol. Valószínűleg éppen indul, frissítés után újraindul, vagy rövid ideig túlterhelt.",
	MsgIngressUnavailableRetry:  "Nincs teendő: ez az oldal %d másodpercenként újrapróbálkozik, és megjeleníti a szolgáltatást, amint az újra válaszol.",
	MsgIngressUnavailableFooter: "Town OS ingress",
}

package i18n

// roROMessages contains all Romanian translations.
//
// Diacritics use the comma-below forms (ș, ț) rather than the cedilla ones
// (ş, ţ). The cedilla letters belong to Turkish and only ever stood in for
// Romanian because early code pages had no comma-below glyph; a modern font
// renders them visibly wrong.
var roROMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "lipsește tokenul de autorizare",
	MsgAuthInvalidSession: "sesiune invalidă",
	MsgAuthAdminRequired:  "este necesar acces de administrator",

	// Authentication.
	MsgAuthInvalidCredentials: "credențiale invalide",

	// Account management.
	MsgAccountAdminStatusImmutable: "statutul de administrator nu poate fi modificat după crearea contului",
	MsgAccountListError:            "listează conturile",
	MsgAccountCheckSessions:        "verifică sesiunile active de administrator",
	MsgAccountCreateFailed:         "crearea contului a eșuat",

	// Settings.
	MsgSettingNotFound:     "setarea %q nu a fost găsită",
	MsgSettingKeyRequired:  "cheia este obligatorie",
	MsgSettingInvalidBytes: "valoare în octeți invalidă pentru %q: %v",
	MsgSettingsMgrMissing:  "managerul de setări nu este disponibil",

	// Audit.
	MsgAuditNotConfigured: "jurnalizarea de audit nu este configurată",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "activarea/dezactivarea nu este permisă",
	MsgUnitCannotStopController:    "systemcontroller nu poate fi oprit",
	MsgUnitInvalidLines:            "parametru lines invalid",
	MsgUnitInvalidSince:            "parametru since invalid",
	MsgUnitInvalidUntil:            "parametru until invalid",
	MsgUnitInvalidPriority:         "parametru priority invalid",

	// Repository management.
	MsgRepoInvalidURL: "url invalid",

	// Pages management.
	MsgPagesNotConfigured:    "pages nu este configurat",
	MsgPagesGitNotConfigured: "clientul git sau directorul pages nu sunt configurate",

	// Package installation.
	MsgInstallNoRepoRoot:      "nu este configurată nicio rădăcină de depozit",
	MsgInstallSummaryUpgrade:  "Actualizează %s de la %s la %s",
	MsgInstallSummaryInstall:  "Instalează %s %s",
	MsgInstallSummaryImage:    "Imagine: %s",
	MsgInstallSummaryVolumes:  "%d volum(e)",
	MsgInstallSummaryNewVols:  "%d noi",
	MsgInstallSummaryMigrated: "%d migrate",
	MsgInstallSummaryNoVols:   "Niciun volum",
	MsgInstallSummaryPorts:    "Porturi externe: %s",
	MsgInstallSummaryConfig:   "Este necesară configurarea",
	MsgInstallSummaryVMImage:  "Imagine VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, numele și versiunea sunt obligatorii",
	MsgManifestNotFound:       "manifestul pachetului nu a fost găsit: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, numele și versiunea sunt obligatorii",
	MsgRebuildRepoNotConfigured: "rădăcina depozitului nu este configurată",
	MsgRebuildGitNotConfigured:  "clientul git nu este configurat",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "câmpul subvolume este obligatoriu",
	MsgArchiveFileRequired:      "este necesar un fișier de arhivă: %v",
	MsgArchiveUnsupportedFormat: "format de descărcare neacceptat: %s",
	MsgArchiveUnpackSuccess:     "arhiva a fost dezarhivată cu succes",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "directorul pages nu este configurat",
	MsgPagesNameRequired:           "câmpul nume este obligatoriu",
	MsgPagesUploadArchiveOnly:      "încărcarea este permisă doar pentru paginile de tip arhivă",
	MsgPagesArchiveRebuildRequired: "paginile de tip arhivă trebuie reconstruite prin încărcarea unei arhive noi via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitorizarea nu este configurată",

	// Upgrades.
	MsgUpgradeSettingsMissing: "managerul de setări nu este disponibil",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "creează sistem de fișiere",
	MsgAuditModifyFilesystem:         "modifică sistem de fișiere",
	MsgAuditRemoveFilesystem:         "elimină sistem de fișiere",
	MsgAuditAddRepository:            "adaugă depozit",
	MsgAuditRemoveRepository:         "elimină depozit",
	MsgAuditMoveRepository:           "mută depozit",
	MsgAuditRefreshRepositories:      "reîmprospătează depozitele",
	MsgAuditInstallPackage:           "instalează pachet",
	MsgAuditUninstallPackage:         "dezinstalează pachet",
	MsgAuditPurgeUninstalledVolumes:  "curăță volumele dezinstalate",
	MsgAuditPurgeVolumes:             "curăță volumele",
	MsgAuditDisablePackage:           "dezactivează pachet",
	MsgAuditEnablePackage:            "activează pachet",
	MsgAuditSetUnitStatus:            "setează starea unității",
	MsgAuditCreateAccount:            "creează cont",
	MsgAuditUpdateAccount:            "actualizează cont",
	MsgAuditDisableAccount:           "dezactivează cont",
	MsgAuditAuthenticate:             "autentifică",
	MsgAuditRevokeSession:            "revocă sesiune",
	MsgAuditUpdateSetting:            "actualizează setare",
	MsgAuditDismissUpgrades:          "respinge actualizările de pachete",
	MsgAuditUploadArchive:            "încarcă arhivă",
	MsgAuditDownloadArchive:          "descarcă arhivă",
	MsgAuditCreatePage:               "creează pagină",
	MsgAuditUpdatePage:               "actualizează pagină",
	MsgAuditRemovePage:               "elimină pagină",
	MsgAuditRebuildPage:              "reconstruiește pagină",
	MsgAuditUploadPageArchive:        "încarcă arhiva paginii",
	MsgAuditEnableAccount:            "activează cont",
	MsgAuditRebuildGit:               "reconstruiește git",
	MsgAuditUploadVMImage:            "încarcă imagine vm",
	MsgAuditDeleteVMImage:            "șterge imagine vm",
	MsgAuditAddDNSRecord:             "adaugă înregistrare dns",
	MsgAuditRemoveDNSRecord:          "elimină înregistrare dns",
	MsgAuditSetDNSTLD:                "setează tld dns",
	MsgAuditSetupDNS:                 "configurează dns",
	MsgAuditRemovePackageVolume:      "elimină volum de pachet",
	MsgAuditRemovePackageVolumeGroup: "elimină grup de volume de pachet",
	MsgAuditClearLastResponses:       "șterge răspunsurile de instalare din cache",
	MsgAuditSetSystemServiceStatus:   "setează starea serviciului de sistem",
	MsgAuditRefreshSystemServices:    "reîmprospătează serviciile de sistem",
	MsgAuditCreateNetwork:            "creează rețea",
	MsgAuditRemoveNetwork:            "elimină rețea",
	MsgAuditEnableNetwork:            "activează rețea",
	MsgAuditDisableNetwork:           "dezactivează rețea",
	MsgAuditAddNetworkPeer:           "adaugă partener de rețea",
	MsgAuditRemoveNetworkPeer:        "elimină partener de rețea",
	MsgAuditRefreshNetworkPeer:       "reîmprospătează partener de rețea",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "acest cont poate folosi doar punctele finale pentru înscrierea în rețea și stocarea de obiecte",
	MsgAuthNetworkOnlyNetworkDenied: "acest cont nu are permisiune în acea rețea",
	MsgAuthWireGuardPeerNotOwned:    "acest cont poate reîmprospăta doar partenerii pe care i-a înscris",
	MsgAuthSessionNotOwned:          "acest cont poate revoca doar propriile sesiuni",
	MsgAuthObjectStorageRequired:    "este necesar acces de administrator sau la stocarea de obiecte",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "încărcările și descărcările de arhive nu pot viza o partiție de stocare de obiecte",
	MsgGfehNotConfigured:         "stocarea de obiecte nu este configurată",
	MsgGfehNameRequired:          "câmpul nume este obligatoriu",
	MsgGfehPartitionExists:       "partiția există deja",
	MsgGfehPartitionNotFound:     "partiția nu a fost găsită",
	MsgGfehNetworkRequired:       "câmpul rețea este obligatoriu",
	MsgGfehPrincipalRequired:     "câmpul principal este obligatoriu",
	MsgGfehPathRequired:          "câmpul cale este obligatoriu",
	MsgGfehUnknownAccount:        "nu există un astfel de cont",
	MsgAuditCreateGfehPartition:  "creează partiție de stocare de obiecte",
	MsgAuditModifyGfehPartition:  "modifică partiție de stocare de obiecte",
	MsgAuditRemoveGfehPartition:  "elimină partiție de stocare de obiecte",
	MsgAuditAddGfehPrincipal:     "adaugă utilizator de stocare de obiecte",
	MsgAuditRemoveGfehPrincipal:  "elimină utilizator de stocare de obiecte",
	MsgAuditAddGfehGrant:         "adaugă permisiune de stocare de obiecte",
	MsgAuditRevokeGfehGrant:      "revocă permisiune de stocare de obiecte",
	MsgAuditWithdrawGfehExposure: "retrage linkul de stocare de obiecte",
}

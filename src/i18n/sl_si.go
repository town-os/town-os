package i18n

// slSIMessages contains all Slovenian translations.
var slSIMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "manjka avtorizacijski žeton",
	MsgAuthInvalidSession: "neveljavna seja",
	MsgAuthAdminRequired:  "zahtevan je skrbniški dostop",

	// Authentication.
	MsgAuthInvalidCredentials: "neveljavne poverilnice",
	MsgAuthNotConfigured:      "overjanje pristnosti ni nastavljeno",

	// Account management.
	MsgAccountAdminStatusImmutable: "skrbniškega stanja po ustvarjanju računa ni mogoče spremeniti",
	MsgAccountListError:            "izpiši račune",
	MsgAccountCheckSessions:        "preveri aktivne skrbniške seje",
	MsgAccountCreateFailed:         "ustvarjanje računa ni uspelo",

	// Settings.
	MsgSettingNotFound:     "nastavitve %q ni mogoče najti",
	MsgSettingKeyRequired:  "ključ je obvezen",
	MsgSettingInvalidBytes: "neveljavna bajtna vrednost za %q: %v",
	MsgSettingsMgrMissing:  "upravitelj nastavitev ni na voljo",

	// Audit.
	MsgAuditNotConfigured: "beleženje revizije ni nastavljeno",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "omogočanje/onemogočanje ni dovoljeno",
	MsgUnitCannotStopController:    "systemcontrollerja ni mogoče ustaviti",
	MsgUnitInvalidLines:            "neveljaven parameter lines",
	MsgUnitInvalidSince:            "neveljaven parameter since",
	MsgUnitInvalidUntil:            "neveljaven parameter until",
	MsgUnitInvalidPriority:         "neveljaven parameter priority",

	// Repository management.
	MsgRepoInvalidURL: "neveljaven url",

	// Pages management.
	MsgPagesNotConfigured:    "pages ni nastavljen",
	MsgPagesGitNotConfigured: "odjemalec git ali imenik pages nista nastavljena",

	// Package installation.
	MsgInstallNoRepoRoot:      "nastavljena ni nobena korenska mapa repozitorija",
	MsgInstallSummaryUpgrade:  "Nadgradi %s z %s na %s",
	MsgInstallSummaryInstall:  "Namesti %s %s",
	MsgInstallSummaryImage:    "Slika: %s",
	MsgInstallSummaryVolumes:  "%d nosilcev",
	MsgInstallSummaryNewVols:  "%d novih",
	MsgInstallSummaryMigrated: "%d preseljenih",
	MsgInstallSummaryNoVols:   "Ni nosilcev",
	MsgInstallSummaryPorts:    "Zunanja vrata: %s",
	MsgInstallSummaryConfig:   "Zahtevana je konfiguracija",
	MsgInstallSummaryVMImage:  "Slika VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, ime in različica so obvezni",
	MsgManifestNotFound:       "manifesta paketa ni mogoče najti: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, ime in različica so obvezni",
	MsgRebuildRepoNotConfigured: "korenska mapa repozitorija ni nastavljena",
	MsgRebuildGitNotConfigured:  "odjemalec git ni nastavljen",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "polje subvolume je obvezno",
	MsgArchiveFileRequired:      "zahtevana je arhivska datoteka: %v",
	MsgArchiveUnsupportedFormat: "nepodprta oblika prenosa: %s",
	MsgArchiveUnpackSuccess:     "arhiv je bil uspešno razpakiran",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "imenik pages ni nastavljen",
	MsgPagesNameRequired:           "polje ime je obvezno",
	MsgPagesUploadArchiveOnly:      "nalaganje je dovoljeno samo za strani vrste arhiv",
	MsgPagesArchiveRebuildRequired: "strani vrste arhiv je treba znova zgraditi z nalaganjem novega arhiva prek /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "nadzor ni nastavljen",

	// Upgrades.
	MsgUpgradeSettingsMissing: "upravitelj nastavitev ni na voljo",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "ustvari datotečni sistem",
	MsgAuditModifyFilesystem:         "spremeni datotečni sistem",
	MsgAuditRemoveFilesystem:         "odstrani datotečni sistem",
	MsgAuditAddRepository:            "dodaj repozitorij",
	MsgAuditRemoveRepository:         "odstrani repozitorij",
	MsgAuditMoveRepository:           "premakni repozitorij",
	MsgAuditRefreshRepositories:      "osveži repozitorije",
	MsgAuditInstallPackage:           "namesti paket",
	MsgAuditUninstallPackage:         "odstrani paket",
	MsgAuditPurgeUninstalledVolumes:  "počisti odstranjene nosilce",
	MsgAuditPurgeVolumes:             "počisti nosilce",
	MsgAuditDisablePackage:           "onemogoči paket",
	MsgAuditEnablePackage:            "omogoči paket",
	MsgAuditSetUnitStatus:            "nastavi stanje enote",
	MsgAuditCreateAccount:            "ustvari račun",
	MsgAuditUpdateAccount:            "posodobi račun",
	MsgAuditDisableAccount:           "onemogoči račun",
	MsgAuditAuthenticate:             "overi",
	MsgAuditRevokeSession:            "prekliči sejo",
	MsgAuditUpdateSetting:            "posodobi nastavitev",
	MsgAuditDismissUpgrades:          "zavrni nadgradnje paketov",
	MsgAuditUploadArchive:            "naloži arhiv",
	MsgAuditDownloadArchive:          "prenesi arhiv",
	MsgAuditCreatePage:               "ustvari stran",
	MsgAuditUpdatePage:               "posodobi stran",
	MsgAuditRemovePage:               "odstrani stran",
	MsgAuditRebuildPage:              "znova zgradi stran",
	MsgAuditUploadPageArchive:        "naloži arhiv strani",
	MsgAuditEnableAccount:            "omogoči račun",
	MsgAuditRebuildGit:               "znova zgradi git",
	MsgAuditUploadVMImage:            "naloži sliko vm",
	MsgAuditDeleteVMImage:            "izbriši sliko vm",
	MsgAuditAddDNSRecord:             "dodaj zapis dns",
	MsgAuditRemoveDNSRecord:          "odstrani zapis dns",
	MsgAuditSetDNSTLD:                "nastavi dns tld",
	MsgAuditSetupDNS:                 "nastavi dns",
	MsgAuditRemovePackageVolume:      "odstrani nosilec paketa",
	MsgAuditRemovePackageVolumeGroup: "odstrani skupino nosilcev paketa",
	MsgAuditClearLastResponses:       "počisti predpomnjene odgovore namestitve",
	MsgAuditSetSystemServiceStatus:   "nastavi stanje sistemske storitve",
	MsgAuditRefreshSystemServices:    "osveži sistemske storitve",
	MsgAuditCreateNetwork:            "ustvari omrežje",
	MsgAuditRemoveNetwork:            "odstrani omrežje",
	MsgAuditEnableNetwork:            "omogoči omrežje",
	MsgAuditDisableNetwork:           "onemogoči omrežje",
	MsgAuditAddNetworkPeer:           "dodaj soležnika omrežja",
	MsgAuditRemoveNetworkPeer:        "odstrani soležnika omrežja",
	MsgAuditRefreshNetworkPeer:       "osveži soležnika omrežja",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "ta račun lahko uporablja samo končne točke za vpis v omrežje in objektno shrambo",
	MsgAuthNetworkOnlyNetworkDenied: "ta račun v tem omrežju ni dovoljen",
	MsgAuthWireGuardPeerNotOwned:    "ta račun lahko osveži samo soležnike, ki jih je sam vpisal",
	MsgAuthSessionNotOwned:          "ta račun lahko prekliče samo lastne seje",
	MsgAuthObjectStorageRequired:    "zahtevan je skrbniški dostop ali dostop do objektne shrambe",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "nalaganja in prenosi arhivov ne morejo naslavljati particije objektne shrambe",
	MsgGfehNotConfigured:         "objektna shramba ni nastavljena",
	MsgGfehNameRequired:          "polje ime je obvezno",
	MsgGfehPartitionExists:       "particija že obstaja",
	MsgGfehPartitionNotFound:     "particije ni mogoče najti",
	MsgGfehNetworkRequired:       "polje omrežje je obvezno",
	MsgGfehPrincipalRequired:     "polje principal je obvezno",
	MsgGfehPathRequired:          "polje pot je obvezno",
	MsgGfehUnknownAccount:        "tak račun ne obstaja",
	MsgAuditCreateGfehPartition:  "ustvari particijo objektne shrambe",
	MsgAuditModifyGfehPartition:  "spremeni particijo objektne shrambe",
	MsgAuditRemoveGfehPartition:  "odstrani particijo objektne shrambe",
	MsgAuditAddGfehPrincipal:     "dodaj uporabnika objektne shrambe",
	MsgAuditRemoveGfehPrincipal:  "odstrani uporabnika objektne shrambe",
	MsgAuditAddGfehGrant:         "dodaj dovoljenje objektne shrambe",
	MsgAuditRevokeGfehGrant:      "prekliči dovoljenje objektne shrambe",
	MsgAuditWithdrawGfehExposure: "umakni povezavo objektne shrambe",

	// The ingress retry page.
	MsgIngressUnavailableTitle:  "%s ni na voljo",
	MsgIngressUnavailableBody:   "Town OS ta naslov še vedno usmerja, vendar storitev za njim ne odgovarja. Najverjetneje se prav zdaj zaganja, ponovno zaganja po posodobitvi ali je za kratek čas preobremenjena.",
	MsgIngressUnavailableRetry:  "Ni treba storiti ničesar: ta stran poskuša znova vsakih %d sekund in bo storitev prikazala takoj, ko spet odgovori.",
	MsgIngressUnavailableFooter: "Ingress Town OS",
}

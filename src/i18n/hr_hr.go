package i18n

// hrHRMessages contains all Croatian translations.
var hrHRMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "nedostaje autorizacijski token",
	MsgAuthInvalidSession: "nevažeća sesija",
	MsgAuthAdminRequired:  "potreban je administratorski pristup",

	// Authentication.
	MsgAuthInvalidCredentials: "nevažeće vjerodajnice",

	// Account management.
	MsgAccountAdminStatusImmutable: "administratorski status ne može se promijeniti nakon stvaranja računa",
	MsgAccountListError:            "ispiši račune",
	MsgAccountCheckSessions:        "provjeri aktivne administratorske sesije",
	MsgAccountCreateFailed:         "stvaranje računa nije uspjelo",

	// Settings.
	MsgSettingNotFound:     "postavka %q nije pronađena",
	MsgSettingKeyRequired:  "ključ je obavezan",
	MsgSettingInvalidBytes: "nevažeća vrijednost bajtova za %q: %v",
	MsgSettingsMgrMissing:  "upravitelj postavki nije dostupan",

	// Audit.
	MsgAuditNotConfigured: "bilježenje revizije nije konfigurirano",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "omogućavanje/onemogućavanje nije dopušteno",
	MsgUnitCannotStopController:    "systemcontroller se ne može zaustaviti",
	MsgUnitInvalidLines:            "nevažeći parametar lines",
	MsgUnitInvalidSince:            "nevažeći parametar since",
	MsgUnitInvalidUntil:            "nevažeći parametar until",
	MsgUnitInvalidPriority:         "nevažeći parametar priority",

	// Repository management.
	MsgRepoInvalidURL: "nevažeći url",

	// Pages management.
	MsgPagesNotConfigured:    "pages nije konfiguriran",
	MsgPagesGitNotConfigured: "git klijent ili direktorij pages nisu konfigurirani",

	// Package installation.
	MsgInstallNoRepoRoot:      "nije konfiguriran nijedan korijen repozitorija",
	MsgInstallSummaryUpgrade:  "Nadogradi %s s %s na %s",
	MsgInstallSummaryInstall:  "Instaliraj %s %s",
	MsgInstallSummaryImage:    "Slika: %s",
	MsgInstallSummaryVolumes:  "%d volumena",
	MsgInstallSummaryNewVols:  "%d novih",
	MsgInstallSummaryMigrated: "%d migrirano",
	MsgInstallSummaryNoVols:   "Nema volumena",
	MsgInstallSummaryPorts:    "Vanjski portovi: %s",
	MsgInstallSummaryConfig:   "Potrebna je konfiguracija",
	MsgInstallSummaryVMImage:  "Slika VM-a: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, naziv i verzija su obavezni",
	MsgManifestNotFound:       "manifest paketa nije pronađen: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, naziv i verzija su obavezni",
	MsgRebuildRepoNotConfigured: "korijen repozitorija nije konfiguriran",
	MsgRebuildGitNotConfigured:  "git klijent nije konfiguriran",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "polje subvolume je obavezno",
	MsgArchiveFileRequired:      "potrebna je arhivska datoteka: %v",
	MsgArchiveUnsupportedFormat: "nepodržan format preuzimanja: %s",
	MsgArchiveUnpackSuccess:     "arhiva je uspješno raspakirana",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "direktorij pages nije konfiguriran",
	MsgPagesNameRequired:           "polje naziv je obavezno",
	MsgPagesUploadArchiveOnly:      "prijenos je dopušten samo za stranice vrste arhiva",
	MsgPagesArchiveRebuildRequired: "stranice vrste arhiva moraju se ponovno izgraditi prijenosom nove arhive putem /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "nadzor nije konfiguriran",

	// Upgrades.
	MsgUpgradeSettingsMissing: "upravitelj postavki nije dostupan",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "stvori datotečni sustav",
	MsgAuditModifyFilesystem:         "izmijeni datotečni sustav",
	MsgAuditRemoveFilesystem:         "ukloni datotečni sustav",
	MsgAuditAddRepository:            "dodaj repozitorij",
	MsgAuditRemoveRepository:         "ukloni repozitorij",
	MsgAuditMoveRepository:           "premjesti repozitorij",
	MsgAuditRefreshRepositories:      "osvježi repozitorije",
	MsgAuditInstallPackage:           "instaliraj paket",
	MsgAuditUninstallPackage:         "deinstaliraj paket",
	MsgAuditPurgeUninstalledVolumes:  "očisti deinstalirane volumene",
	MsgAuditPurgeVolumes:             "očisti volumene",
	MsgAuditDisablePackage:           "onemogući paket",
	MsgAuditEnablePackage:            "omogući paket",
	MsgAuditSetUnitStatus:            "postavi status jedinice",
	MsgAuditCreateAccount:            "stvori račun",
	MsgAuditUpdateAccount:            "ažuriraj račun",
	MsgAuditDisableAccount:           "onemogući račun",
	MsgAuditAuthenticate:             "autentificiraj",
	MsgAuditRevokeSession:            "opozovi sesiju",
	MsgAuditUpdateSetting:            "ažuriraj postavku",
	MsgAuditDismissUpgrades:          "odbaci nadogradnje paketa",
	MsgAuditUploadArchive:            "prenesi arhivu",
	MsgAuditDownloadArchive:          "preuzmi arhivu",
	MsgAuditCreatePage:               "stvori stranicu",
	MsgAuditUpdatePage:               "ažuriraj stranicu",
	MsgAuditRemovePage:               "ukloni stranicu",
	MsgAuditRebuildPage:              "ponovno izgradi stranicu",
	MsgAuditUploadPageArchive:        "prenesi arhivu stranice",
	MsgAuditEnableAccount:            "omogući račun",
	MsgAuditRebuildGit:               "ponovno izgradi git",
	MsgAuditUploadVMImage:            "prenesi sliku vm-a",
	MsgAuditDeleteVMImage:            "izbriši sliku vm-a",
	MsgAuditAddDNSRecord:             "dodaj dns zapis",
	MsgAuditRemoveDNSRecord:          "ukloni dns zapis",
	MsgAuditSetDNSTLD:                "postavi dns tld",
	MsgAuditSetupDNS:                 "postavi dns",
	MsgAuditRemovePackageVolume:      "ukloni volumen paketa",
	MsgAuditRemovePackageVolumeGroup: "ukloni grupu volumena paketa",
	MsgAuditClearLastResponses:       "očisti predmemorirane odgovore instalacije",
	MsgAuditSetSystemServiceStatus:   "postavi status sistemske usluge",
	MsgAuditRefreshSystemServices:    "osvježi sistemske usluge",
	MsgAuditCreateNetwork:            "stvori mrežu",
	MsgAuditRemoveNetwork:            "ukloni mrežu",
	MsgAuditEnableNetwork:            "omogući mrežu",
	MsgAuditDisableNetwork:           "onemogući mrežu",
	MsgAuditAddNetworkPeer:           "dodaj mrežnog partnera",
	MsgAuditRemoveNetworkPeer:        "ukloni mrežnog partnera",
	MsgAuditRefreshNetworkPeer:       "osvježi mrežnog partnera",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "ovaj račun može koristiti samo krajnje točke za upis u mrežu i objektnu pohranu",
	MsgAuthNetworkOnlyNetworkDenied: "ovaj račun nema dopuštenje na toj mreži",
	MsgAuthWireGuardPeerNotOwned:    "ovaj račun može osvježiti samo partnere koje je sam upisao",
	MsgAuthSessionNotOwned:          "ovaj račun može opozvati samo vlastite sesije",
	MsgAuthObjectStorageRequired:    "potreban je administratorski pristup ili pristup objektnoj pohrani",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "prijenosi i preuzimanja arhiva ne mogu ciljati particiju objektne pohrane",
	MsgGfehNotConfigured:         "objektna pohrana nije konfigurirana",
	MsgGfehNameRequired:          "polje naziv je obavezno",
	MsgGfehPartitionExists:       "particija već postoji",
	MsgGfehPartitionNotFound:     "particija nije pronađena",
	MsgGfehNetworkRequired:       "polje mreža je obavezno",
	MsgGfehPrincipalRequired:     "polje principal je obavezno",
	MsgGfehPathRequired:          "polje putanja je obavezno",
	MsgGfehUnknownAccount:        "takav račun ne postoji",
	MsgAuditCreateGfehPartition:  "stvori particiju objektne pohrane",
	MsgAuditModifyGfehPartition:  "izmijeni particiju objektne pohrane",
	MsgAuditRemoveGfehPartition:  "ukloni particiju objektne pohrane",
	MsgAuditAddGfehPrincipal:     "dodaj korisnika objektne pohrane",
	MsgAuditRemoveGfehPrincipal:  "ukloni korisnika objektne pohrane",
	MsgAuditAddGfehGrant:         "dodaj dopuštenje objektne pohrane",
	MsgAuditRevokeGfehGrant:      "opozovi dopuštenje objektne pohrane",
	MsgAuditWithdrawGfehExposure: "povuci poveznicu objektne pohrane",
}

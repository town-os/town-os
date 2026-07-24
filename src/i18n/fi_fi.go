package i18n

// fiFIMessages contains all Finnish translations.
var fiFIMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "valtuutustunniste puuttuu",
	MsgAuthInvalidSession: "virheellinen istunto",
	MsgAuthAdminRequired:  "ylläpitäjän käyttöoikeus vaaditaan",

	// Authentication.
	MsgAuthInvalidCredentials: "virheelliset tunnistetiedot",

	// Account management.
	MsgAccountAdminStatusImmutable: "ylläpitäjän asemaa ei voi muuttaa tilin luonnin jälkeen",
	MsgAccountListError:            "listaa tilit",
	MsgAccountCheckSessions:        "tarkista aktiiviset ylläpitäjän istunnot",
	MsgAccountCreateFailed:         "tilin luonti epäonnistui",

	// Settings.
	MsgSettingNotFound:     "asetusta %q ei löytynyt",
	MsgSettingKeyRequired:  "avain vaaditaan",
	MsgSettingInvalidBytes: "virheellinen tavuarvo asetukselle %q: %v",
	MsgSettingsMgrMissing:  "asetusten hallinta ei ole käytettävissä",

	// Audit.
	MsgAuditNotConfigured: "valvontalokitusta ei ole määritetty",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "käyttöönotto/käytöstäpoisto ei ole sallittu",
	MsgUnitCannotStopController:    "systemcontrolleria ei voi pysäyttää",
	MsgUnitInvalidLines:            "virheellinen lines-parametri",
	MsgUnitInvalidSince:            "virheellinen since-parametri",
	MsgUnitInvalidUntil:            "virheellinen until-parametri",
	MsgUnitInvalidPriority:         "virheellinen priority-parametri",

	// Repository management.
	MsgRepoInvalidURL: "virheellinen url",

	// Pages management.
	MsgPagesNotConfigured:    "sivuja ei ole määritetty",
	MsgPagesGitNotConfigured: "git-asiakasohjelmaa tai sivuhakemistoa ei ole määritetty",

	// Package installation.
	MsgInstallNoRepoRoot:      "repositorion juurta ei ole määritetty",
	MsgInstallSummaryUpgrade:  "Päivitä %s versiosta %s versioon %s",
	MsgInstallSummaryInstall:  "Asenna %s %s",
	MsgInstallSummaryImage:    "Vedos: %s",
	MsgInstallSummaryVolumes:  "%d volyymi(ä)",
	MsgInstallSummaryNewVols:  "%d uutta",
	MsgInstallSummaryMigrated: "%d siirretty",
	MsgInstallSummaryNoVols:   "Ei volyymejä",
	MsgInstallSummaryPorts:    "Ulkoiset portit: %s",
	MsgInstallSummaryConfig:   "Määritykset vaaditaan",
	MsgInstallSummaryVMImage:  "VM-vedos: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repositorio, nimi ja versio vaaditaan",
	MsgManifestNotFound:       "paketin manifestia ei löytynyt: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repositorio, nimi ja versio vaaditaan",
	MsgRebuildRepoNotConfigured: "repositorion juurta ei ole määritetty",
	MsgRebuildGitNotConfigured:  "git-asiakasohjelmaa ei ole määritetty",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume-kenttä vaaditaan",
	MsgArchiveFileRequired:      "arkistotiedosto vaaditaan: %v",
	MsgArchiveUnsupportedFormat: "ei-tuettu latausmuoto: %s",
	MsgArchiveUnpackSuccess:     "arkiston purku onnistui",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "sivuhakemistoa ei ole määritetty",
	MsgPagesNameRequired:           "nimi-kenttä vaaditaan",
	MsgPagesUploadArchiveOnly:      "lataus on sallittu vain arkistotyyppisille sivuille",
	MsgPagesArchiveRebuildRequired: "arkistosivut on rakennettava uudelleen lataamalla uusi arkisto /pages/upload-osoitteen kautta",

	// Monitoring.
	MsgMonitoringNotConfigured: "valvontaa ei ole määritetty",

	// Upgrades.
	MsgUpgradeSettingsMissing: "asetusten hallinta ei ole käytettävissä",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "luo tiedostojärjestelmä",
	MsgAuditModifyFilesystem:        "muokkaa tiedostojärjestelmää",
	MsgAuditRemoveFilesystem:        "poista tiedostojärjestelmä",
	MsgAuditAddRepository:           "lisää repositorio",
	MsgAuditRemoveRepository:        "poista repositorio",
	MsgAuditMoveRepository:          "siirrä repositorio",
	MsgAuditRefreshRepositories:     "päivitä repositoriot",
	MsgAuditInstallPackage:          "asenna paketti",
	MsgAuditUninstallPackage:        "poista paketti",
	MsgAuditPurgeUninstalledVolumes: "tyhjennä poistetut volyymit",
	MsgAuditPurgeVolumes:            "tyhjennä volyymit",
	MsgAuditDisablePackage:          "poista paketti käytöstä",
	MsgAuditEnablePackage:           "ota paketti käyttöön",
	MsgAuditSetUnitStatus:           "aseta yksikön tila",
	MsgAuditCreateAccount:           "luo tili",
	MsgAuditUpdateAccount:           "päivitä tili",
	MsgAuditDisableAccount:          "poista tili käytöstä",
	MsgAuditAuthenticate:            "todenna",
	MsgAuditRevokeSession:           "peruuta istunto",
	MsgAuditUpdateSetting:           "päivitä asetus",
	MsgAuditDismissUpgrades:         "hylkää pakettipäivitykset",
	MsgAuditUploadArchive:           "lataa arkisto",
	MsgAuditDownloadArchive:         "lataa arkisto alas",
	MsgAuditCreatePage:              "luo sivu",
	MsgAuditUpdatePage:              "päivitä sivu",
	MsgAuditRemovePage:              "poista sivu",
	MsgAuditRebuildPage:             "rakenna sivu uudelleen",
	MsgAuditUploadPageArchive:       "lataa sivuarkisto",
	MsgAuditEnableAccount:           "ota tili käyttöön",
	MsgAuditRebuildGit:              "rakenna git uudelleen",
	MsgAuditUploadVMImage:           "lataa vm-vedos",
	MsgAuditDeleteVMImage:           "poista vm-vedos",
	MsgAuditAddDNSRecord:            "lisää dns-tietue",
	MsgAuditRemoveDNSRecord:         "poista dns-tietue",
	MsgAuditSetDNSTLD:               "aseta dns-tld",
	MsgAuditSetupDNS:                "määritä dns",
	MsgAuditRemovePackageVolume:     "poista paketin volyymi",
	MsgAuditRemovePackageVolumeGroup: "poista paketin volyymiryhmä",
	MsgAuditClearLastResponses:      "tyhjennä välimuistiin tallennetut asennusvastaukset",
	MsgAuditSetSystemServiceStatus:  "aseta järjestelmäpalvelun tila",
	MsgAuditRefreshSystemServices:   "päivitä järjestelmäpalvelut",
	MsgAuditCreateNetwork:           "luo verkko",
	MsgAuditRemoveNetwork:           "poista verkko",
	MsgAuditEnableNetwork:           "ota verkko käyttöön",
	MsgAuditDisableNetwork:          "poista verkko käytöstä",
	MsgAuditAddNetworkPeer:          "lisää verkon vertaislaite",
	MsgAuditRemoveNetworkPeer:       "poista verkon vertaislaite",
	MsgAuditRefreshNetworkPeer:      "päivitä verkon vertaislaite",

	// WireGuard-only account restrictions.
	MsgAuthWireGuardRestricted:    "tämä tili voi käyttää vain wireguard-rekisteröintipäätepisteitä",
	MsgAuthWireGuardNetworkDenied: "tällä tilillä ei ole käyttöoikeutta kyseiseen verkkoon",
	MsgAuthWireGuardPeerNotOwned:  "tämä tili voi päivittää vain itse rekisteröimiään vertaislaitteita",
}

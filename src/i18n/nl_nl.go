package i18n

// nlNLMessages contains all Dutch translations.
var nlNLMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "autorisatietoken ontbreekt",
	MsgAuthInvalidSession: "ongeldige sessie",
	MsgAuthAdminRequired:  "beheerderstoegang vereist",

	// Authentication.
	MsgAuthInvalidCredentials: "ongeldige inloggegevens",

	// Account management.
	MsgAccountAdminStatusImmutable: "beheerdersstatus kan na het aanmaken van het account niet meer worden gewijzigd",
	MsgAccountListError:            "accounts weergeven",
	MsgAccountCheckSessions:        "actieve beheerderssessies controleren",
	MsgAccountCreateFailed:         "aanmaken van account mislukt",

	// Settings.
	MsgSettingNotFound:     "instelling %q niet gevonden",
	MsgSettingKeyRequired:  "sleutel is vereist",
	MsgSettingInvalidBytes: "ongeldige bytewaarde voor %q: %v",
	MsgSettingsMgrMissing:  "instellingenbeheer niet beschikbaar",

	// Audit.
	MsgAuditNotConfigured: "auditregistratie niet geconfigureerd",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "in-/uitschakelen niet toegestaan",
	MsgUnitCannotStopController:    "systemcontroller kan niet worden gestopt",
	MsgUnitInvalidLines:            "ongeldige parameter voor regels",
	MsgUnitInvalidSince:            "ongeldige since-parameter",
	MsgUnitInvalidUntil:            "ongeldige until-parameter",
	MsgUnitInvalidPriority:         "ongeldige prioriteitsparameter",

	// Repository management.
	MsgRepoInvalidURL: "ongeldige url",

	// Pages management.
	MsgPagesNotConfigured:    "pages niet geconfigureerd",
	MsgPagesGitNotConfigured: "git-client of pages-map niet geconfigureerd",

	// Package installation.
	MsgInstallNoRepoRoot:      "geen repository-root geconfigureerd",
	MsgInstallSummaryUpgrade:  "%s upgraden van %s naar %s",
	MsgInstallSummaryInstall:  "%s %s installeren",
	MsgInstallSummaryImage:    "Image: %s",
	MsgInstallSummaryVolumes:  "%d volume(s)",
	MsgInstallSummaryNewVols:  "%d nieuw",
	MsgInstallSummaryMigrated: "%d gemigreerd",
	MsgInstallSummaryNoVols:   "Geen volumes",
	MsgInstallSummaryPorts:    "Externe poorten: %s",
	MsgInstallSummaryConfig:   "Configuratie vereist",
	MsgInstallSummaryVMImage:  "VM-image: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, naam en versie zijn vereist",
	MsgManifestNotFound:       "pakketmanifest niet gevonden: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repo, naam en versie zijn vereist",
	MsgRebuildRepoNotConfigured: "repository-root niet geconfigureerd",
	MsgRebuildGitNotConfigured:  "git-client niet geconfigureerd",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "veld subvolume is vereist",
	MsgArchiveFileRequired:      "archiefbestand vereist: %v",
	MsgArchiveUnsupportedFormat: "niet-ondersteund downloadformaat: %s",
	MsgArchiveUnpackSuccess:     "archief succesvol uitgepakt",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "pages-map niet geconfigureerd",
	MsgPagesNameRequired:           "veld naam is vereist",
	MsgPagesUploadArchiveOnly:      "uploaden is alleen toegestaan voor pages van het type archief",
	MsgPagesArchiveRebuildRequired: "archief-pages moeten opnieuw worden opgebouwd door een nieuw archief te uploaden via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitoring is niet geconfigureerd",

	// Upgrades.
	MsgUpgradeSettingsMissing: "instellingenbeheer niet beschikbaar",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "bestandssysteem aanmaken",
	MsgAuditModifyFilesystem:        "bestandssysteem wijzigen",
	MsgAuditRemoveFilesystem:        "bestandssysteem verwijderen",
	MsgAuditAddRepository:           "repository toevoegen",
	MsgAuditRemoveRepository:        "repository verwijderen",
	MsgAuditMoveRepository:          "repository verplaatsen",
	MsgAuditRefreshRepositories:     "repositories vernieuwen",
	MsgAuditInstallPackage:          "pakket installeren",
	MsgAuditUninstallPackage:        "pakket verwijderen",
	MsgAuditPurgeUninstalledVolumes: "verwijderde volumes opschonen",
	MsgAuditPurgeVolumes:            "volumes opschonen",
	MsgAuditDisablePackage:          "pakket uitschakelen",
	MsgAuditEnablePackage:           "pakket inschakelen",
	MsgAuditSetUnitStatus:           "unit-status instellen",
	MsgAuditCreateAccount:           "account aanmaken",
	MsgAuditUpdateAccount:           "account bijwerken",
	MsgAuditDisableAccount:          "account uitschakelen",
	MsgAuditAuthenticate:            "authenticeren",
	MsgAuditRevokeSession:           "sessie intrekken",
	MsgAuditUpdateSetting:           "instelling bijwerken",
	MsgAuditDismissUpgrades:         "pakketupgrades negeren",
	MsgAuditUploadArchive:           "archief uploaden",
	MsgAuditDownloadArchive:         "archief downloaden",
	MsgAuditCreatePage:              "pagina aanmaken",
	MsgAuditUpdatePage:              "pagina bijwerken",
	MsgAuditRemovePage:              "pagina verwijderen",
	MsgAuditRebuildPage:             "pagina opnieuw opbouwen",
	MsgAuditUploadPageArchive:       "pagina-archief uploaden",
	MsgAuditEnableAccount:           "account inschakelen",
	MsgAuditRebuildGit:              "git opnieuw opbouwen",
	MsgAuditUploadVMImage:           "vm-image uploaden",
	MsgAuditDeleteVMImage:           "vm-image verwijderen",
	MsgAuditAddDNSRecord:            "dns-record toevoegen",
	MsgAuditRemoveDNSRecord:         "dns-record verwijderen",
	MsgAuditSetDNSTLD:               "dns-tld instellen",
	MsgAuditSetupDNS:                "dns instellen",
	MsgAuditRemovePackageVolume:     "pakketvolume verwijderen",
	MsgAuditRemovePackageVolumeGroup: "pakketvolumegroep verwijderen",
	MsgAuditClearLastResponses:      "gecachte installatie-antwoorden wissen",
	MsgAuditSetSystemServiceStatus:  "status van systeemservice instellen",
	MsgAuditRefreshSystemServices:   "systeemservices vernieuwen",
	MsgAuditCreateNetwork:           "netwerk aanmaken",
	MsgAuditRemoveNetwork:           "netwerk verwijderen",
	MsgAuditEnableNetwork:           "netwerk inschakelen",
	MsgAuditDisableNetwork:          "netwerk uitschakelen",
	MsgAuditAddNetworkPeer:          "netwerkpeer toevoegen",
	MsgAuditRemoveNetworkPeer:       "netwerkpeer verwijderen",
	MsgAuditRefreshNetworkPeer:      "netwerkpeer vernieuwen",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "dit account mag alleen endpoints voor netwerkinschrijving en objectopslag gebruiken",
	MsgAuthNetworkOnlyNetworkDenied: "dit account is niet toegestaan op dat netwerk",
	MsgAuthWireGuardPeerNotOwned:  "dit account mag alleen peers vernieuwen die het zelf heeft ingeschreven",
	MsgAuthObjectStorageRequired:  "beheerders- of objectopslagtoegang vereist",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "archiefuploads en -downloads kunnen geen objectopslagpartitie aanspreken",
	MsgGfehNotConfigured:         "objectopslag is niet geconfigureerd",
	MsgGfehNameRequired:          "naamveld is verplicht",
	MsgGfehPartitionExists:       "partitie bestaat al",
	MsgGfehPartitionNotFound:     "partitie niet gevonden",
	MsgGfehNetworkRequired:       "netwerkveld is verplicht",
	MsgGfehPrincipalRequired:     "principal-veld is verplicht",
	MsgGfehPathRequired:          "padveld is verplicht",
	MsgGfehUnknownAccount:        "account bestaat niet",
	MsgAuditCreateGfehPartition:  "objectopslagpartitie aanmaken",
	MsgAuditModifyGfehPartition:  "objectopslagpartitie wijzigen",
	MsgAuditRemoveGfehPartition:  "objectopslagpartitie verwijderen",
	MsgAuditAddGfehPrincipal:     "objectopslaggebruiker toevoegen",
	MsgAuditRemoveGfehPrincipal:  "objectopslaggebruiker verwijderen",
	MsgAuditAddGfehGrant:         "objectopslagrecht toevoegen",
	MsgAuditRevokeGfehGrant:      "objectopslagrecht intrekken",
	MsgAuditWithdrawGfehExposure: "objectopslaglink intrekken",
}

package i18n

// deDEMessages contains all German translations.
var deDEMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "Autorisierungstoken fehlt",
	MsgAuthInvalidSession: "ungültige Sitzung",
	MsgAuthAdminRequired:  "Administratorzugriff erforderlich",

	// Authentication.
	MsgAuthInvalidCredentials: "ungültige Anmeldedaten",

	// Account management.
	MsgAccountAdminStatusImmutable: "Administratorstatus kann nach der Kontoerstellung nicht geändert werden",
	MsgAccountListError:            "Konten auflisten",
	MsgAccountCheckSessions:        "aktive Administratorsitzungen prüfen",
	MsgAccountCreateFailed:         "Kontoerstellung fehlgeschlagen",

	// Settings.
	MsgSettingNotFound:     "Einstellung %q nicht gefunden",
	MsgSettingKeyRequired:  "Schlüssel ist erforderlich",
	MsgSettingInvalidBytes: "ungültiger Byte-Wert für %q: %v",
	MsgSettingsMgrMissing:  "Einstellungsverwaltung nicht verfügbar",

	// Audit.
	MsgAuditNotConfigured: "Audit-Protokollierung nicht konfiguriert",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "Aktivieren/Deaktivieren nicht erlaubt",
	MsgUnitCannotStopController:    "systemcontroller kann nicht gestoppt werden",
	MsgUnitInvalidLines:            "ungültiger Parameter lines",
	MsgUnitInvalidSince:            "ungültiger Parameter since",
	MsgUnitInvalidUntil:            "ungültiger Parameter until",
	MsgUnitInvalidPriority:         "ungültiger Parameter priority",

	// Repository management.
	MsgRepoInvalidURL: "ungültige URL",

	// Pages management.
	MsgPagesNotConfigured:    "Pages nicht konfiguriert",
	MsgPagesGitNotConfigured: "Git-Client oder Pages-Verzeichnis nicht konfiguriert",

	// Package installation.
	MsgInstallNoRepoRoot:      "kein Repository-Stammverzeichnis konfiguriert",
	MsgInstallSummaryUpgrade:  "Upgrade von %s von %s auf %s",
	MsgInstallSummaryInstall:  "%s %s installieren",
	MsgInstallSummaryImage:    "Image: %s",
	MsgInstallSummaryVolumes:  "%d Volume(s)",
	MsgInstallSummaryNewVols:  "%d neu",
	MsgInstallSummaryMigrated: "%d migriert",
	MsgInstallSummaryNoVols:   "Keine Volumes",
	MsgInstallSummaryPorts:    "Externe Ports: %s",
	MsgInstallSummaryConfig:   "Konfiguration erforderlich",
	MsgInstallSummaryVMImage:  "VM-Image: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name und version sind erforderlich",
	MsgManifestNotFound:       "Paketmanifest nicht gefunden: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, name und version sind erforderlich",
	MsgRebuildRepoNotConfigured: "Repository-Stammverzeichnis nicht konfiguriert",
	MsgRebuildGitNotConfigured:  "Git-Client nicht konfiguriert",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "Feld subvolume ist erforderlich",
	MsgArchiveFileRequired:      "Archivdatei erforderlich: %v",
	MsgArchiveUnsupportedFormat: "nicht unterstütztes Download-Format: %s",
	MsgArchiveUnpackSuccess:     "Archiv erfolgreich entpackt",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "Pages-Verzeichnis nicht konfiguriert",
	MsgPagesNameRequired:           "Feld name ist erforderlich",
	MsgPagesUploadArchiveOnly:      "Upload ist nur für Pages vom Typ Archiv erlaubt",
	MsgPagesArchiveRebuildRequired: "Archiv-Pages müssen durch Hochladen eines neuen Archivs über /pages/upload neu erstellt werden",

	// Monitoring.
	MsgMonitoringNotConfigured: "Monitoring ist nicht konfiguriert",

	// Upgrades.
	MsgUpgradeSettingsMissing: "Einstellungsverwaltung nicht verfügbar",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "Dateisystem erstellen",
	MsgAuditModifyFilesystem:         "Dateisystem ändern",
	MsgAuditRemoveFilesystem:         "Dateisystem entfernen",
	MsgAuditAddRepository:            "Repository hinzufügen",
	MsgAuditRemoveRepository:         "Repository entfernen",
	MsgAuditMoveRepository:           "Repository verschieben",
	MsgAuditRefreshRepositories:      "Repositories aktualisieren",
	MsgAuditInstallPackage:           "Paket installieren",
	MsgAuditUninstallPackage:         "Paket deinstallieren",
	MsgAuditPurgeUninstalledVolumes:  "deinstallierte Volumes bereinigen",
	MsgAuditPurgeVolumes:             "Volumes bereinigen",
	MsgAuditDisablePackage:           "Paket deaktivieren",
	MsgAuditEnablePackage:            "Paket aktivieren",
	MsgAuditSetUnitStatus:            "Unit-Status setzen",
	MsgAuditCreateAccount:            "Konto erstellen",
	MsgAuditUpdateAccount:            "Konto aktualisieren",
	MsgAuditDisableAccount:           "Konto deaktivieren",
	MsgAuditAuthenticate:             "authentifizieren",
	MsgAuditRevokeSession:            "Sitzung widerrufen",
	MsgAuditUpdateSetting:            "Einstellung aktualisieren",
	MsgAuditDismissUpgrades:          "Paket-Upgrades verwerfen",
	MsgAuditUploadArchive:            "Archiv hochladen",
	MsgAuditDownloadArchive:          "Archiv herunterladen",
	MsgAuditCreatePage:               "Page erstellen",
	MsgAuditUpdatePage:               "Page aktualisieren",
	MsgAuditRemovePage:               "Page entfernen",
	MsgAuditRebuildPage:              "Page neu erstellen",
	MsgAuditUploadPageArchive:        "Page-Archiv hochladen",
	MsgAuditEnableAccount:            "Konto aktivieren",
	MsgAuditRebuildGit:               "Git neu erstellen",
	MsgAuditUploadVMImage:            "VM-Image hochladen",
	MsgAuditDeleteVMImage:            "VM-Image löschen",
	MsgAuditAddDNSRecord:             "DNS-Eintrag hinzufügen",
	MsgAuditRemoveDNSRecord:          "DNS-Eintrag entfernen",
	MsgAuditSetDNSTLD:                "DNS-TLD setzen",
	MsgAuditSetupDNS:                 "DNS einrichten",
	MsgAuditRemovePackageVolume:      "Paket-Volume entfernen",
	MsgAuditRemovePackageVolumeGroup: "Paket-Volume-Gruppe entfernen",
	MsgAuditClearLastResponses:       "zwischengespeicherte Installationsantworten löschen",
	MsgAuditSetSystemServiceStatus:   "Systemdienststatus setzen",
	MsgAuditRefreshSystemServices:    "Systemdienste aktualisieren",
	MsgAuditCreateNetwork:            "Netzwerk erstellen",
	MsgAuditRemoveNetwork:            "Netzwerk entfernen",
	MsgAuditEnableNetwork:            "Netzwerk aktivieren",
	MsgAuditDisableNetwork:           "Netzwerk deaktivieren",
	MsgAuditAddNetworkPeer:           "Netzwerk-Peer hinzufügen",
	MsgAuditRemoveNetworkPeer:        "Netzwerk-Peer entfernen",
	MsgAuditRefreshNetworkPeer:       "Netzwerk-Peer aktualisieren",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "dieses Konto darf nur Endpunkte für Netzwerk-Enrollment und Objektspeicher verwenden",
	MsgAuthNetworkOnlyNetworkDenied: "dieses Konto ist in diesem Netzwerk nicht zugelassen",
	MsgAuthWireGuardPeerNotOwned:    "dieses Konto darf nur Peers aktualisieren, die es selbst registriert hat",
	MsgAuthSessionNotOwned:          "dieses Konto darf nur eigene Sitzungen widerrufen",
	MsgAuthObjectStorageRequired:    "Administrator- oder Objektspeicherzugriff erforderlich",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "Archiv-Uploads und -Downloads können keine Objektspeicher-Partition adressieren",
	MsgGfehNotConfigured:         "Objektspeicher ist nicht konfiguriert",
	MsgGfehNameRequired:          "Namensfeld ist erforderlich",
	MsgGfehPartitionExists:       "Partition existiert bereits",
	MsgGfehPartitionNotFound:     "Partition nicht gefunden",
	MsgGfehNetworkRequired:       "Netzwerkfeld ist erforderlich",
	MsgGfehPrincipalRequired:     "Principal-Feld ist erforderlich",
	MsgGfehPathRequired:          "Pfadfeld ist erforderlich",
	MsgGfehUnknownAccount:        "Konto nicht vorhanden",
	MsgAuditCreateGfehPartition:  "Objektspeicher-Partition erstellen",
	MsgAuditModifyGfehPartition:  "Objektspeicher-Partition ändern",
	MsgAuditRemoveGfehPartition:  "Objektspeicher-Partition entfernen",
	MsgAuditAddGfehPrincipal:     "Objektspeicher-Benutzer hinzufügen",
	MsgAuditRemoveGfehPrincipal:  "Objektspeicher-Benutzer entfernen",
	MsgAuditAddGfehGrant:         "Objektspeicher-Berechtigung hinzufügen",
	MsgAuditRevokeGfehGrant:      "Objektspeicher-Berechtigung widerrufen",
	MsgAuditWithdrawGfehExposure: "Objektspeicher-Link zurückziehen",
}

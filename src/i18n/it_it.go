package i18n

// itITMessages contains all Italian translations.
var itITMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "token di autorizzazione mancante",
	MsgAuthInvalidSession: "sessione non valida",
	MsgAuthAdminRequired:  "accesso amministratore richiesto",

	// Authentication.
	MsgAuthInvalidCredentials: "credenziali non valide",

	// Account management.
	MsgAccountAdminStatusImmutable: "lo stato di amministratore non può essere modificato dopo la creazione dell'account",
	MsgAccountListError:            "elenca account",
	MsgAccountCheckSessions:        "verifica sessioni amministratore attive",
	MsgAccountCreateFailed:         "creazione dell'account non riuscita",

	// Settings.
	MsgSettingNotFound:     "impostazione %q non trovata",
	MsgSettingKeyRequired:  "la chiave è obbligatoria",
	MsgSettingInvalidBytes: "valore in byte non valido per %q: %v",
	MsgSettingsMgrMissing:  "gestore delle impostazioni non disponibile",

	// Audit.
	MsgAuditNotConfigured: "registrazione di audit non configurata",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "abilitazione/disabilitazione non consentita",
	MsgUnitCannotStopController:    "impossibile arrestare systemcontroller",
	MsgUnitInvalidLines:            "parametro lines non valido",
	MsgUnitInvalidSince:            "parametro since non valido",
	MsgUnitInvalidUntil:            "parametro until non valido",
	MsgUnitInvalidPriority:         "parametro priority non valido",

	// Repository management.
	MsgRepoInvalidURL: "url non valido",

	// Pages management.
	MsgPagesNotConfigured:    "pagine non configurate",
	MsgPagesGitNotConfigured: "client git o directory delle pagine non configurati",

	// Package installation.
	MsgInstallNoRepoRoot:      "nessuna radice del repository configurata",
	MsgInstallSummaryUpgrade:  "Aggiorna %s dalla versione %s alla %s",
	MsgInstallSummaryInstall:  "Installa %s %s",
	MsgInstallSummaryImage:    "Immagine: %s",
	MsgInstallSummaryVolumes:  "%d volume/i",
	MsgInstallSummaryNewVols:  "%d nuovi",
	MsgInstallSummaryMigrated: "%d migrati",
	MsgInstallSummaryNoVols:   "Nessun volume",
	MsgInstallSummaryPorts:    "Porte esterne: %s",
	MsgInstallSummaryConfig:   "Configurazione richiesta",
	MsgInstallSummaryVMImage:  "Immagine VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, nome e versione sono obbligatori",
	MsgManifestNotFound:       "manifest del pacchetto non trovato: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repo, nome e versione sono obbligatori",
	MsgRebuildRepoNotConfigured: "radice del repository non configurata",
	MsgRebuildGitNotConfigured:  "client git non configurato",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "il campo subvolume è obbligatorio",
	MsgArchiveFileRequired:      "file di archivio obbligatorio: %v",
	MsgArchiveUnsupportedFormat: "formato di download non supportato: %s",
	MsgArchiveUnpackSuccess:     "archivio estratto correttamente",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "directory delle pagine non configurata",
	MsgPagesNameRequired:           "il campo nome è obbligatorio",
	MsgPagesUploadArchiveOnly:      "il caricamento è consentito solo per le pagine di tipo archivio",
	MsgPagesArchiveRebuildRequired: "le pagine di tipo archivio devono essere ricostruite caricando un nuovo archivio tramite /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "il monitoraggio non è configurato",

	// Upgrades.
	MsgUpgradeSettingsMissing: "gestore delle impostazioni non disponibile",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "crea filesystem",
	MsgAuditModifyFilesystem:        "modifica filesystem",
	MsgAuditRemoveFilesystem:        "rimuovi filesystem",
	MsgAuditAddRepository:           "aggiungi repository",
	MsgAuditRemoveRepository:        "rimuovi repository",
	MsgAuditMoveRepository:          "sposta repository",
	MsgAuditRefreshRepositories:     "aggiorna repository",
	MsgAuditInstallPackage:          "installa pacchetto",
	MsgAuditUninstallPackage:        "disinstalla pacchetto",
	MsgAuditPurgeUninstalledVolumes: "elimina volumi disinstallati",
	MsgAuditPurgeVolumes:            "elimina volumi",
	MsgAuditDisablePackage:          "disabilita pacchetto",
	MsgAuditEnablePackage:           "abilita pacchetto",
	MsgAuditSetUnitStatus:           "imposta stato unità",
	MsgAuditCreateAccount:           "crea account",
	MsgAuditUpdateAccount:           "aggiorna account",
	MsgAuditDisableAccount:          "disabilita account",
	MsgAuditAuthenticate:            "autentica",
	MsgAuditRevokeSession:           "revoca sessione",
	MsgAuditUpdateSetting:           "aggiorna impostazione",
	MsgAuditDismissUpgrades:         "ignora aggiornamenti pacchetti",
	MsgAuditUploadArchive:           "carica archivio",
	MsgAuditDownloadArchive:         "scarica archivio",
	MsgAuditCreatePage:              "crea pagina",
	MsgAuditUpdatePage:              "aggiorna pagina",
	MsgAuditRemovePage:              "rimuovi pagina",
	MsgAuditRebuildPage:             "ricostruisci pagina",
	MsgAuditUploadPageArchive:       "carica archivio pagina",
	MsgAuditEnableAccount:           "abilita account",
	MsgAuditRebuildGit:              "ricostruisci git",
	MsgAuditUploadVMImage:           "carica immagine vm",
	MsgAuditDeleteVMImage:           "elimina immagine vm",
	MsgAuditAddDNSRecord:            "aggiungi record dns",
	MsgAuditRemoveDNSRecord:         "rimuovi record dns",
	MsgAuditSetDNSTLD:               "imposta tld dns",
	MsgAuditSetupDNS:                "configura dns",
	MsgAuditRemovePackageVolume:     "rimuovi volume pacchetto",
	MsgAuditRemovePackageVolumeGroup: "rimuovi gruppo volumi pacchetto",
	MsgAuditClearLastResponses:      "cancella risposte di installazione memorizzate",
	MsgAuditSetSystemServiceStatus:  "imposta stato servizio di sistema",
	MsgAuditRefreshSystemServices:   "aggiorna servizi di sistema",
	MsgAuditCreateNetwork:           "crea rete",
	MsgAuditRemoveNetwork:           "rimuovi rete",
	MsgAuditEnableNetwork:           "abilita rete",
	MsgAuditDisableNetwork:          "disabilita rete",
	MsgAuditAddNetworkPeer:          "aggiungi peer di rete",
	MsgAuditRemoveNetworkPeer:       "rimuovi peer di rete",
	MsgAuditRefreshNetworkPeer:      "aggiorna peer di rete",

	// WireGuard-only account restrictions.
	MsgAuthWireGuardRestricted:    "questo account può utilizzare solo gli endpoint di registrazione wireguard",
	MsgAuthWireGuardNetworkDenied: "questo account non è autorizzato su tale rete",
	MsgAuthWireGuardPeerNotOwned:  "questo account può aggiornare solo i peer che ha registrato",
}

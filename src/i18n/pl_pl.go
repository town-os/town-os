package i18n

// plPLMessages contains all Polish translations.
var plPLMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "brak tokenu autoryzacji",
	MsgAuthInvalidSession: "nieprawidłowa sesja",
	MsgAuthAdminRequired:  "wymagany dostęp administratora",

	// Authentication.
	MsgAuthInvalidCredentials: "nieprawidłowe dane logowania",

	// Account management.
	MsgAccountAdminStatusImmutable: "statusu administratora nie można zmienić po utworzeniu konta",
	MsgAccountListError:            "wyświetl listę kont",
	MsgAccountCheckSessions:        "sprawdź aktywne sesje administratora",
	MsgAccountCreateFailed:         "utworzenie konta nie powiodło się",

	// Settings.
	MsgSettingNotFound:     "nie znaleziono ustawienia %q",
	MsgSettingKeyRequired:  "klucz jest wymagany",
	MsgSettingInvalidBytes: "nieprawidłowa wartość bajtów dla %q: %v",
	MsgSettingsMgrMissing:  "menedżer ustawień jest niedostępny",

	// Audit.
	MsgAuditNotConfigured: "rejestrowanie audytu nie jest skonfigurowane",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "włączanie/wyłączanie jest niedozwolone",
	MsgUnitCannotStopController:    "nie można zatrzymać kontrolera systemu",
	MsgUnitInvalidLines:            "nieprawidłowy parametr lines",
	MsgUnitInvalidSince:            "nieprawidłowy parametr since",
	MsgUnitInvalidUntil:            "nieprawidłowy parametr until",
	MsgUnitInvalidPriority:         "nieprawidłowy parametr priority",

	// Repository management.
	MsgRepoInvalidURL: "nieprawidłowy adres url",

	// Pages management.
	MsgPagesNotConfigured:    "strony nie są skonfigurowane",
	MsgPagesGitNotConfigured: "klient git lub katalog stron nie jest skonfigurowany",

	// Package installation.
	MsgInstallNoRepoRoot:      "nie skonfigurowano katalogu głównego repozytorium",
	MsgInstallSummaryUpgrade:  "Aktualizacja %s z %s do %s",
	MsgInstallSummaryInstall:  "Instalacja %s %s",
	MsgInstallSummaryImage:    "Obraz: %s",
	MsgInstallSummaryVolumes:  "%d wolumin(ów)",
	MsgInstallSummaryNewVols:  "%d nowych",
	MsgInstallSummaryMigrated: "%d zmigrowanych",
	MsgInstallSummaryNoVols:   "Brak woluminów",
	MsgInstallSummaryPorts:    "Porty zewnętrzne: %s",
	MsgInstallSummaryConfig:   "Wymagana konfiguracja",
	MsgInstallSummaryVMImage:  "Obraz VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repozytorium, nazwa i wersja są wymagane",
	MsgManifestNotFound:       "nie znaleziono manifestu pakietu: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repozytorium, nazwa i wersja są wymagane",
	MsgRebuildRepoNotConfigured: "katalog główny repozytorium nie jest skonfigurowany",
	MsgRebuildGitNotConfigured:  "klient git nie jest skonfigurowany",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "pole subwoluminu jest wymagane",
	MsgArchiveFileRequired:      "wymagany plik archiwum: %v",
	MsgArchiveUnsupportedFormat: "nieobsługiwany format pobierania: %s",
	MsgArchiveUnpackSuccess:     "archiwum rozpakowano pomyślnie",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "katalog stron nie jest skonfigurowany",
	MsgPagesNameRequired:           "pole nazwy jest wymagane",
	MsgPagesUploadArchiveOnly:      "przesyłanie jest dozwolone tylko dla stron typu archiwum",
	MsgPagesArchiveRebuildRequired: "strony typu archiwum należy przebudować, przesyłając nowe archiwum przez /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitorowanie nie jest skonfigurowane",

	// Upgrades.
	MsgUpgradeSettingsMissing: "menedżer ustawień jest niedostępny",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "utwórz system plików",
	MsgAuditModifyFilesystem:         "zmodyfikuj system plików",
	MsgAuditRemoveFilesystem:         "usuń system plików",
	MsgAuditAddRepository:            "dodaj repozytorium",
	MsgAuditRemoveRepository:         "usuń repozytorium",
	MsgAuditMoveRepository:           "przenieś repozytorium",
	MsgAuditRefreshRepositories:      "odśwież repozytoria",
	MsgAuditInstallPackage:           "zainstaluj pakiet",
	MsgAuditUninstallPackage:         "odinstaluj pakiet",
	MsgAuditPurgeUninstalledVolumes:  "wyczyść odinstalowane woluminy",
	MsgAuditPurgeVolumes:             "wyczyść woluminy",
	MsgAuditDisablePackage:           "wyłącz pakiet",
	MsgAuditEnablePackage:            "włącz pakiet",
	MsgAuditSetUnitStatus:            "ustaw status jednostki",
	MsgAuditCreateAccount:            "utwórz konto",
	MsgAuditUpdateAccount:            "zaktualizuj konto",
	MsgAuditDisableAccount:           "wyłącz konto",
	MsgAuditAuthenticate:             "uwierzytelnij",
	MsgAuditRevokeSession:            "unieważnij sesję",
	MsgAuditUpdateSetting:            "zaktualizuj ustawienie",
	MsgAuditDismissUpgrades:          "odrzuć aktualizacje pakietów",
	MsgAuditUploadArchive:            "prześlij archiwum",
	MsgAuditDownloadArchive:          "pobierz archiwum",
	MsgAuditCreatePage:               "utwórz stronę",
	MsgAuditUpdatePage:               "zaktualizuj stronę",
	MsgAuditRemovePage:               "usuń stronę",
	MsgAuditRebuildPage:              "przebuduj stronę",
	MsgAuditUploadPageArchive:        "prześlij archiwum strony",
	MsgAuditEnableAccount:            "włącz konto",
	MsgAuditRebuildGit:               "przebuduj git",
	MsgAuditUploadVMImage:            "prześlij obraz vm",
	MsgAuditDeleteVMImage:            "usuń obraz vm",
	MsgAuditAddDNSRecord:             "dodaj rekord dns",
	MsgAuditRemoveDNSRecord:          "usuń rekord dns",
	MsgAuditSetDNSTLD:                "ustaw domenę dns",
	MsgAuditSetupDNS:                 "skonfiguruj dns",
	MsgAuditRemovePackageVolume:      "usuń wolumin pakietu",
	MsgAuditRemovePackageVolumeGroup: "usuń grupę woluminów pakietu",
	MsgAuditClearLastResponses:       "wyczyść zapisane odpowiedzi instalacji",
	MsgAuditSetSystemServiceStatus:   "ustaw status usługi systemowej",
	MsgAuditRefreshSystemServices:    "odśwież usługi systemowe",
	MsgAuditCreateNetwork:            "utwórz sieć",
	MsgAuditRemoveNetwork:            "usuń sieć",
	MsgAuditEnableNetwork:            "włącz sieć",
	MsgAuditDisableNetwork:           "wyłącz sieć",
	MsgAuditAddNetworkPeer:           "dodaj węzeł sieci",
	MsgAuditRemoveNetworkPeer:        "usuń węzeł sieci",
	MsgAuditRefreshNetworkPeer:       "odśwież węzeł sieci",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "to konto może używać wyłącznie punktów końcowych rejestracji sieci i magazynu obiektów",
	MsgAuthNetworkOnlyNetworkDenied: "to konto nie ma uprawnień w tej sieci",
	MsgAuthWireGuardPeerNotOwned:    "to konto może odświeżać tylko węzły, które samo zarejestrowało",
	MsgAuthSessionNotOwned:          "to konto może unieważnić tylko własne sesje",
	MsgAuthObjectStorageRequired:    "wymagany dostęp administratora lub magazynu obiektów",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "przesyłanie i pobieranie archiwów nie może dotyczyć partycji magazynu obiektów",
	MsgGfehNotConfigured:         "magazyn obiektów nie jest skonfigurowany",
	MsgGfehNameRequired:          "pole nazwy jest wymagane",
	MsgGfehPartitionExists:       "partycja już istnieje",
	MsgGfehPartitionNotFound:     "nie znaleziono partycji",
	MsgGfehNetworkRequired:       "pole sieci jest wymagane",
	MsgGfehPrincipalRequired:     "pole podmiotu jest wymagane",
	MsgGfehPathRequired:          "pole ścieżki jest wymagane",
	MsgGfehUnknownAccount:        "nie ma takiego konta",
	MsgAuditCreateGfehPartition:  "utwórz partycję magazynu obiektów",
	MsgAuditModifyGfehPartition:  "zmień partycję magazynu obiektów",
	MsgAuditRemoveGfehPartition:  "usuń partycję magazynu obiektów",
	MsgAuditAddGfehPrincipal:     "dodaj użytkownika magazynu obiektów",
	MsgAuditRemoveGfehPrincipal:  "usuń użytkownika magazynu obiektów",
	MsgAuditAddGfehGrant:         "dodaj uprawnienie magazynu obiektów",
	MsgAuditRevokeGfehGrant:      "cofnij uprawnienie magazynu obiektów",
	MsgAuditWithdrawGfehExposure: "wycofaj odnośnik magazynu obiektów",
}

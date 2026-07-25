package i18n

// ukUAMessages contains all Ukrainian translations.
var ukUAMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "відсутній токен авторизації",
	MsgAuthInvalidSession: "недійсна сесія",
	MsgAuthAdminRequired:  "потрібен доступ адміністратора",

	// Authentication.
	MsgAuthInvalidCredentials: "недійсні облікові дані",

	// Account management.
	MsgAccountAdminStatusImmutable: "статус адміністратора не можна змінити після створення облікового запису",
	MsgAccountListError:            "перелік облікових записів",
	MsgAccountCheckSessions:        "перевірити активні сесії адміністратора",
	MsgAccountCreateFailed:         "не вдалося створити обліковий запис",

	// Settings.
	MsgSettingNotFound:     "налаштування %q не знайдено",
	MsgSettingKeyRequired:  "потрібен ключ",
	MsgSettingInvalidBytes: "недійсне значення байтів для %q: %v",
	MsgSettingsMgrMissing:  "менеджер налаштувань недоступний",

	// Audit.
	MsgAuditNotConfigured: "журнал аудиту не налаштовано",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "увімкнення/вимкнення не дозволено",
	MsgUnitCannotStopController:    "неможливо зупинити systemcontroller",
	MsgUnitInvalidLines:            "недійсний параметр lines",
	MsgUnitInvalidSince:            "недійсний параметр since",
	MsgUnitInvalidUntil:            "недійсний параметр until",
	MsgUnitInvalidPriority:         "недійсний параметр priority",

	// Repository management.
	MsgRepoInvalidURL: "недійсна url-адреса",

	// Pages management.
	MsgPagesNotConfigured:    "сторінки не налаштовано",
	MsgPagesGitNotConfigured: "git-клієнт або каталог сторінок не налаштовано",

	// Package installation.
	MsgInstallNoRepoRoot:      "не налаштовано кореневий каталог репозиторію",
	MsgInstallSummaryUpgrade:  "Оновлення %s з %s до %s",
	MsgInstallSummaryInstall:  "Встановлення %s %s",
	MsgInstallSummaryImage:    "Образ: %s",
	MsgInstallSummaryVolumes:  "%d том(ів)",
	MsgInstallSummaryNewVols:  "%d нових",
	MsgInstallSummaryMigrated: "%d перенесено",
	MsgInstallSummaryNoVols:   "Немає томів",
	MsgInstallSummaryPorts:    "Зовнішні порти: %s",
	MsgInstallSummaryConfig:   "Потрібне налаштування",
	MsgInstallSummaryVMImage:  "Образ ВМ: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "потрібні repo, name та version",
	MsgManifestNotFound:       "маніфест пакета не знайдено: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "потрібні repo, name та version",
	MsgRebuildRepoNotConfigured: "кореневий каталог репозиторію не налаштовано",
	MsgRebuildGitNotConfigured:  "git-клієнт не налаштовано",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "поле subvolume обовʼязкове",
	MsgArchiveFileRequired:      "потрібен файл архіву: %v",
	MsgArchiveUnsupportedFormat: "непідтримуваний формат завантаження: %s",
	MsgArchiveUnpackSuccess:     "архів успішно розпаковано",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "каталог сторінок не налаштовано",
	MsgPagesNameRequired:           "поле name обовʼязкове",
	MsgPagesUploadArchiveOnly:      "завантаження дозволено лише для сторінок типу archive",
	MsgPagesArchiveRebuildRequired: "сторінки типу archive потрібно перебудовувати, завантаживши новий архів через /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "моніторинг не налаштовано",

	// Upgrades.
	MsgUpgradeSettingsMissing: "менеджер налаштувань недоступний",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "створити файлову систему",
	MsgAuditModifyFilesystem:        "змінити файлову систему",
	MsgAuditRemoveFilesystem:        "видалити файлову систему",
	MsgAuditAddRepository:           "додати репозиторій",
	MsgAuditRemoveRepository:        "видалити репозиторій",
	MsgAuditMoveRepository:          "перемістити репозиторій",
	MsgAuditRefreshRepositories:     "оновити репозиторії",
	MsgAuditInstallPackage:          "встановити пакет",
	MsgAuditUninstallPackage:        "видалити пакет",
	MsgAuditPurgeUninstalledVolumes: "очистити видалені томи",
	MsgAuditPurgeVolumes:            "очистити томи",
	MsgAuditDisablePackage:          "вимкнути пакет",
	MsgAuditEnablePackage:           "увімкнути пакет",
	MsgAuditSetUnitStatus:           "встановити статус юніта",
	MsgAuditCreateAccount:           "створити обліковий запис",
	MsgAuditUpdateAccount:           "оновити обліковий запис",
	MsgAuditDisableAccount:          "вимкнути обліковий запис",
	MsgAuditAuthenticate:            "автентифікація",
	MsgAuditRevokeSession:           "відкликати сесію",
	MsgAuditUpdateSetting:           "оновити налаштування",
	MsgAuditDismissUpgrades:         "відхилити оновлення пакетів",
	MsgAuditUploadArchive:           "завантажити архів",
	MsgAuditDownloadArchive:         "звантажити архів",
	MsgAuditCreatePage:              "створити сторінку",
	MsgAuditUpdatePage:              "оновити сторінку",
	MsgAuditRemovePage:              "видалити сторінку",
	MsgAuditRebuildPage:             "перебудувати сторінку",
	MsgAuditUploadPageArchive:       "завантажити архів сторінки",
	MsgAuditEnableAccount:           "увімкнути обліковий запис",
	MsgAuditRebuildGit:              "перебудувати git",
	MsgAuditUploadVMImage:           "завантажити образ ВМ",
	MsgAuditDeleteVMImage:           "видалити образ ВМ",
	MsgAuditAddDNSRecord:            "додати запис DNS",
	MsgAuditRemoveDNSRecord:         "видалити запис DNS",
	MsgAuditSetDNSTLD:               "встановити домен DNS",
	MsgAuditSetupDNS:                "налаштувати DNS",
	MsgAuditRemovePackageVolume:     "видалити том пакета",
	MsgAuditRemovePackageVolumeGroup: "видалити групу томів пакета",
	MsgAuditClearLastResponses:      "очистити кешовані відповіді встановлення",
	MsgAuditSetSystemServiceStatus:  "встановити статус системної служби",
	MsgAuditRefreshSystemServices:   "оновити системні служби",
	MsgAuditCreateNetwork:           "створити мережу",
	MsgAuditRemoveNetwork:           "видалити мережу",
	MsgAuditEnableNetwork:           "увімкнути мережу",
	MsgAuditDisableNetwork:          "вимкнути мережу",
	MsgAuditAddNetworkPeer:          "додати вузол мережі",
	MsgAuditRemoveNetworkPeer:       "видалити вузол мережі",
	MsgAuditRefreshNetworkPeer:      "оновити вузол мережі",

	// WireGuard-only account restrictions.
	MsgAuthWireGuardRestricted:    "цей обліковий запис може використовувати лише кінцеві точки реєстрації wireguard",
	MsgAuthWireGuardNetworkDenied: "цьому обліковому запису не дозволено в цій мережі",
	MsgAuthWireGuardPeerNotOwned:  "цей обліковий запис може оновлювати лише вузли, які він зареєстрував",
}

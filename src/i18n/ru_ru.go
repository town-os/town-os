package i18n

// ruRUMessages contains all Russian translations.
var ruRUMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "отсутствует токен авторизации",
	MsgAuthInvalidSession: "недействительная сессия",
	MsgAuthAdminRequired:  "требуется доступ администратора",

	// Authentication.
	MsgAuthInvalidCredentials: "неверные учётные данные",

	// Account management.
	MsgAccountAdminStatusImmutable: "статус администратора нельзя изменить после создания учётной записи",
	MsgAccountListError:            "получить список учётных записей",
	MsgAccountCheckSessions:        "проверить активные сессии администратора",
	MsgAccountCreateFailed:         "не удалось создать учётную запись",

	// Settings.
	MsgSettingNotFound:     "настройка %q не найдена",
	MsgSettingKeyRequired:  "требуется ключ",
	MsgSettingInvalidBytes: "недопустимое значение байтов для %q: %v",
	MsgSettingsMgrMissing:  "менеджер настроек недоступен",

	// Audit.
	MsgAuditNotConfigured: "журнал аудита не настроен",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "включение/отключение не разрешено",
	MsgUnitCannotStopController:    "невозможно остановить systemcontroller",
	MsgUnitInvalidLines:            "недопустимый параметр lines",
	MsgUnitInvalidSince:            "недопустимый параметр since",
	MsgUnitInvalidUntil:            "недопустимый параметр until",
	MsgUnitInvalidPriority:         "недопустимый параметр priority",

	// Repository management.
	MsgRepoInvalidURL: "недопустимый url",

	// Pages management.
	MsgPagesNotConfigured:    "страницы не настроены",
	MsgPagesGitNotConfigured: "git-клиент или каталог страниц не настроены",

	// Package installation.
	MsgInstallNoRepoRoot:      "не настроен корень репозитория",
	MsgInstallSummaryUpgrade:  "Обновить %s с %s до %s",
	MsgInstallSummaryInstall:  "Установить %s %s",
	MsgInstallSummaryImage:    "Образ: %s",
	MsgInstallSummaryVolumes:  "%d том(ов)",
	MsgInstallSummaryNewVols:  "%d новых",
	MsgInstallSummaryMigrated: "%d перенесено",
	MsgInstallSummaryNoVols:   "Нет томов",
	MsgInstallSummaryPorts:    "Внешние порты: %s",
	MsgInstallSummaryConfig:   "Требуется настройка",
	MsgInstallSummaryVMImage:  "Образ VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "требуются repo, name и version",
	MsgManifestNotFound:       "манифест пакета не найден: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "требуются repo, name и version",
	MsgRebuildRepoNotConfigured: "корень репозитория не настроен",
	MsgRebuildGitNotConfigured:  "git-клиент не настроен",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "требуется поле subvolume",
	MsgArchiveFileRequired:      "требуется файл архива: %v",
	MsgArchiveUnsupportedFormat: "неподдерживаемый формат загрузки: %s",
	MsgArchiveUnpackSuccess:     "архив успешно распакован",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "каталог страниц не настроен",
	MsgPagesNameRequired:           "требуется поле name",
	MsgPagesUploadArchiveOnly:      "загрузка разрешена только для страниц типа archive",
	MsgPagesArchiveRebuildRequired: "страницы типа archive нужно пересобирать загрузкой нового архива через /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "мониторинг не настроен",

	// Upgrades.
	MsgUpgradeSettingsMissing: "менеджер настроек недоступен",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "создать файловую систему",
	MsgAuditModifyFilesystem:         "изменить файловую систему",
	MsgAuditRemoveFilesystem:         "удалить файловую систему",
	MsgAuditAddRepository:            "добавить репозиторий",
	MsgAuditRemoveRepository:         "удалить репозиторий",
	MsgAuditMoveRepository:           "переместить репозиторий",
	MsgAuditRefreshRepositories:      "обновить репозитории",
	MsgAuditInstallPackage:           "установить пакет",
	MsgAuditUninstallPackage:         "удалить пакет",
	MsgAuditPurgeUninstalledVolumes:  "очистить тома удалённых пакетов",
	MsgAuditPurgeVolumes:             "очистить тома",
	MsgAuditDisablePackage:           "отключить пакет",
	MsgAuditEnablePackage:            "включить пакет",
	MsgAuditSetUnitStatus:            "изменить статус юнита",
	MsgAuditCreateAccount:            "создать учётную запись",
	MsgAuditUpdateAccount:            "обновить учётную запись",
	MsgAuditDisableAccount:           "отключить учётную запись",
	MsgAuditAuthenticate:             "аутентификация",
	MsgAuditRevokeSession:            "отозвать сессию",
	MsgAuditUpdateSetting:            "обновить настройку",
	MsgAuditDismissUpgrades:          "скрыть обновления пакетов",
	MsgAuditUploadArchive:            "загрузить архив",
	MsgAuditDownloadArchive:          "скачать архив",
	MsgAuditCreatePage:               "создать страницу",
	MsgAuditUpdatePage:               "обновить страницу",
	MsgAuditRemovePage:               "удалить страницу",
	MsgAuditRebuildPage:              "пересобрать страницу",
	MsgAuditUploadPageArchive:        "загрузить архив страницы",
	MsgAuditEnableAccount:            "включить учётную запись",
	MsgAuditRebuildGit:               "пересобрать git",
	MsgAuditUploadVMImage:            "загрузить образ vm",
	MsgAuditDeleteVMImage:            "удалить образ vm",
	MsgAuditAddDNSRecord:             "добавить запись dns",
	MsgAuditRemoveDNSRecord:          "удалить запись dns",
	MsgAuditSetDNSTLD:                "задать dns tld",
	MsgAuditSetupDNS:                 "настроить dns",
	MsgAuditRemovePackageVolume:      "удалить том пакета",
	MsgAuditRemovePackageVolumeGroup: "удалить группу томов пакета",
	MsgAuditClearLastResponses:       "очистить кэшированные ответы установки",
	MsgAuditSetSystemServiceStatus:   "изменить статус системной службы",
	MsgAuditRefreshSystemServices:    "обновить системные службы",
	MsgAuditCreateNetwork:            "создать сеть",
	MsgAuditRemoveNetwork:            "удалить сеть",
	MsgAuditEnableNetwork:            "включить сеть",
	MsgAuditDisableNetwork:           "отключить сеть",
	MsgAuditAddNetworkPeer:           "добавить узел сети",
	MsgAuditRemoveNetworkPeer:        "удалить узел сети",
	MsgAuditRefreshNetworkPeer:       "обновить узел сети",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "эта учётная запись может использовать только конечные точки регистрации в сети и объектного хранилища",
	MsgAuthNetworkOnlyNetworkDenied: "этой учётной записи не разрешён доступ к этой сети",
	MsgAuthWireGuardPeerNotOwned:    "эта учётная запись может обновлять только зарегистрированные ею узлы",
	MsgAuthSessionNotOwned:          "эта учётная запись может отозвать только свои сеансы",
	MsgAuthObjectStorageRequired:    "требуется доступ администратора или к объектному хранилищу",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "загрузка и выгрузка архивов не может адресовать раздел объектного хранилища",
	MsgGfehNotConfigured:         "объектное хранилище не настроено",
	MsgGfehNameRequired:          "поле имени обязательно",
	MsgGfehPartitionExists:       "раздел уже существует",
	MsgGfehPartitionNotFound:     "раздел не найден",
	MsgGfehNetworkRequired:       "поле сети обязательно",
	MsgGfehPrincipalRequired:     "поле субъекта обязательно",
	MsgGfehPathRequired:          "поле пути обязательно",
	MsgGfehUnknownAccount:        "такой учётной записи нет",
	MsgAuditCreateGfehPartition:  "создать раздел объектного хранилища",
	MsgAuditModifyGfehPartition:  "изменить раздел объектного хранилища",
	MsgAuditRemoveGfehPartition:  "удалить раздел объектного хранилища",
	MsgAuditAddGfehPrincipal:     "добавить пользователя объектного хранилища",
	MsgAuditRemoveGfehPrincipal:  "удалить пользователя объектного хранилища",
	MsgAuditAddGfehGrant:         "добавить право объектного хранилища",
	MsgAuditRevokeGfehGrant:      "отозвать право объектного хранилища",
	MsgAuditWithdrawGfehExposure: "отозвать ссылку объектного хранилища",
}

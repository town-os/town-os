package i18n

// zhTWMessages contains all Traditional Chinese translations.
var zhTWMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "缺少授權權杖",
	MsgAuthInvalidSession: "工作階段無效",
	MsgAuthAdminRequired:  "需要管理員權限",

	// Authentication.
	MsgAuthInvalidCredentials: "認證資訊無效",
	MsgAuthNotConfigured:      "尚未設定驗證",

	// Account management.
	MsgAccountAdminStatusImmutable: "帳號建立後無法變更管理員狀態",
	MsgAccountListError:            "列出帳號",
	MsgAccountCheckSessions:        "檢查作用中的管理員工作階段",
	MsgAccountCreateFailed:         "建立帳號失敗",

	// Settings.
	MsgSettingNotFound:     "找不到設定 %q",
	MsgSettingKeyRequired:  "需要提供索引鍵",
	MsgSettingInvalidBytes: "%q 的位元組值無效：%v",
	MsgSettingsMgrMissing:  "設定管理員無法使用",

	// Audit.
	MsgAuditNotConfigured: "尚未設定稽核記錄",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "不允許啟用／停用",
	MsgUnitCannotStopController:    "無法停止 systemcontroller",
	MsgUnitInvalidLines:            "lines 參數無效",
	MsgUnitInvalidSince:            "since 參數無效",
	MsgUnitInvalidUntil:            "until 參數無效",
	MsgUnitInvalidPriority:         "priority 參數無效",

	// Repository management.
	MsgRepoInvalidURL: "URL 無效",

	// Pages management.
	MsgPagesNotConfigured:    "尚未設定 Pages",
	MsgPagesGitNotConfigured: "尚未設定 git 用戶端或 Pages 目錄",

	// Package installation.
	MsgInstallNoRepoRoot:      "尚未設定套件庫根目錄",
	MsgInstallSummaryUpgrade:  "將 %s 從 %s 升級至 %s",
	MsgInstallSummaryInstall:  "安裝 %s %s",
	MsgInstallSummaryImage:    "映像檔：%s",
	MsgInstallSummaryVolumes:  "%d 個 Volume",
	MsgInstallSummaryNewVols:  "%d 個新增",
	MsgInstallSummaryMigrated: "%d 個已移轉",
	MsgInstallSummaryNoVols:   "無 Volume",
	MsgInstallSummaryPorts:    "外部連接埠：%s",
	MsgInstallSummaryConfig:   "需要進行設定",
	MsgInstallSummaryVMImage:  "VM 映像檔：%s",

	// Package manifest.
	MsgManifestFieldsRequired: "需要提供 repo、name 與 version",
	MsgManifestNotFound:       "找不到套件資訊清單：%s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "需要提供 repo、name 與 version",
	MsgRebuildRepoNotConfigured: "尚未設定套件庫根目錄",
	MsgRebuildGitNotConfigured:  "尚未設定 git 用戶端",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "需要提供 subvolume 欄位",
	MsgArchiveFileRequired:      "需要提供封存檔案：%v",
	MsgArchiveUnsupportedFormat: "不支援的下載格式：%s",
	MsgArchiveUnpackSuccess:     "封存檔已成功解開",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "尚未設定 Pages 目錄",
	MsgPagesNameRequired:           "需要提供 name 欄位",
	MsgPagesUploadArchiveOnly:      "只有封存類型的頁面才允許上傳",
	MsgPagesArchiveRebuildRequired: "封存類型的頁面必須透過 /pages/upload 上傳新封存檔以重建",

	// Monitoring.
	MsgMonitoringNotConfigured: "尚未設定監控",

	// Upgrades.
	MsgUpgradeSettingsMissing: "設定管理員無法使用",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "建立檔案系統",
	MsgAuditModifyFilesystem:         "修改檔案系統",
	MsgAuditRemoveFilesystem:         "移除檔案系統",
	MsgAuditAddRepository:            "新增套件庫",
	MsgAuditRemoveRepository:         "移除套件庫",
	MsgAuditMoveRepository:           "移動套件庫",
	MsgAuditRefreshRepositories:      "重新整理套件庫",
	MsgAuditInstallPackage:           "安裝套件",
	MsgAuditUninstallPackage:         "解除安裝套件",
	MsgAuditPurgeUninstalledVolumes:  "清除已解除安裝的 Volume",
	MsgAuditPurgeVolumes:             "清除 Volume",
	MsgAuditDisablePackage:           "停用套件",
	MsgAuditEnablePackage:            "啟用套件",
	MsgAuditSetUnitStatus:            "設定單元狀態",
	MsgAuditCreateAccount:            "建立帳號",
	MsgAuditUpdateAccount:            "更新帳號",
	MsgAuditDisableAccount:           "停用帳號",
	MsgAuditAuthenticate:             "驗證",
	MsgAuditRevokeSession:            "撤銷工作階段",
	MsgAuditUpdateSetting:            "更新設定",
	MsgAuditDismissUpgrades:          "略過套件升級",
	MsgAuditUploadArchive:            "上傳封存檔",
	MsgAuditDownloadArchive:          "下載封存檔",
	MsgAuditCreatePage:               "建立頁面",
	MsgAuditUpdatePage:               "更新頁面",
	MsgAuditRemovePage:               "移除頁面",
	MsgAuditRebuildPage:              "重建頁面",
	MsgAuditUploadPageArchive:        "上傳頁面封存檔",
	MsgAuditEnableAccount:            "啟用帳號",
	MsgAuditRebuildGit:               "重建 git",
	MsgAuditUploadVMImage:            "上傳 VM 映像檔",
	MsgAuditDeleteVMImage:            "刪除 VM 映像檔",
	MsgAuditAddDNSRecord:             "新增 DNS 記錄",
	MsgAuditRemoveDNSRecord:          "移除 DNS 記錄",
	MsgAuditSetDNSTLD:                "設定 DNS TLD",
	MsgAuditSetupDNS:                 "設定 DNS",
	MsgAuditRemovePackageVolume:      "移除套件 Volume",
	MsgAuditRemovePackageVolumeGroup: "移除套件 Volume 群組",
	MsgAuditClearLastResponses:       "清除已快取的安裝回覆",
	MsgAuditSetSystemServiceStatus:   "設定系統服務狀態",
	MsgAuditRefreshSystemServices:    "重新整理系統服務",
	MsgAuditCreateNetwork:            "建立網路",
	MsgAuditRemoveNetwork:            "移除網路",
	MsgAuditEnableNetwork:            "啟用網路",
	MsgAuditDisableNetwork:           "停用網路",
	MsgAuditAddNetworkPeer:           "新增網路對等點",
	MsgAuditRemoveNetworkPeer:        "移除網路對等點",
	MsgAuditRefreshNetworkPeer:       "重新整理網路對等點",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "此帳號僅能使用網路註冊與物件儲存端點",
	MsgAuthNetworkOnlyNetworkDenied: "此帳號未獲授權使用該網路",
	MsgAuthWireGuardPeerNotOwned:    "此帳號僅能重新整理自己註冊的對等點",
	MsgAuthSessionNotOwned:          "此帳戶只能撤銷自己的工作階段",
	MsgAuthObjectStorageRequired:    "需要管理員或物件儲存權限",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "封存上傳與下載無法指向物件儲存分割區",
	MsgGfehNotConfigured:         "物件儲存尚未設定",
	MsgGfehNameRequired:          "名稱欄位為必填",
	MsgGfehPartitionExists:       "分割區已存在",
	MsgGfehPartitionNotFound:     "找不到分割區",
	MsgGfehNetworkRequired:       "網路欄位為必填",
	MsgGfehPrincipalRequired:     "主體欄位為必填",
	MsgGfehPathRequired:          "路徑欄位為必填",
	MsgGfehUnknownAccount:        "沒有這個帳戶",
	MsgAuditCreateGfehPartition:  "建立物件儲存分割區",
	MsgAuditModifyGfehPartition:  "修改物件儲存分割區",
	MsgAuditRemoveGfehPartition:  "移除物件儲存分割區",
	MsgAuditAddGfehPrincipal:     "新增物件儲存使用者",
	MsgAuditRemoveGfehPrincipal:  "移除物件儲存使用者",
	MsgAuditAddGfehGrant:         "新增物件儲存授權",
	MsgAuditRevokeGfehGrant:      "撤銷物件儲存授權",
	MsgAuditWithdrawGfehExposure: "撤回物件儲存連結",
}

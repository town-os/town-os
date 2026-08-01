package i18n

// zhCNMessages contains all Simplified Chinese translations.
var zhCNMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "缺少授权令牌",
	MsgAuthInvalidSession: "会话无效",
	MsgAuthAdminRequired:  "需要管理员权限",

	// Authentication.
	MsgAuthInvalidCredentials: "凭据无效",

	// Account management.
	MsgAccountAdminStatusImmutable: "账户创建后无法更改管理员状态",
	MsgAccountListError:            "列出账户",
	MsgAccountCheckSessions:        "检查活动的管理员会话",
	MsgAccountCreateFailed:         "账户创建失败",

	// Settings.
	MsgSettingNotFound:     "未找到设置 %q",
	MsgSettingKeyRequired:  "键为必填项",
	MsgSettingInvalidBytes: "%q 的字节值无效：%v",
	MsgSettingsMgrMissing:  "设置管理器不可用",

	// Audit.
	MsgAuditNotConfigured: "未配置审计日志",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "不允许启用/禁用",
	MsgUnitCannotStopController:    "无法停止 systemcontroller",
	MsgUnitInvalidLines:            "lines 参数无效",
	MsgUnitInvalidSince:            "since 参数无效",
	MsgUnitInvalidUntil:            "until 参数无效",
	MsgUnitInvalidPriority:         "priority 参数无效",

	// Repository management.
	MsgRepoInvalidURL: "URL 无效",

	// Pages management.
	MsgPagesNotConfigured:    "未配置 Pages",
	MsgPagesGitNotConfigured: "未配置 git 客户端或 Pages 目录",

	// Package installation.
	MsgInstallNoRepoRoot:      "未配置仓库根目录",
	MsgInstallSummaryUpgrade:  "将 %s 从 %s 升级到 %s",
	MsgInstallSummaryInstall:  "安装 %s %s",
	MsgInstallSummaryImage:    "镜像：%s",
	MsgInstallSummaryVolumes:  "%d 个卷",
	MsgInstallSummaryNewVols:  "%d 个新建",
	MsgInstallSummaryMigrated: "%d 个已迁移",
	MsgInstallSummaryNoVols:   "无卷",
	MsgInstallSummaryPorts:    "外部端口：%s",
	MsgInstallSummaryConfig:   "需要配置",
	MsgInstallSummaryVMImage:  "VM 镜像：%s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo、name 和 version 为必填项",
	MsgManifestNotFound:       "未找到软件包清单：%s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repo、name 和 version 为必填项",
	MsgRebuildRepoNotConfigured: "未配置仓库根目录",
	MsgRebuildGitNotConfigured:  "未配置 git 客户端",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume 字段为必填项",
	MsgArchiveFileRequired:      "需要归档文件：%v",
	MsgArchiveUnsupportedFormat: "不支持的下载格式：%s",
	MsgArchiveUnpackSuccess:     "归档解压成功",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "未配置 Pages 目录",
	MsgPagesNameRequired:           "name 字段为必填项",
	MsgPagesUploadArchiveOnly:      "仅允许为归档类型的页面上传",
	MsgPagesArchiveRebuildRequired: "归档页面必须通过 /pages/upload 上传新的归档来重建",

	// Monitoring.
	MsgMonitoringNotConfigured: "未配置监控",

	// Upgrades.
	MsgUpgradeSettingsMissing: "设置管理器不可用",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "创建文件系统",
	MsgAuditModifyFilesystem:        "修改文件系统",
	MsgAuditRemoveFilesystem:        "删除文件系统",
	MsgAuditAddRepository:           "添加仓库",
	MsgAuditRemoveRepository:        "删除仓库",
	MsgAuditMoveRepository:          "移动仓库",
	MsgAuditRefreshRepositories:     "刷新仓库",
	MsgAuditInstallPackage:          "安装软件包",
	MsgAuditUninstallPackage:        "卸载软件包",
	MsgAuditPurgeUninstalledVolumes: "清除已卸载的卷",
	MsgAuditPurgeVolumes:            "清除卷",
	MsgAuditDisablePackage:          "禁用软件包",
	MsgAuditEnablePackage:           "启用软件包",
	MsgAuditSetUnitStatus:           "设置单元状态",
	MsgAuditCreateAccount:           "创建账户",
	MsgAuditUpdateAccount:           "更新账户",
	MsgAuditDisableAccount:          "禁用账户",
	MsgAuditAuthenticate:            "身份验证",
	MsgAuditRevokeSession:           "吊销会话",
	MsgAuditUpdateSetting:           "更新设置",
	MsgAuditDismissUpgrades:         "忽略软件包升级",
	MsgAuditUploadArchive:           "上传归档",
	MsgAuditDownloadArchive:         "下载归档",
	MsgAuditCreatePage:              "创建页面",
	MsgAuditUpdatePage:              "更新页面",
	MsgAuditRemovePage:              "删除页面",
	MsgAuditRebuildPage:             "重建页面",
	MsgAuditUploadPageArchive:       "上传页面归档",
	MsgAuditEnableAccount:           "启用账户",
	MsgAuditRebuildGit:              "重建 git",
	MsgAuditUploadVMImage:           "上传 VM 镜像",
	MsgAuditDeleteVMImage:           "删除 VM 镜像",
	MsgAuditAddDNSRecord:            "添加 DNS 记录",
	MsgAuditRemoveDNSRecord:         "删除 DNS 记录",
	MsgAuditSetDNSTLD:               "设置 DNS TLD",
	MsgAuditSetupDNS:                "初始化 DNS",
	MsgAuditRemovePackageVolume:     "删除软件包卷",
	MsgAuditRemovePackageVolumeGroup: "删除软件包卷组",
	MsgAuditClearLastResponses:      "清除缓存的安装应答",
	MsgAuditSetSystemServiceStatus:  "设置系统服务状态",
	MsgAuditRefreshSystemServices:   "刷新系统服务",
	MsgAuditCreateNetwork:           "创建网络",
	MsgAuditRemoveNetwork:           "删除网络",
	MsgAuditEnableNetwork:           "启用网络",
	MsgAuditDisableNetwork:          "禁用网络",
	MsgAuditAddNetworkPeer:          "添加网络对等端",
	MsgAuditRemoveNetworkPeer:       "删除网络对等端",
	MsgAuditRefreshNetworkPeer:      "刷新网络对等端",

	// WireGuard-only account restrictions.
	MsgAuthWireGuardRestricted:    "此账户只能使用 WireGuard 注册端点",
	MsgAuthWireGuardNetworkDenied: "此账户无权访问该网络",
	MsgAuthWireGuardPeerNotOwned:  "此账户只能刷新自己注册的对等端",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "归档上传和下载不能指向对象存储分区",
	MsgGfehNotConfigured:         "对象存储未配置",
	MsgGfehNameRequired:          "名称字段为必填项",
	MsgGfehPartitionExists:       "分区已存在",
	MsgGfehPartitionNotFound:     "未找到分区",
	MsgGfehNetworkRequired:       "网络字段为必填项",
	MsgGfehPrincipalRequired:     "主体字段为必填项",
	MsgGfehPathRequired:          "路径字段为必填项",
	MsgGfehUnknownAccount:        "该账户不存在",
	MsgGfehServiceAccountProtected: "对象存储服务账户不能被禁用；每个分区都以它进行身份验证",
	MsgAuditCreateGfehPartition:  "创建对象存储分区",
	MsgAuditModifyGfehPartition:  "修改对象存储分区",
	MsgAuditRemoveGfehPartition:  "删除对象存储分区",
	MsgAuditAddGfehPrincipal:     "添加对象存储用户",
	MsgAuditRemoveGfehPrincipal:  "移除对象存储用户",
	MsgAuditAddGfehGrant:         "添加对象存储授权",
	MsgAuditRevokeGfehGrant:      "撤销对象存储授权",
	MsgAuditWithdrawGfehExposure: "撤回对象存储链接",
}

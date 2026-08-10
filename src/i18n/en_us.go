package i18n

// enUSMessages contains all English (United States) translations.
var enUSMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "missing authorization token",
	MsgAuthInvalidSession: "invalid session",
	MsgAuthAdminRequired:  "admin access required",

	// Authentication.
	MsgAuthInvalidCredentials: "invalid credentials",
	MsgAuthNotConfigured:      "authentication is not configured",

	// Account management.
	MsgAccountAdminStatusImmutable: "admin status cannot be changed after account creation",
	MsgAccountListError:            "list accounts",
	MsgAccountCheckSessions:        "check active admin sessions",
	MsgAccountCreateFailed:         "account creation failed",

	// Settings.
	MsgSettingNotFound:     "setting %q not found",
	MsgSettingKeyRequired:  "key is required",
	MsgSettingInvalidBytes: "invalid byte value for %q: %v",
	MsgSettingsMgrMissing:  "settings manager not available",

	// Audit.
	MsgAuditNotConfigured: "audit logging not configured",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "enable/disable not allowed",
	MsgUnitCannotStopController:    "cannot stop systemcontroller",
	MsgUnitInvalidLines:            "invalid lines parameter",
	MsgUnitInvalidSince:            "invalid since parameter",
	MsgUnitInvalidUntil:            "invalid until parameter",
	MsgUnitInvalidPriority:         "invalid priority parameter",

	// Repository management.
	MsgRepoInvalidURL: "invalid url",

	// Pages management.
	MsgPagesNotConfigured:    "pages not configured",
	MsgPagesGitNotConfigured: "git client or pages directory not configured",

	// Package installation.
	MsgInstallNoRepoRoot:      "no repository root configured",
	MsgInstallSummaryUpgrade:  "Upgrade %s from %s to %s",
	MsgInstallSummaryInstall:  "Install %s %s",
	MsgInstallSummaryImage:    "Image: %s",
	MsgInstallSummaryVolumes:  "%d volume(s)",
	MsgInstallSummaryNewVols:  "%d new",
	MsgInstallSummaryMigrated: "%d migrated",
	MsgInstallSummaryNoVols:   "No volumes",
	MsgInstallSummaryPorts:    "External ports: %s",
	MsgInstallSummaryConfig:   "Configuration required",
	MsgInstallSummaryVMImage:  "VM Image: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name, and version are required",
	MsgManifestNotFound:       "package manifest not found: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, name, and version are required",
	MsgRebuildRepoNotConfigured: "repository root not configured",
	MsgRebuildGitNotConfigured:  "git client not configured",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume field is required",
	MsgArchiveFileRequired:      "archive file required: %v",
	MsgArchiveUnsupportedFormat: "unsupported download format: %s",
	MsgArchiveUnpackSuccess:     "archive unpacked successfully",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "pages directory not configured",
	MsgPagesNameRequired:           "name field is required",
	MsgPagesUploadArchiveOnly:      "upload is only allowed for archive-type pages",
	MsgPagesArchiveRebuildRequired: "archive pages must be rebuilt by uploading a new archive via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitoring is not configured",

	// Upgrades.
	MsgUpgradeSettingsMissing: "settings manager not available",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "create filesystem",
	MsgAuditModifyFilesystem:         "modify filesystem",
	MsgAuditRemoveFilesystem:         "remove filesystem",
	MsgAuditAddRepository:            "add repository",
	MsgAuditRemoveRepository:         "remove repository",
	MsgAuditMoveRepository:           "move repository",
	MsgAuditRefreshRepositories:      "refresh repositories",
	MsgAuditInstallPackage:           "install package",
	MsgAuditUninstallPackage:         "uninstall package",
	MsgAuditPurgeUninstalledVolumes:  "purge uninstalled volumes",
	MsgAuditPurgeVolumes:             "purge volumes",
	MsgAuditDisablePackage:           "disable package",
	MsgAuditEnablePackage:            "enable package",
	MsgAuditSetUnitStatus:            "set unit status",
	MsgAuditCreateAccount:            "create account",
	MsgAuditUpdateAccount:            "update account",
	MsgAuditDisableAccount:           "disable account",
	MsgAuditAuthenticate:             "authenticate",
	MsgAuditRevokeSession:            "revoke session",
	MsgAuditUpdateSetting:            "update setting",
	MsgAuditDismissUpgrades:          "dismiss package upgrades",
	MsgAuditUploadArchive:            "upload archive",
	MsgAuditDownloadArchive:          "download archive",
	MsgAuditCreatePage:               "create page",
	MsgAuditUpdatePage:               "update page",
	MsgAuditRemovePage:               "remove page",
	MsgAuditRebuildPage:              "rebuild page",
	MsgAuditUploadPageArchive:        "upload page archive",
	MsgAuditEnableAccount:            "enable account",
	MsgAuditRebuildGit:               "rebuild git",
	MsgAuditUploadVMImage:            "upload vm image",
	MsgAuditDeleteVMImage:            "delete vm image",
	MsgAuditAddDNSRecord:             "add dns record",
	MsgAuditRemoveDNSRecord:          "remove dns record",
	MsgAuditSetDNSTLD:                "set dns tld",
	MsgAuditSetupDNS:                 "setup dns",
	MsgAuditRemovePackageVolume:      "remove package volume",
	MsgAuditRemovePackageVolumeGroup: "remove package volume group",
	MsgAuditClearLastResponses:       "clear cached install responses",
	MsgAuditSetSystemServiceStatus:   "set system service status",
	MsgAuditRefreshSystemServices:    "refresh system services",
	MsgAuditCreateNetwork:            "create network",
	MsgAuditRemoveNetwork:            "remove network",
	MsgAuditEnableNetwork:            "enable network",
	MsgAuditDisableNetwork:           "disable network",
	MsgAuditAddNetworkPeer:           "add network peer",
	MsgAuditRemoveNetworkPeer:        "remove network peer",
	MsgAuditRefreshNetworkPeer:       "refresh network peer",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "this account may only use network enrollment and object storage endpoints",
	MsgAuthNetworkOnlyNetworkDenied: "this account is not permitted on that network",
	MsgAuthWireGuardPeerNotOwned:    "this account may only refresh peers it enrolled",
	MsgAuthSessionNotOwned:          "this account may only revoke its own sessions",
	MsgAuthObjectStorageRequired:    "administrator or object storage access required",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "archive uploads and downloads cannot address an object storage partition",
	MsgGfehNotConfigured:         "object storage is not configured",
	MsgGfehNameRequired:          "name field is required",
	MsgGfehPartitionExists:       "partition already exists",
	MsgGfehPartitionNotFound:     "partition not found",
	MsgGfehNetworkRequired:       "network field is required",
	MsgGfehPrincipalRequired:     "principal field is required",
	MsgGfehPathRequired:          "path field is required",
	MsgGfehUnknownAccount:        "no such account",
	MsgAuditCreateGfehPartition:  "create object storage partition",
	MsgAuditModifyGfehPartition:  "modify object storage partition",
	MsgAuditRemoveGfehPartition:  "remove object storage partition",
	MsgAuditAddGfehPrincipal:     "add object storage user",
	MsgAuditRemoveGfehPrincipal:  "remove object storage user",
	MsgAuditAddGfehGrant:         "add object storage grant",
	MsgAuditRevokeGfehGrant:      "revoke object storage grant",
	MsgAuditWithdrawGfehExposure: "withdraw object storage link",
}

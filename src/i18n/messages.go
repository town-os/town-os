package i18n

// Message key constants for all user-facing strings in the Control Plane Service.
// Keys use a dot-separated namespace: "category.subcategory.description".
const (
	// MsgAuthMissingToken indicates a missing authorization token.
	MsgAuthMissingToken = "auth.missing_token" //nolint:gosec // message key, not a credential
	// MsgAuthInvalidSession indicates an invalid or expired session.
	MsgAuthInvalidSession = "auth.invalid_session"
	// MsgAuthAdminRequired indicates admin access is required.
	MsgAuthAdminRequired = "auth.admin_required"

	// MsgAccountAdminStatusImmutable indicates admin status cannot be changed.
	MsgAccountAdminStatusImmutable = "account.admin_status_immutable"
	// MsgAccountListError indicates a failure listing accounts.
	MsgAccountListError = "account.list_error"
	// MsgAccountCheckSessions indicates a failure checking active sessions.
	MsgAccountCheckSessions = "account.check_sessions"

	// MsgSettingNotFound indicates a setting key was not found.
	MsgSettingNotFound = "settings.not_found"
	// MsgSettingKeyRequired indicates a setting key is required.
	MsgSettingKeyRequired = "settings.key_required"
	// MsgSettingInvalidBytes indicates an invalid byte-value setting.
	MsgSettingInvalidBytes = "settings.invalid_bytes"
	// MsgSettingsMgrMissing indicates the settings manager is not configured.
	MsgSettingsMgrMissing = "settings.manager_missing"

	// MsgAuditNotConfigured indicates the audit system is not configured.
	MsgAuditNotConfigured = "audit.not_configured"

	// MsgUnitEnableDisableNotAllowed indicates enable/disable is not allowed for this unit.
	MsgUnitEnableDisableNotAllowed = "unit.enable_disable_not_allowed"
	// MsgUnitCannotStopController indicates the controller unit cannot be stopped.
	MsgUnitCannotStopController = "unit.cannot_stop_controller"
	// MsgUnitInvalidLines indicates an invalid lines parameter.
	MsgUnitInvalidLines = "unit.invalid_lines"
	// MsgUnitInvalidSince indicates an invalid since parameter.
	MsgUnitInvalidSince = "unit.invalid_since"
	// MsgUnitInvalidUntil indicates an invalid until parameter.
	MsgUnitInvalidUntil = "unit.invalid_until"
	// MsgUnitInvalidPriority indicates an invalid priority parameter.
	MsgUnitInvalidPriority = "unit.invalid_priority"

	// MsgRepoInvalidURL indicates the repository URL is invalid.
	MsgRepoInvalidURL = "repository.invalid_url"

	// MsgPagesNotConfigured indicates the pages system is not configured.
	MsgPagesNotConfigured = "pages.not_configured"
	// MsgPagesGitNotConfigured indicates the git client is not configured for pages.
	MsgPagesGitNotConfigured = "pages.git_not_configured"

	// MsgInstallNoRepoRoot indicates the repository root is not configured for installs.
	MsgInstallNoRepoRoot = "install.no_repo_root"
	// MsgInstallSummaryUpgrade is the install summary line for upgrades.
	MsgInstallSummaryUpgrade = "install.summary.upgrade"
	// MsgInstallSummaryInstall is the install summary line for new installs.
	MsgInstallSummaryInstall = "install.summary.install"
	// MsgInstallSummaryImage is the install summary line for the container image.
	MsgInstallSummaryImage = "install.summary.image"
	// MsgInstallSummaryVolumes is the install summary header for volumes.
	MsgInstallSummaryVolumes = "install.summary.volumes"
	// MsgInstallSummaryNewVols is the install summary line for new volumes.
	MsgInstallSummaryNewVols = "install.summary.new_volumes"
	// MsgInstallSummaryMigrated is the install summary line for migrated volumes.
	MsgInstallSummaryMigrated = "install.summary.migrated"
	// MsgInstallSummaryNoVols is the install summary line when there are no volumes.
	MsgInstallSummaryNoVols = "install.summary.no_volumes"
	// MsgInstallSummaryPorts is the install summary line for external ports.
	MsgInstallSummaryPorts = "install.summary.external_ports"
	// MsgInstallSummaryConfig is the install summary line indicating configuration is required.
	MsgInstallSummaryConfig = "install.summary.config_required"

	// MsgRebuildFieldsRequired indicates required fields are missing for a rebuild.
	MsgRebuildFieldsRequired = "rebuild.fields_required"
	// MsgRebuildRepoNotConfigured indicates the repository root is not configured for rebuilds.
	MsgRebuildRepoNotConfigured = "rebuild.repo_not_configured"
	// MsgRebuildGitNotConfigured indicates the git client is not configured for rebuilds.
	MsgRebuildGitNotConfigured = "rebuild.git_not_configured"

	// MsgArchiveSubvolumeRequired indicates a subvolume field is required.
	MsgArchiveSubvolumeRequired = "archive.subvolume_required"
	// MsgArchiveFileRequired indicates an archive file is required.
	MsgArchiveFileRequired = "archive.file_required"
	// MsgArchiveUnsupportedFormat indicates the download format is not supported.
	MsgArchiveUnsupportedFormat = "archive.unsupported_format"
	// MsgArchiveUnpackSuccess indicates the archive was unpacked successfully.
	MsgArchiveUnpackSuccess = "archive.unpack_success"

	// MsgPagesDirNotConfigured indicates the pages directory is not configured.
	MsgPagesDirNotConfigured = "pages.dir_not_configured"
	// MsgPagesNameRequired indicates the name field is required.
	MsgPagesNameRequired = "pages.name_required"
	// MsgPagesUploadArchiveOnly indicates uploads are only allowed for archive-type pages.
	MsgPagesUploadArchiveOnly = "pages.upload_archive_only"
	// MsgPagesArchiveRebuildRequired indicates archive pages must be rebuilt via upload.
	MsgPagesArchiveRebuildRequired = "pages.archive_rebuild_required"

	// MsgMonitoringNotConfigured indicates the monitoring stack is not configured.
	MsgMonitoringNotConfigured = "monitoring.not_configured"
	// MsgMonitoringInvalidGrafanaURL indicates the Grafana URL is invalid.
	MsgMonitoringInvalidGrafanaURL = "monitoring.invalid_grafana_url"

	// MsgUpgradeSettingsMissing indicates the settings manager is missing for upgrades.
	MsgUpgradeSettingsMissing = "upgrade.settings_missing"

	// MsgAuditCreateFilesystem is the audit action for creating a filesystem.
	MsgAuditCreateFilesystem = "audit.action.create_filesystem"
	// MsgAuditModifyFilesystem is the audit action for modifying a filesystem.
	MsgAuditModifyFilesystem = "audit.action.modify_filesystem"
	// MsgAuditRemoveFilesystem is the audit action for removing a filesystem.
	MsgAuditRemoveFilesystem = "audit.action.remove_filesystem"
	// MsgAuditAddRepository is the audit action for adding a repository.
	MsgAuditAddRepository = "audit.action.add_repository"
	// MsgAuditRemoveRepository is the audit action for removing a repository.
	MsgAuditRemoveRepository = "audit.action.remove_repository"
	// MsgAuditMoveRepository is the audit action for reordering a repository.
	MsgAuditMoveRepository = "audit.action.move_repository"
	// MsgAuditRefreshRepositories is the audit action for refreshing repositories.
	MsgAuditRefreshRepositories = "audit.action.refresh_repositories"
	// MsgAuditInstallPackage is the audit action for installing a package.
	MsgAuditInstallPackage = "audit.action.install_package"
	// MsgAuditUninstallPackage is the audit action for uninstalling a package.
	MsgAuditUninstallPackage = "audit.action.uninstall_package"
	// MsgAuditPurgeUninstalledVolumes is the audit action for purging uninstalled volumes.
	MsgAuditPurgeUninstalledVolumes = "audit.action.purge_uninstalled_volumes"
	// MsgAuditPurgeVolumes is the audit action for purging volumes.
	MsgAuditPurgeVolumes = "audit.action.purge_volumes"
	// MsgAuditDisablePackage is the audit action for disabling a package.
	MsgAuditDisablePackage = "audit.action.disable_package"
	// MsgAuditEnablePackage is the audit action for enabling a package.
	MsgAuditEnablePackage = "audit.action.enable_package"
	// MsgAuditSetUnitStatus is the audit action for setting a unit's status.
	MsgAuditSetUnitStatus = "audit.action.set_unit_status"
	// MsgAuditCreateAccount is the audit action for creating an account.
	MsgAuditCreateAccount = "audit.action.create_account"
	// MsgAuditUpdateAccount is the audit action for updating an account.
	MsgAuditUpdateAccount = "audit.action.update_account"
	// MsgAuditDisableAccount is the audit action for disabling an account.
	MsgAuditDisableAccount = "audit.action.disable_account"
	// MsgAuditAuthenticate is the audit action for authentication.
	MsgAuditAuthenticate = "audit.action.authenticate"
	// MsgAuditRevokeSession is the audit action for revoking a session.
	MsgAuditRevokeSession = "audit.action.revoke_session"
	// MsgAuditUpdateSetting is the audit action for updating a setting.
	MsgAuditUpdateSetting = "audit.action.update_setting"
	// MsgAuditDismissUpgrades is the audit action for dismissing upgrades.
	MsgAuditDismissUpgrades = "audit.action.dismiss_upgrades"
	// MsgAuditUploadArchive is the audit action for uploading an archive.
	MsgAuditUploadArchive = "audit.action.upload_archive"
	// MsgAuditDownloadArchive is the audit action for downloading an archive.
	MsgAuditDownloadArchive = "audit.action.download_archive"
	// MsgAuditCreatePage is the audit action for creating a page.
	MsgAuditCreatePage = "audit.action.create_page"
	// MsgAuditUpdatePage is the audit action for updating a page.
	MsgAuditUpdatePage = "audit.action.update_page"
	// MsgAuditRemovePage is the audit action for removing a page.
	MsgAuditRemovePage = "audit.action.remove_page"
	// MsgAuditRebuildPage is the audit action for rebuilding a page.
	MsgAuditRebuildPage = "audit.action.rebuild_page"
	// MsgAuditUploadPageArchive is the audit action for uploading a page archive.
	MsgAuditUploadPageArchive = "audit.action.upload_page_archive"
	// MsgAuditEnableAccount is the audit action for enabling an account.
	MsgAuditEnableAccount = "audit.action.enable_account"
	// MsgAuditRebuildGit is the audit action for rebuilding from git.
	MsgAuditRebuildGit = "audit.action.rebuild_git"
)

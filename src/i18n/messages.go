package i18n

// Message key constants for all user-facing strings in the Control Plane Service.
// Keys use a dot-separated namespace: "category.subcategory.description".
const (
	// MsgAuthMissingToken indicates a missing authorization token.
	MsgAuthMissingToken = "auth.missing_token" //nolint:gosec // G101 -- not a credential
	// MsgAuthInvalidSession indicates an invalid or expired session.
	MsgAuthInvalidSession = "auth.invalid_session"
	// MsgAuthAdminRequired indicates admin access is required.
	MsgAuthAdminRequired = "auth.admin_required"

	// MsgAuthInvalidCredentials is a generic authentication failure message
	// that does not reveal whether the account exists.
	MsgAuthInvalidCredentials = "auth.invalid_credentials" //nolint:gosec // G101 -- message key, not a credential

	// MsgAccountAdminStatusImmutable indicates admin status cannot be changed.
	MsgAccountAdminStatusImmutable = "account.admin_status_immutable"
	// MsgAccountListError indicates a failure listing accounts.
	MsgAccountListError = "account.list_error"
	// MsgAccountCheckSessions indicates a failure checking active sessions.
	MsgAccountCheckSessions = "account.check_sessions"
	// MsgAccountCreateFailed is a generic account creation failure message
	// that does not reveal whether the username is taken.
	MsgAccountCreateFailed = "account.create_failed"

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
	// MsgInstallSummaryVMImage is the install summary line for a VM disk image.
	MsgInstallSummaryVMImage = "install.summary.vm_image"

	// MsgManifestFieldsRequired indicates required fields are missing for a manifest request.
	MsgManifestFieldsRequired = "manifest.fields_required"
	// MsgManifestNotFound indicates the requested package manifest was not found.
	MsgManifestNotFound = "manifest.not_found"

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
	// MsgArchiveGfehRefused indicates the archive endpoints will not address a
	// gfeh partition: unpacking a tar into one would create files gfeh's index
	// has never seen.
	MsgArchiveGfehRefused = "archive.gfeh_refused"

	// MsgGfehNotConfigured indicates no gfeh partition serves the request.
	MsgGfehNotConfigured = "gfeh.not_configured"
	// MsgGfehNameRequired indicates the partition name field is required.
	MsgGfehNameRequired = "gfeh.name_required"
	// MsgGfehPartitionExists indicates the partition already exists.
	MsgGfehPartitionExists = "gfeh.partition_exists"
	// MsgGfehPartitionNotFound indicates the named partition does not exist.
	MsgGfehPartitionNotFound = "gfeh.partition_not_found"
	// MsgGfehNetworkRequired indicates the network field is required.
	MsgGfehNetworkRequired = "gfeh.network_required"
	// MsgGfehPrincipalRequired indicates the principal field is required.
	MsgGfehPrincipalRequired = "gfeh.principal_required"
	// MsgGfehPathRequired indicates the grant path field is required.
	MsgGfehPathRequired = "gfeh.path_required"
	// MsgGfehUnknownAccount indicates the named Town OS account does not exist.
	MsgGfehUnknownAccount = "gfeh.unknown_account"

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
	// MsgAuditUploadVMImage is the audit action for uploading a VM image.
	MsgAuditUploadVMImage = "audit.action.upload_vm_image"
	// MsgAuditDeleteVMImage is the audit action for deleting a VM image.
	MsgAuditDeleteVMImage = "audit.action.delete_vm_image"
	// MsgAuditAddDNSRecord is the audit action for adding a DNS record.
	MsgAuditAddDNSRecord = "audit.action.add_dns_record"
	// MsgAuditRemoveDNSRecord is the audit action for removing a DNS record.
	MsgAuditRemoveDNSRecord = "audit.action.remove_dns_record"
	// MsgAuditSetDNSTLD is the audit action for setting the DNS TLD.
	MsgAuditSetDNSTLD = "audit.action.set_dns_tld"
	// MsgAuditSetupDNS is the audit action for setting up DNS.
	MsgAuditSetupDNS = "audit.action.setup_dns"
	// MsgAuditRemovePackageVolume is the audit action for removing a single
	// package volume via /storage/remove-package-volume.
	MsgAuditRemovePackageVolume = "audit.action.remove_package_volume"
	// MsgAuditRemovePackageVolumeGroup is the audit action for cascading
	// removal of a whole package's (or a single version's) volumes via
	// /storage/remove-package-volume-group. The handler also stops every
	// systemd unit in the package's dependency tree before deleting.
	MsgAuditRemovePackageVolumeGroup = "audit.action.remove_package_volume_group"
	// MsgAuditClearLastResponses is the audit action for clearing the
	// cached last-install responses for a package via
	// /packages/clear-last-responses.
	MsgAuditClearLastResponses = "audit.action.clear_last_responses"
	// MsgAuditSetSystemServiceStatus is the audit action for changing a
	// system service's runtime state via /system-services/status.
	MsgAuditSetSystemServiceStatus = "audit.action.set_system_service_status"
	// MsgAuditRefreshSystemServices is the audit action for refreshing
	// system services via /system-services/refresh.
	MsgAuditRefreshSystemServices = "audit.action.refresh_system_services"

	// MsgAuditCreateNetwork is the audit action for creating an overlay network.
	MsgAuditCreateNetwork = "audit.action.create_network"
	// MsgAuditRemoveNetwork is the audit action for removing an overlay network.
	MsgAuditRemoveNetwork = "audit.action.remove_network"
	// MsgAuditEnableNetwork is the audit action for enabling a network's overlay.
	MsgAuditEnableNetwork = "audit.action.enable_network"
	// MsgAuditDisableNetwork is the audit action for disabling a network's overlay.
	MsgAuditDisableNetwork = "audit.action.disable_network"
	// MsgAuditAddNetworkPeer is the audit action for enrolling a peer on a network.
	MsgAuditAddNetworkPeer = "audit.action.add_network_peer"
	// MsgAuditRemoveNetworkPeer is the audit action for removing a peer.
	MsgAuditRemoveNetworkPeer = "audit.action.remove_network_peer"
	// MsgAuditRefreshNetworkPeer is the audit action for refreshing a peer's TTL.
	MsgAuditRefreshNetworkPeer = "audit.action.refresh_network_peer"

	// MsgAuditCreateGfehPartition is the audit action for creating a partition.
	MsgAuditCreateGfehPartition = "audit.action.create_gfeh_partition"
	// MsgAuditModifyGfehPartition is the audit action for resizing a partition.
	MsgAuditModifyGfehPartition = "audit.action.modify_gfeh_partition"
	// MsgAuditRemoveGfehPartition is the audit action for removing a partition.
	MsgAuditRemoveGfehPartition = "audit.action.remove_gfeh_partition"
	// MsgAuditAddGfehPrincipal is the audit action for adding a partition user.
	MsgAuditAddGfehPrincipal = "audit.action.add_gfeh_principal"
	// MsgAuditRemoveGfehPrincipal is the audit action for removing a partition user.
	MsgAuditRemoveGfehPrincipal = "audit.action.remove_gfeh_principal"
	// MsgAuditAddGfehGrant is the audit action for granting a path.
	MsgAuditAddGfehGrant = "audit.action.add_gfeh_grant"
	// MsgAuditRevokeGfehGrant is the audit action for revoking a grant.
	MsgAuditRevokeGfehGrant = "audit.action.revoke_gfeh_grant"
	// MsgAuditWithdrawGfehExposure is the audit action for withdrawing a published link.
	MsgAuditWithdrawGfehExposure = "audit.action.withdraw_gfeh_exposure"

	// MsgAuthNetworkOnlyRestricted indicates a network-only account tried to
	// reach an endpoint outside its allowlist.
	MsgAuthNetworkOnlyRestricted = "auth.network_only_restricted"
	// MsgAuthNetworkOnlyNetworkDenied indicates a network-only account tried to
	// act on a network outside its permitted scope — enroll a peer on it, or
	// administer the object storage it owns.
	MsgAuthNetworkOnlyNetworkDenied = "auth.network_only_network_denied"
	// MsgAuthWireGuardPeerNotOwned indicates a network-only account tried to
	// refresh a peer it did not enroll.
	MsgAuthWireGuardPeerNotOwned = "auth.wireguard_peer_not_owned"

	// MsgAuthObjectStorageRequired indicates an account that is neither an
	// administrator nor network-only tried to change a partition's users,
	// grants, or published links. Distinct from MsgAuthAdminRequired because
	// the answer to "what do I need" is different: not administrator, but
	// either administrator or an account scoped to that network.
	MsgAuthObjectStorageRequired = "auth.object_storage_required"
)

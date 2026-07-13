package account

import (
	"time"

	"gitea.com/town-os/town-os/src/i18n"
)

type AuditEntry struct {
	ID        int64     `json:"id"`
	Account   string    `json:"account"`
	Action    string    `json:"action"`
	Path      string    `json:"path"`
	Detail    string    `json:"detail"`
	Success   bool      `json:"success"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditListOptions struct {
	BeforeID  int64  `json:"before_id"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
	Account   string `json:"account"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
	Search    string `json:"search"`
}

type AuditPage struct {
	Entries    []AuditEntry `json:"entries"`
	HasMore    bool         `json:"has_more"`
	TotalPages int          `json:"total_pages"`
	TotalCount int          `json:"total_count"`
}

type AuditManager interface {
	LogEntry(entry AuditEntry) error
	List(opts AuditListOptions) (*AuditPage, error)
	CountRecentErrors(since time.Time) (int, error)
}

// RouteActionKeys maps API paths to i18n message keys for audit log actions.
var RouteActionKeys = map[string]string{
	"/storage/create":                     i18n.MsgAuditCreateFilesystem,
	"/storage/modify":                     i18n.MsgAuditModifyFilesystem,
	"/storage/remove":                     i18n.MsgAuditRemoveFilesystem,
	"/repository/add":                     i18n.MsgAuditAddRepository,
	"/repository/remove":                  i18n.MsgAuditRemoveRepository,
	"/repository/move":                    i18n.MsgAuditMoveRepository,
	"/repository/refresh":                 i18n.MsgAuditRefreshRepositories,
	"/packages/install":                   i18n.MsgAuditInstallPackage,
	"/packages/uninstall":                 i18n.MsgAuditUninstallPackage,
	"/packages/purge-uninstalled-volumes": i18n.MsgAuditPurgeUninstalledVolumes,
	"/packages/purge-volumes":             i18n.MsgAuditPurgeVolumes,
	"/packages/disable":                   i18n.MsgAuditDisablePackage,
	"/packages/enable":                    i18n.MsgAuditEnablePackage,
	"/systemd/status":                     i18n.MsgAuditSetUnitStatus,
	"/account/create":                     i18n.MsgAuditCreateAccount,
	"/account/update":                     i18n.MsgAuditUpdateAccount,
	"/account/disable":                    i18n.MsgAuditDisableAccount,
	"/account/enable":                     i18n.MsgAuditEnableAccount,
	"/account/authenticate":               i18n.MsgAuditAuthenticate,
	"/account/session/revoke":             i18n.MsgAuditRevokeSession,
	"/settings/set":                       i18n.MsgAuditUpdateSetting,
	"/packages/upgrades/dismiss":          i18n.MsgAuditDismissUpgrades,
	"/storage/upload-archive":             i18n.MsgAuditUploadArchive,
	"/storage/download-archive":           i18n.MsgAuditDownloadArchive,
	"/pages/create":                       i18n.MsgAuditCreatePage,
	"/pages/update":                       i18n.MsgAuditUpdatePage,
	"/pages/remove":                       i18n.MsgAuditRemovePage,
	"/pages/rebuild":                      i18n.MsgAuditRebuildPage,
	"/pages/upload":                       i18n.MsgAuditUploadPageArchive,
	"/packages/rebuild-git":               i18n.MsgAuditRebuildGit,
	"/vm-images/upload":                   i18n.MsgAuditUploadVMImage,
	"/vm-images/delete":                   i18n.MsgAuditDeleteVMImage,
	"/dns/records/add":                     i18n.MsgAuditAddDNSRecord,
	"/dns/records/remove":                  i18n.MsgAuditRemoveDNSRecord,
	"/dns/tld":                             i18n.MsgAuditSetDNSTLD,
	"/dns/setup":                           i18n.MsgAuditSetupDNS,
	"/storage/remove-package-volume":       i18n.MsgAuditRemovePackageVolume,
	"/storage/remove-package-volume-group": i18n.MsgAuditRemovePackageVolumeGroup,
	"/packages/clear-last-responses":       i18n.MsgAuditClearLastResponses,
	"/system-services/status":              i18n.MsgAuditSetSystemServiceStatus,
	"/system-services/refresh":             i18n.MsgAuditRefreshSystemServices,
	"/networks/create":                     i18n.MsgAuditCreateNetwork,
	"/networks/remove":                     i18n.MsgAuditRemoveNetwork,
	"/networks/enable":                     i18n.MsgAuditEnableNetwork,
	"/networks/disable":                    i18n.MsgAuditDisableNetwork,
	"/networks/peers/add":                  i18n.MsgAuditAddNetworkPeer,
	"/networks/peers/remove":               i18n.MsgAuditRemoveNetworkPeer,
	"/networks/peers/refresh":              i18n.MsgAuditRefreshNetworkPeer,
}

// RouteActions maps API paths to their localized audit action descriptions
// using the default locale. This is used for audit log entries.
var RouteActions = func() map[string]string {
	m := make(map[string]string, len(RouteActionKeys))
	for path, key := range RouteActionKeys {
		m[path] = i18n.T(i18n.DefaultLocale, key)
	}
	return m
}()

// LocalizedRouteActions returns a copy of RouteActions with action
// descriptions translated to the given locale.
func LocalizedRouteActions(locale string) map[string]string {
	m := make(map[string]string, len(RouteActionKeys))
	for path, key := range RouteActionKeys {
		m[path] = i18n.T(locale, key)
	}
	return m
}

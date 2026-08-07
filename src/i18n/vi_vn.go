package i18n

// viVNMessages contains all Vietnamese translations.
var viVNMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "thiếu mã thông báo ủy quyền",
	MsgAuthInvalidSession: "phiên không hợp lệ",
	MsgAuthAdminRequired:  "yêu cầu quyền quản trị viên",

	// Authentication.
	MsgAuthInvalidCredentials: "thông tin đăng nhập không hợp lệ",

	// Account management.
	MsgAccountAdminStatusImmutable: "không thể thay đổi trạng thái quản trị viên sau khi tạo tài khoản",
	MsgAccountListError:            "liệt kê tài khoản",
	MsgAccountCheckSessions:        "kiểm tra các phiên quản trị viên đang hoạt động",
	MsgAccountCreateFailed:         "tạo tài khoản thất bại",

	// Settings.
	MsgSettingNotFound:     "không tìm thấy cài đặt %q",
	MsgSettingKeyRequired:  "khóa là bắt buộc",
	MsgSettingInvalidBytes: "giá trị byte không hợp lệ cho %q: %v",
	MsgSettingsMgrMissing:  "trình quản lý cài đặt không khả dụng",

	// Audit.
	MsgAuditNotConfigured: "chưa cấu hình ghi nhật ký kiểm tra",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "không cho phép bật/tắt",
	MsgUnitCannotStopController:    "không thể dừng systemcontroller",
	MsgUnitInvalidLines:            "tham số lines không hợp lệ",
	MsgUnitInvalidSince:            "tham số since không hợp lệ",
	MsgUnitInvalidUntil:            "tham số until không hợp lệ",
	MsgUnitInvalidPriority:         "tham số priority không hợp lệ",

	// Repository management.
	MsgRepoInvalidURL: "url không hợp lệ",

	// Pages management.
	MsgPagesNotConfigured:    "chưa cấu hình pages",
	MsgPagesGitNotConfigured: "chưa cấu hình git client hoặc thư mục pages",

	// Package installation.
	MsgInstallNoRepoRoot:      "chưa cấu hình gốc kho lưu trữ",
	MsgInstallSummaryUpgrade:  "Nâng cấp %s từ %s lên %s",
	MsgInstallSummaryInstall:  "Cài đặt %s %s",
	MsgInstallSummaryImage:    "Ảnh: %s",
	MsgInstallSummaryVolumes:  "%d volume",
	MsgInstallSummaryNewVols:  "%d mới",
	MsgInstallSummaryMigrated: "%d đã di chuyển",
	MsgInstallSummaryNoVols:   "Không có volume",
	MsgInstallSummaryPorts:    "Cổng ngoài: %s",
	MsgInstallSummaryConfig:   "Cần cấu hình",
	MsgInstallSummaryVMImage:  "Ảnh VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name và version là bắt buộc",
	MsgManifestNotFound:       "không tìm thấy manifest gói: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, name và version là bắt buộc",
	MsgRebuildRepoNotConfigured: "chưa cấu hình gốc kho lưu trữ",
	MsgRebuildGitNotConfigured:  "chưa cấu hình git client",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "trường subvolume là bắt buộc",
	MsgArchiveFileRequired:      "cần tệp lưu trữ: %v",
	MsgArchiveUnsupportedFormat: "định dạng tải xuống không được hỗ trợ: %s",
	MsgArchiveUnpackSuccess:     "giải nén tệp lưu trữ thành công",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "chưa cấu hình thư mục pages",
	MsgPagesNameRequired:           "trường name là bắt buộc",
	MsgPagesUploadArchiveOnly:      "chỉ cho phép tải lên đối với trang loại archive",
	MsgPagesArchiveRebuildRequired: "trang archive phải được xây dựng lại bằng cách tải lên tệp lưu trữ mới qua /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "chưa cấu hình giám sát",

	// Upgrades.
	MsgUpgradeSettingsMissing: "trình quản lý cài đặt không khả dụng",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "tạo hệ thống tệp",
	MsgAuditModifyFilesystem:         "sửa đổi hệ thống tệp",
	MsgAuditRemoveFilesystem:         "xóa hệ thống tệp",
	MsgAuditAddRepository:            "thêm kho lưu trữ",
	MsgAuditRemoveRepository:         "xóa kho lưu trữ",
	MsgAuditMoveRepository:           "di chuyển kho lưu trữ",
	MsgAuditRefreshRepositories:      "làm mới kho lưu trữ",
	MsgAuditInstallPackage:           "cài đặt gói",
	MsgAuditUninstallPackage:         "gỡ cài đặt gói",
	MsgAuditPurgeUninstalledVolumes:  "xóa sạch volume đã gỡ cài đặt",
	MsgAuditPurgeVolumes:             "xóa sạch volume",
	MsgAuditDisablePackage:           "vô hiệu hóa gói",
	MsgAuditEnablePackage:            "bật gói",
	MsgAuditSetUnitStatus:            "đặt trạng thái unit",
	MsgAuditCreateAccount:            "tạo tài khoản",
	MsgAuditUpdateAccount:            "cập nhật tài khoản",
	MsgAuditDisableAccount:           "vô hiệu hóa tài khoản",
	MsgAuditAuthenticate:             "xác thực",
	MsgAuditRevokeSession:            "thu hồi phiên",
	MsgAuditUpdateSetting:            "cập nhật cài đặt",
	MsgAuditDismissUpgrades:          "bỏ qua nâng cấp gói",
	MsgAuditUploadArchive:            "tải lên tệp lưu trữ",
	MsgAuditDownloadArchive:          "tải xuống tệp lưu trữ",
	MsgAuditCreatePage:               "tạo trang",
	MsgAuditUpdatePage:               "cập nhật trang",
	MsgAuditRemovePage:               "xóa trang",
	MsgAuditRebuildPage:              "xây dựng lại trang",
	MsgAuditUploadPageArchive:        "tải lên tệp lưu trữ trang",
	MsgAuditEnableAccount:            "bật tài khoản",
	MsgAuditRebuildGit:               "xây dựng lại git",
	MsgAuditUploadVMImage:            "tải lên ảnh vm",
	MsgAuditDeleteVMImage:            "xóa ảnh vm",
	MsgAuditAddDNSRecord:             "thêm bản ghi dns",
	MsgAuditRemoveDNSRecord:          "xóa bản ghi dns",
	MsgAuditSetDNSTLD:                "đặt dns tld",
	MsgAuditSetupDNS:                 "thiết lập dns",
	MsgAuditRemovePackageVolume:      "xóa volume gói",
	MsgAuditRemovePackageVolumeGroup: "xóa nhóm volume gói",
	MsgAuditClearLastResponses:       "xóa các phản hồi cài đặt đã lưu",
	MsgAuditSetSystemServiceStatus:   "đặt trạng thái dịch vụ hệ thống",
	MsgAuditRefreshSystemServices:    "làm mới dịch vụ hệ thống",
	MsgAuditCreateNetwork:            "tạo mạng",
	MsgAuditRemoveNetwork:            "xóa mạng",
	MsgAuditEnableNetwork:            "bật mạng",
	MsgAuditDisableNetwork:           "vô hiệu hóa mạng",
	MsgAuditAddNetworkPeer:           "thêm peer mạng",
	MsgAuditRemoveNetworkPeer:        "xóa peer mạng",
	MsgAuditRefreshNetworkPeer:       "làm mới peer mạng",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "tài khoản này chỉ có thể sử dụng các điểm cuối đăng ký mạng và lưu trữ đối tượng",
	MsgAuthNetworkOnlyNetworkDenied: "tài khoản này không được phép trên mạng đó",
	MsgAuthWireGuardPeerNotOwned:    "tài khoản này chỉ có thể làm mới các peer mà nó đã đăng ký",
	MsgAuthSessionNotOwned:          "tài khoản này chỉ có thể thu hồi phiên của chính nó",
	MsgAuthObjectStorageRequired:    "tài khoản quản trị hoặc quyền lưu trữ đối tượng là bắt buộc",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "tải lên và tải xuống kho lưu trữ không thể nhắm tới một phân vùng lưu trữ đối tượng",
	MsgGfehNotConfigured:         "lưu trữ đối tượng chưa được cấu hình",
	MsgGfehNameRequired:          "trường tên là bắt buộc",
	MsgGfehPartitionExists:       "phân vùng đã tồn tại",
	MsgGfehPartitionNotFound:     "không tìm thấy phân vùng",
	MsgGfehNetworkRequired:       "trường mạng là bắt buộc",
	MsgGfehPrincipalRequired:     "trường chủ thể là bắt buộc",
	MsgGfehPathRequired:          "trường đường dẫn là bắt buộc",
	MsgGfehUnknownAccount:        "không có tài khoản như vậy",
	MsgAuditCreateGfehPartition:  "tạo phân vùng lưu trữ đối tượng",
	MsgAuditModifyGfehPartition:  "sửa phân vùng lưu trữ đối tượng",
	MsgAuditRemoveGfehPartition:  "xóa phân vùng lưu trữ đối tượng",
	MsgAuditAddGfehPrincipal:     "thêm người dùng lưu trữ đối tượng",
	MsgAuditRemoveGfehPrincipal:  "xóa người dùng lưu trữ đối tượng",
	MsgAuditAddGfehGrant:         "thêm quyền lưu trữ đối tượng",
	MsgAuditRevokeGfehGrant:      "thu hồi quyền lưu trữ đối tượng",
	MsgAuditWithdrawGfehExposure: "thu hồi liên kết lưu trữ đối tượng",
}

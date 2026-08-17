package i18n

// koKRMessages contains all Korean translations.
var koKRMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "인증 토큰이 없습니다",
	MsgAuthInvalidSession: "유효하지 않은 세션",
	MsgAuthAdminRequired:  "관리자 권한이 필요합니다",

	// Authentication.
	MsgAuthInvalidCredentials: "인증 정보가 올바르지 않습니다",
	MsgAuthNotConfigured:      "인증이 구성되지 않았습니다",

	// Account management.
	MsgAccountAdminStatusImmutable: "계정 생성 후에는 관리자 상태를 변경할 수 없습니다",
	MsgAccountListError:            "계정 목록 조회",
	MsgAccountCheckSessions:        "활성 관리자 세션 확인",
	MsgAccountCreateFailed:         "계정 생성에 실패했습니다",

	// Settings.
	MsgSettingNotFound:     "설정 %q을(를) 찾을 수 없습니다",
	MsgSettingKeyRequired:  "키가 필요합니다",
	MsgSettingInvalidBytes: "%q에 대한 바이트 값이 올바르지 않습니다: %v",
	MsgSettingsMgrMissing:  "설정 관리자를 사용할 수 없습니다",

	// Audit.
	MsgAuditNotConfigured: "감사 로깅이 구성되지 않았습니다",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "활성화/비활성화가 허용되지 않습니다",
	MsgUnitCannotStopController:    "systemcontroller를 중지할 수 없습니다",
	MsgUnitInvalidLines:            "lines 매개변수가 올바르지 않습니다",
	MsgUnitInvalidSince:            "since 매개변수가 올바르지 않습니다",
	MsgUnitInvalidUntil:            "until 매개변수가 올바르지 않습니다",
	MsgUnitInvalidPriority:         "priority 매개변수가 올바르지 않습니다",

	// Repository management.
	MsgRepoInvalidURL: "URL이 올바르지 않습니다",

	// Pages management.
	MsgPagesNotConfigured:    "페이지가 구성되지 않았습니다",
	MsgPagesGitNotConfigured: "git 클라이언트 또는 페이지 디렉터리가 구성되지 않았습니다",

	// Package installation.
	MsgInstallNoRepoRoot:      "구성된 저장소 루트가 없습니다",
	MsgInstallSummaryUpgrade:  "%s을(를) %s에서 %s(으)로 업그레이드",
	MsgInstallSummaryInstall:  "%s %s 설치",
	MsgInstallSummaryImage:    "이미지: %s",
	MsgInstallSummaryVolumes:  "볼륨 %d개",
	MsgInstallSummaryNewVols:  "신규 %d개",
	MsgInstallSummaryMigrated: "마이그레이션 %d개",
	MsgInstallSummaryNoVols:   "볼륨 없음",
	MsgInstallSummaryPorts:    "외부 포트: %s",
	MsgInstallSummaryConfig:   "구성이 필요합니다",
	MsgInstallSummaryVMImage:  "VM 이미지: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name, version이 필요합니다",
	MsgManifestNotFound:       "패키지 매니페스트를 찾을 수 없습니다: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, name, version이 필요합니다",
	MsgRebuildRepoNotConfigured: "저장소 루트가 구성되지 않았습니다",
	MsgRebuildGitNotConfigured:  "git 클라이언트가 구성되지 않았습니다",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume 필드가 필요합니다",
	MsgArchiveFileRequired:      "아카이브 파일이 필요합니다: %v",
	MsgArchiveUnsupportedFormat: "지원되지 않는 다운로드 형식: %s",
	MsgArchiveUnpackSuccess:     "아카이브 압축을 성공적으로 풀었습니다",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "페이지 디렉터리가 구성되지 않았습니다",
	MsgPagesNameRequired:           "name 필드가 필요합니다",
	MsgPagesUploadArchiveOnly:      "업로드는 아카이브 유형 페이지에만 허용됩니다",
	MsgPagesArchiveRebuildRequired: "아카이브 페이지는 /pages/upload를 통해 새 아카이브를 업로드하여 다시 빌드해야 합니다",

	// Monitoring.
	MsgMonitoringNotConfigured: "모니터링이 구성되지 않았습니다",

	// Upgrades.
	MsgUpgradeSettingsMissing: "설정 관리자를 사용할 수 없습니다",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "파일시스템 생성",
	MsgAuditModifyFilesystem:         "파일시스템 수정",
	MsgAuditRemoveFilesystem:         "파일시스템 삭제",
	MsgAuditAddRepository:            "저장소 추가",
	MsgAuditRemoveRepository:         "저장소 삭제",
	MsgAuditMoveRepository:           "저장소 이동",
	MsgAuditRefreshRepositories:      "저장소 새로고침",
	MsgAuditInstallPackage:           "패키지 설치",
	MsgAuditUninstallPackage:         "패키지 제거",
	MsgAuditPurgeUninstalledVolumes:  "제거된 볼륨 영구 삭제",
	MsgAuditPurgeVolumes:             "볼륨 영구 삭제",
	MsgAuditDisablePackage:           "패키지 비활성화",
	MsgAuditEnablePackage:            "패키지 활성화",
	MsgAuditSetUnitStatus:            "유닛 상태 설정",
	MsgAuditCreateAccount:            "계정 생성",
	MsgAuditUpdateAccount:            "계정 업데이트",
	MsgAuditDisableAccount:           "계정 비활성화",
	MsgAuditAuthenticate:             "인증",
	MsgAuditRevokeSession:            "세션 취소",
	MsgAuditUpdateSetting:            "설정 업데이트",
	MsgAuditDismissUpgrades:          "패키지 업그레이드 무시",
	MsgAuditUploadArchive:            "아카이브 업로드",
	MsgAuditDownloadArchive:          "아카이브 다운로드",
	MsgAuditCreatePage:               "페이지 생성",
	MsgAuditUpdatePage:               "페이지 업데이트",
	MsgAuditRemovePage:               "페이지 삭제",
	MsgAuditRebuildPage:              "페이지 다시 빌드",
	MsgAuditUploadPageArchive:        "페이지 아카이브 업로드",
	MsgAuditEnableAccount:            "계정 활성화",
	MsgAuditRebuildGit:               "git 다시 빌드",
	MsgAuditUploadVMImage:            "VM 이미지 업로드",
	MsgAuditDeleteVMImage:            "VM 이미지 삭제",
	MsgAuditAddDNSRecord:             "DNS 레코드 추가",
	MsgAuditRemoveDNSRecord:          "DNS 레코드 삭제",
	MsgAuditSetDNSTLD:                "DNS TLD 설정",
	MsgAuditSetupDNS:                 "DNS 설정",
	MsgAuditRemovePackageVolume:      "패키지 볼륨 삭제",
	MsgAuditRemovePackageVolumeGroup: "패키지 볼륨 그룹 삭제",
	MsgAuditClearLastResponses:       "캐시된 설치 응답 삭제",
	MsgAuditSetSystemServiceStatus:   "시스템 서비스 상태 설정",
	MsgAuditRefreshSystemServices:    "시스템 서비스 새로고침",
	MsgAuditCreateNetwork:            "네트워크 생성",
	MsgAuditRemoveNetwork:            "네트워크 삭제",
	MsgAuditEnableNetwork:            "네트워크 활성화",
	MsgAuditDisableNetwork:           "네트워크 비활성화",
	MsgAuditAddNetworkPeer:           "네트워크 피어 추가",
	MsgAuditRemoveNetworkPeer:        "네트워크 피어 삭제",
	MsgAuditRefreshNetworkPeer:       "네트워크 피어 새로고침",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "이 계정은 네트워크 등록 및 오브젝트 스토리지 엔드포인트만 사용할 수 있습니다",
	MsgAuthNetworkOnlyNetworkDenied: "이 계정은 해당 네트워크에서 허용되지 않습니다",
	MsgAuthWireGuardPeerNotOwned:    "이 계정은 자신이 등록한 피어만 새로고침할 수 있습니다",
	MsgAuthSessionNotOwned:          "이 계정은 자신의 세션만 해지할 수 있습니다",
	MsgAuthObjectStorageRequired:    "관리자 또는 오브젝트 스토리지 권한이 필요합니다",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "아카이브 업로드와 다운로드는 오브젝트 스토리지 파티션을 대상으로 할 수 없습니다",
	MsgGfehNotConfigured:         "오브젝트 스토리지가 구성되지 않았습니다",
	MsgGfehNameRequired:          "이름 필드는 필수입니다",
	MsgGfehPartitionExists:       "파티션이 이미 존재합니다",
	MsgGfehPartitionNotFound:     "파티션을 찾을 수 없습니다",
	MsgGfehNetworkRequired:       "네트워크 필드는 필수입니다",
	MsgGfehPrincipalRequired:     "주체 필드는 필수입니다",
	MsgGfehPathRequired:          "경로 필드는 필수입니다",
	MsgGfehUnknownAccount:        "해당 계정이 없습니다",
	MsgAuditCreateGfehPartition:  "오브젝트 스토리지 파티션 생성",
	MsgAuditModifyGfehPartition:  "오브젝트 스토리지 파티션 수정",
	MsgAuditRemoveGfehPartition:  "오브젝트 스토리지 파티션 제거",
	MsgAuditAddGfehPrincipal:     "오브젝트 스토리지 사용자 추가",
	MsgAuditRemoveGfehPrincipal:  "오브젝트 스토리지 사용자 제거",
	MsgAuditAddGfehGrant:         "오브젝트 스토리지 권한 추가",
	MsgAuditRevokeGfehGrant:      "오브젝트 스토리지 권한 취소",
	MsgAuditWithdrawGfehExposure: "오브젝트 스토리지 링크 회수",

	// The ingress retry page.
	MsgIngressUnavailableTitle:  "%s을(를) 사용할 수 없습니다",
	MsgIngressUnavailableBody:   "Town OS는 이 주소를 계속 라우팅하고 있지만, 뒤에 있는 서비스가 응답하지 않습니다. 시작 중이거나, 업데이트 후 재시작 중이거나, 잠시 과부하 상태일 가능성이 높습니다.",
	MsgIngressUnavailableRetry:  "할 일은 없습니다. 이 페이지는 %d초마다 다시 시도하고, 서비스가 응답하는 즉시 보여 줍니다.",
	MsgIngressUnavailableFooter: "Town OS 인그레스",
}

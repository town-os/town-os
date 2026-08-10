package i18n

// jaJPMessages contains all Japanese translations.
var jaJPMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "認証トークンがありません",
	MsgAuthInvalidSession: "セッションが無効です",
	MsgAuthAdminRequired:  "管理者権限が必要です",

	// Authentication.
	MsgAuthInvalidCredentials: "認証情報が正しくありません",
	MsgAuthNotConfigured:      "認証が構成されていません",

	// Account management.
	MsgAccountAdminStatusImmutable: "アカウント作成後に管理者ステータスは変更できません",
	MsgAccountListError:            "アカウントの一覧取得",
	MsgAccountCheckSessions:        "有効な管理者セッションの確認",
	MsgAccountCreateFailed:         "アカウントの作成に失敗しました",

	// Settings.
	MsgSettingNotFound:     "設定 %q が見つかりません",
	MsgSettingKeyRequired:  "キーは必須です",
	MsgSettingInvalidBytes: "%q のバイト値が不正です: %v",
	MsgSettingsMgrMissing:  "設定マネージャーが利用できません",

	// Audit.
	MsgAuditNotConfigured: "監査ログが構成されていません",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "有効化／無効化は許可されていません",
	MsgUnitCannotStopController:    "systemcontroller は停止できません",
	MsgUnitInvalidLines:            "lines パラメータが不正です",
	MsgUnitInvalidSince:            "since パラメータが不正です",
	MsgUnitInvalidUntil:            "until パラメータが不正です",
	MsgUnitInvalidPriority:         "priority パラメータが不正です",

	// Repository management.
	MsgRepoInvalidURL: "URL が不正です",

	// Pages management.
	MsgPagesNotConfigured:    "ページが構成されていません",
	MsgPagesGitNotConfigured: "git クライアントまたはページディレクトリが構成されていません",

	// Package installation.
	MsgInstallNoRepoRoot:      "リポジトリルートが構成されていません",
	MsgInstallSummaryUpgrade:  "%s を %s から %s へアップグレード",
	MsgInstallSummaryInstall:  "%s %s をインストール",
	MsgInstallSummaryImage:    "イメージ: %s",
	MsgInstallSummaryVolumes:  "%d 個のボリューム",
	MsgInstallSummaryNewVols:  "%d 個を新規作成",
	MsgInstallSummaryMigrated: "%d 個を移行",
	MsgInstallSummaryNoVols:   "ボリュームなし",
	MsgInstallSummaryPorts:    "外部ポート: %s",
	MsgInstallSummaryConfig:   "設定が必要です",
	MsgInstallSummaryVMImage:  "VM イメージ: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo、name、version は必須です",
	MsgManifestNotFound:       "パッケージマニフェストが見つかりません: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo、name、version は必須です",
	MsgRebuildRepoNotConfigured: "リポジトリルートが構成されていません",
	MsgRebuildGitNotConfigured:  "git クライアントが構成されていません",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume フィールドは必須です",
	MsgArchiveFileRequired:      "アーカイブファイルが必要です: %v",
	MsgArchiveUnsupportedFormat: "サポートされていないダウンロード形式です: %s",
	MsgArchiveUnpackSuccess:     "アーカイブを正常に展開しました",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "ページディレクトリが構成されていません",
	MsgPagesNameRequired:           "name フィールドは必須です",
	MsgPagesUploadArchiveOnly:      "アップロードはアーカイブ形式のページでのみ許可されています",
	MsgPagesArchiveRebuildRequired: "アーカイブページは /pages/upload から新しいアーカイブをアップロードして再構築する必要があります",

	// Monitoring.
	MsgMonitoringNotConfigured: "モニタリングが構成されていません",

	// Upgrades.
	MsgUpgradeSettingsMissing: "設定マネージャーが利用できません",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "ファイルシステムの作成",
	MsgAuditModifyFilesystem:         "ファイルシステムの変更",
	MsgAuditRemoveFilesystem:         "ファイルシステムの削除",
	MsgAuditAddRepository:            "リポジトリの追加",
	MsgAuditRemoveRepository:         "リポジトリの削除",
	MsgAuditMoveRepository:           "リポジトリの並べ替え",
	MsgAuditRefreshRepositories:      "リポジトリの更新",
	MsgAuditInstallPackage:           "パッケージのインストール",
	MsgAuditUninstallPackage:         "パッケージのアンインストール",
	MsgAuditPurgeUninstalledVolumes:  "アンインストール済みボリュームの完全削除",
	MsgAuditPurgeVolumes:             "ボリュームの完全削除",
	MsgAuditDisablePackage:           "パッケージの無効化",
	MsgAuditEnablePackage:            "パッケージの有効化",
	MsgAuditSetUnitStatus:            "ユニットステータスの設定",
	MsgAuditCreateAccount:            "アカウントの作成",
	MsgAuditUpdateAccount:            "アカウントの更新",
	MsgAuditDisableAccount:           "アカウントの無効化",
	MsgAuditAuthenticate:             "認証",
	MsgAuditRevokeSession:            "セッションの失効",
	MsgAuditUpdateSetting:            "設定の更新",
	MsgAuditDismissUpgrades:          "パッケージアップグレードの非表示",
	MsgAuditUploadArchive:            "アーカイブのアップロード",
	MsgAuditDownloadArchive:          "アーカイブのダウンロード",
	MsgAuditCreatePage:               "ページの作成",
	MsgAuditUpdatePage:               "ページの更新",
	MsgAuditRemovePage:               "ページの削除",
	MsgAuditRebuildPage:              "ページの再構築",
	MsgAuditUploadPageArchive:        "ページアーカイブのアップロード",
	MsgAuditEnableAccount:            "アカウントの有効化",
	MsgAuditRebuildGit:               "git の再構築",
	MsgAuditUploadVMImage:            "VM イメージのアップロード",
	MsgAuditDeleteVMImage:            "VM イメージの削除",
	MsgAuditAddDNSRecord:             "DNS レコードの追加",
	MsgAuditRemoveDNSRecord:          "DNS レコードの削除",
	MsgAuditSetDNSTLD:                "DNS TLD の設定",
	MsgAuditSetupDNS:                 "DNS のセットアップ",
	MsgAuditRemovePackageVolume:      "パッケージボリュームの削除",
	MsgAuditRemovePackageVolumeGroup: "パッケージボリュームグループの削除",
	MsgAuditClearLastResponses:       "キャッシュされたインストール応答の消去",
	MsgAuditSetSystemServiceStatus:   "システムサービスステータスの設定",
	MsgAuditRefreshSystemServices:    "システムサービスの更新",
	MsgAuditCreateNetwork:            "ネットワークの作成",
	MsgAuditRemoveNetwork:            "ネットワークの削除",
	MsgAuditEnableNetwork:            "ネットワークの有効化",
	MsgAuditDisableNetwork:           "ネットワークの無効化",
	MsgAuditAddNetworkPeer:           "ネットワークピアの追加",
	MsgAuditRemoveNetworkPeer:        "ネットワークピアの削除",
	MsgAuditRefreshNetworkPeer:       "ネットワークピアの更新",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "このアカウントはネットワーク登録とオブジェクトストレージのエンドポイントのみ使用できます",
	MsgAuthNetworkOnlyNetworkDenied: "このアカウントはそのネットワークで許可されていません",
	MsgAuthWireGuardPeerNotOwned:    "このアカウントは自身が登録したピアのみ更新できます",
	MsgAuthSessionNotOwned:          "このアカウントは自分のセッションのみ取り消せます",
	MsgAuthObjectStorageRequired:    "管理者またはオブジェクトストレージの権限が必要です",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "アーカイブのアップロードとダウンロードはオブジェクトストレージのパーティションを対象にできません",
	MsgGfehNotConfigured:         "オブジェクトストレージが設定されていません",
	MsgGfehNameRequired:          "名前フィールドは必須です",
	MsgGfehPartitionExists:       "パーティションは既に存在します",
	MsgGfehPartitionNotFound:     "パーティションが見つかりません",
	MsgGfehNetworkRequired:       "ネットワークフィールドは必須です",
	MsgGfehPrincipalRequired:     "プリンシパルフィールドは必須です",
	MsgGfehPathRequired:          "パスフィールドは必須です",
	MsgGfehUnknownAccount:        "そのアカウントは存在しません",
	MsgAuditCreateGfehPartition:  "オブジェクトストレージパーティションの作成",
	MsgAuditModifyGfehPartition:  "オブジェクトストレージパーティションの変更",
	MsgAuditRemoveGfehPartition:  "オブジェクトストレージパーティションの削除",
	MsgAuditAddGfehPrincipal:     "オブジェクトストレージユーザーの追加",
	MsgAuditRemoveGfehPrincipal:  "オブジェクトストレージユーザーの削除",
	MsgAuditAddGfehGrant:         "オブジェクトストレージ権限の追加",
	MsgAuditRevokeGfehGrant:      "オブジェクトストレージ権限の取り消し",
	MsgAuditWithdrawGfehExposure: "オブジェクトストレージリンクの取り下げ",
}

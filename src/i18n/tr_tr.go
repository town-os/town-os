package i18n

// trTRMessages contains all Turkish translations.
var trTRMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "yetkilendirme belirteci eksik",
	MsgAuthInvalidSession: "geçersiz oturum",
	MsgAuthAdminRequired:  "yönetici erişimi gerekli",

	// Authentication.
	MsgAuthInvalidCredentials: "geçersiz kimlik bilgileri",
	MsgAuthNotConfigured:      "kimlik doğrulama yapılandırılmamış",

	// Account management.
	MsgAccountAdminStatusImmutable: "yönetici durumu hesap oluşturulduktan sonra değiştirilemez",
	MsgAccountListError:            "hesapları listele",
	MsgAccountCheckSessions:        "etkin yönetici oturumlarını denetle",
	MsgAccountCreateFailed:         "hesap oluşturma başarısız",

	// Settings.
	MsgSettingNotFound:     "%q ayarı bulunamadı",
	MsgSettingKeyRequired:  "anahtar gerekli",
	MsgSettingInvalidBytes: "%q için geçersiz bayt değeri: %v",
	MsgSettingsMgrMissing:  "ayarlar yöneticisi kullanılamıyor",

	// Audit.
	MsgAuditNotConfigured: "denetim günlüğü yapılandırılmamış",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "etkinleştirme/devre dışı bırakma izni yok",
	MsgUnitCannotStopController:    "systemcontroller durdurulamaz",
	MsgUnitInvalidLines:            "geçersiz lines parametresi",
	MsgUnitInvalidSince:            "geçersiz since parametresi",
	MsgUnitInvalidUntil:            "geçersiz until parametresi",
	MsgUnitInvalidPriority:         "geçersiz priority parametresi",

	// Repository management.
	MsgRepoInvalidURL: "geçersiz url",

	// Pages management.
	MsgPagesNotConfigured:    "sayfalar yapılandırılmamış",
	MsgPagesGitNotConfigured: "git istemcisi veya sayfalar dizini yapılandırılmamış",

	// Package installation.
	MsgInstallNoRepoRoot:      "yapılandırılmış depo kökü yok",
	MsgInstallSummaryUpgrade:  "%s paketini %s sürümünden %s sürümüne yükselt",
	MsgInstallSummaryInstall:  "%s %s kur",
	MsgInstallSummaryImage:    "İmaj: %s",
	MsgInstallSummaryVolumes:  "%d birim",
	MsgInstallSummaryNewVols:  "%d yeni",
	MsgInstallSummaryMigrated: "%d taşındı",
	MsgInstallSummaryNoVols:   "Birim yok",
	MsgInstallSummaryPorts:    "Dış bağlantı noktaları: %s",
	MsgInstallSummaryConfig:   "Yapılandırma gerekli",
	MsgInstallSummaryVMImage:  "VM İmajı: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, ad ve sürüm gerekli",
	MsgManifestNotFound:       "paket bildirimi bulunamadı: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, ad ve sürüm gerekli",
	MsgRebuildRepoNotConfigured: "depo kökü yapılandırılmamış",
	MsgRebuildGitNotConfigured:  "git istemcisi yapılandırılmamış",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume alanı gerekli",
	MsgArchiveFileRequired:      "arşiv dosyası gerekli: %v",
	MsgArchiveUnsupportedFormat: "desteklenmeyen indirme biçimi: %s",
	MsgArchiveUnpackSuccess:     "arşiv başarıyla açıldı",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "sayfalar dizini yapılandırılmamış",
	MsgPagesNameRequired:           "ad alanı gerekli",
	MsgPagesUploadArchiveOnly:      "yükleme yalnızca arşiv türündeki sayfalar için geçerlidir",
	MsgPagesArchiveRebuildRequired: "arşiv sayfaları /pages/upload üzerinden yeni bir arşiv yüklenerek yeniden oluşturulmalıdır",

	// Monitoring.
	MsgMonitoringNotConfigured: "izleme yapılandırılmamış",

	// Upgrades.
	MsgUpgradeSettingsMissing: "ayarlar yöneticisi kullanılamıyor",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "dosya sistemi oluştur",
	MsgAuditModifyFilesystem:         "dosya sistemini değiştir",
	MsgAuditRemoveFilesystem:         "dosya sistemini kaldır",
	MsgAuditAddRepository:            "depo ekle",
	MsgAuditRemoveRepository:         "depoyu kaldır",
	MsgAuditMoveRepository:           "depoyu taşı",
	MsgAuditRefreshRepositories:      "depoları yenile",
	MsgAuditInstallPackage:           "paket kur",
	MsgAuditUninstallPackage:         "paketi kaldır",
	MsgAuditPurgeUninstalledVolumes:  "kaldırılmış birimleri temizle",
	MsgAuditPurgeVolumes:             "birimleri temizle",
	MsgAuditDisablePackage:           "paketi devre dışı bırak",
	MsgAuditEnablePackage:            "paketi etkinleştir",
	MsgAuditSetUnitStatus:            "birim durumunu ayarla",
	MsgAuditCreateAccount:            "hesap oluştur",
	MsgAuditUpdateAccount:            "hesabı güncelle",
	MsgAuditDisableAccount:           "hesabı devre dışı bırak",
	MsgAuditAuthenticate:             "kimlik doğrula",
	MsgAuditRevokeSession:            "oturumu iptal et",
	MsgAuditUpdateSetting:            "ayarı güncelle",
	MsgAuditDismissUpgrades:          "paket yükseltmelerini yoksay",
	MsgAuditUploadArchive:            "arşiv yükle",
	MsgAuditDownloadArchive:          "arşiv indir",
	MsgAuditCreatePage:               "sayfa oluştur",
	MsgAuditUpdatePage:               "sayfayı güncelle",
	MsgAuditRemovePage:               "sayfayı kaldır",
	MsgAuditRebuildPage:              "sayfayı yeniden oluştur",
	MsgAuditUploadPageArchive:        "sayfa arşivi yükle",
	MsgAuditEnableAccount:            "hesabı etkinleştir",
	MsgAuditRebuildGit:               "git yeniden oluştur",
	MsgAuditUploadVMImage:            "vm imajı yükle",
	MsgAuditDeleteVMImage:            "vm imajını sil",
	MsgAuditAddDNSRecord:             "dns kaydı ekle",
	MsgAuditRemoveDNSRecord:          "dns kaydını kaldır",
	MsgAuditSetDNSTLD:                "dns tld ayarla",
	MsgAuditSetupDNS:                 "dns kur",
	MsgAuditRemovePackageVolume:      "paket birimini kaldır",
	MsgAuditRemovePackageVolumeGroup: "paket birim grubunu kaldır",
	MsgAuditClearLastResponses:       "önbelleğe alınmış kurulum yanıtlarını temizle",
	MsgAuditSetSystemServiceStatus:   "sistem hizmeti durumunu ayarla",
	MsgAuditRefreshSystemServices:    "sistem hizmetlerini yenile",
	MsgAuditCreateNetwork:            "ağ oluştur",
	MsgAuditRemoveNetwork:            "ağı kaldır",
	MsgAuditEnableNetwork:            "ağı etkinleştir",
	MsgAuditDisableNetwork:           "ağı devre dışı bırak",
	MsgAuditAddNetworkPeer:           "ağ eşi ekle",
	MsgAuditRemoveNetworkPeer:        "ağ eşini kaldır",
	MsgAuditRefreshNetworkPeer:       "ağ eşini yenile",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "bu hesap yalnızca ağ kaydı ve nesne depolama uç noktalarını kullanabilir",
	MsgAuthNetworkOnlyNetworkDenied: "bu hesabın o ağda bulunmasına izin verilmiyor",
	MsgAuthWireGuardPeerNotOwned:    "bu hesap yalnızca kendi kaydettiği eşleri yenileyebilir",
	MsgAuthSessionNotOwned:          "bu hesap yalnızca kendi oturumlarını sonlandırabilir",
	MsgAuthObjectStorageRequired:    "yönetici veya nesne depolama erişimi gerekli",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "arşiv yüklemeleri ve indirmeleri bir nesne depolama bölümünü hedefleyemez",
	MsgGfehNotConfigured:         "nesne depolama yapılandırılmamış",
	MsgGfehNameRequired:          "ad alanı gereklidir",
	MsgGfehPartitionExists:       "bölüm zaten var",
	MsgGfehPartitionNotFound:     "bölüm bulunamadı",
	MsgGfehNetworkRequired:       "ağ alanı gereklidir",
	MsgGfehPrincipalRequired:     "asıl alanı gereklidir",
	MsgGfehPathRequired:          "yol alanı gereklidir",
	MsgGfehUnknownAccount:        "böyle bir hesap yok",
	MsgAuditCreateGfehPartition:  "nesne depolama bölümü oluştur",
	MsgAuditModifyGfehPartition:  "nesne depolama bölümünü değiştir",
	MsgAuditRemoveGfehPartition:  "nesne depolama bölümünü kaldır",
	MsgAuditAddGfehPrincipal:     "nesne depolama kullanıcısı ekle",
	MsgAuditRemoveGfehPrincipal:  "nesne depolama kullanıcısını kaldır",
	MsgAuditAddGfehGrant:         "nesne depolama izni ekle",
	MsgAuditRevokeGfehGrant:      "nesne depolama iznini iptal et",
	MsgAuditWithdrawGfehExposure: "nesne depolama bağlantısını geri çek",

	// The ingress retry page.
	MsgIngressUnavailableTitle:  "%s kullanılamıyor",
	MsgIngressUnavailableBody:   "Town OS bu adresi yönlendirmeye devam ediyor, ancak arkasındaki hizmet yanıt vermiyor. Büyük olasılıkla başlatılıyor, bir güncellemeden sonra yeniden başlatılıyor ya da kısa süreliğine aşırı yüklenmiş durumda.",
	MsgIngressUnavailableRetry:  "Yapılacak bir şey yok: bu sayfa her %d saniyede bir yeniden deniyor ve hizmet yanıt verir vermez onu gösterecek.",
	MsgIngressUnavailableFooter: "Town OS ingress",
}

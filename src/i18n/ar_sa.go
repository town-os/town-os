package i18n

// arSAMessages contains all Arabic translations.
var arSAMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "رمز التفويض مفقود",
	MsgAuthInvalidSession: "جلسة غير صالحة",
	MsgAuthAdminRequired:  "مطلوب صلاحية المسؤول",

	// Authentication.
	MsgAuthInvalidCredentials: "بيانات اعتماد غير صالحة",

	// Account management.
	MsgAccountAdminStatusImmutable: "لا يمكن تغيير حالة المسؤول بعد إنشاء الحساب",
	MsgAccountListError:            "سرد الحسابات",
	MsgAccountCheckSessions:        "التحقق من جلسات المسؤول النشطة",
	MsgAccountCreateFailed:         "فشل إنشاء الحساب",

	// Settings.
	MsgSettingNotFound:     "الإعداد %q غير موجود",
	MsgSettingKeyRequired:  "المفتاح مطلوب",
	MsgSettingInvalidBytes: "قيمة بايت غير صالحة لـ %q: %v",
	MsgSettingsMgrMissing:  "مدير الإعدادات غير متوفر",

	// Audit.
	MsgAuditNotConfigured: "تسجيل التدقيق غير مُهيأ",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "التمكين/التعطيل غير مسموح",
	MsgUnitCannotStopController:    "لا يمكن إيقاف systemcontroller",
	MsgUnitInvalidLines:            "معامل الأسطر غير صالح",
	MsgUnitInvalidSince:            "معامل since غير صالح",
	MsgUnitInvalidUntil:            "معامل until غير صالح",
	MsgUnitInvalidPriority:         "معامل الأولوية غير صالح",

	// Repository management.
	MsgRepoInvalidURL: "عنوان url غير صالح",

	// Pages management.
	MsgPagesNotConfigured:    "الصفحات غير مُهيأة",
	MsgPagesGitNotConfigured: "عميل git أو دليل الصفحات غير مُهيأ",

	// Package installation.
	MsgInstallNoRepoRoot:      "لا يوجد جذر مستودع مُهيأ",
	MsgInstallSummaryUpgrade:  "ترقية %s من %s إلى %s",
	MsgInstallSummaryInstall:  "تثبيت %s %s",
	MsgInstallSummaryImage:    "الصورة: %s",
	MsgInstallSummaryVolumes:  "%d وحدة تخزين",
	MsgInstallSummaryNewVols:  "%d جديدة",
	MsgInstallSummaryMigrated: "%d منقولة",
	MsgInstallSummaryNoVols:   "لا توجد وحدات تخزين",
	MsgInstallSummaryPorts:    "المنافذ الخارجية: %s",
	MsgInstallSummaryConfig:   "التهيئة مطلوبة",
	MsgInstallSummaryVMImage:  "صورة الجهاز الافتراضي: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "المستودع والاسم والإصدار مطلوبة",
	MsgManifestNotFound:       "بيان الحزمة غير موجود: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "المستودع والاسم والإصدار مطلوبة",
	MsgRebuildRepoNotConfigured: "جذر المستودع غير مُهيأ",
	MsgRebuildGitNotConfigured:  "عميل git غير مُهيأ",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "حقل الوحدة الفرعية مطلوب",
	MsgArchiveFileRequired:      "ملف الأرشيف مطلوب: %v",
	MsgArchiveUnsupportedFormat: "تنسيق التنزيل غير مدعوم: %s",
	MsgArchiveUnpackSuccess:     "تم فك ضغط الأرشيف بنجاح",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "دليل الصفحات غير مُهيأ",
	MsgPagesNameRequired:           "حقل الاسم مطلوب",
	MsgPagesUploadArchiveOnly:      "الرفع مسموح فقط للصفحات من نوع الأرشيف",
	MsgPagesArchiveRebuildRequired: "يجب إعادة بناء صفحات الأرشيف عن طريق رفع أرشيف جديد عبر /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "المراقبة غير مُهيأة",

	// Upgrades.
	MsgUpgradeSettingsMissing: "مدير الإعدادات غير متوفر",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "إنشاء نظام ملفات",
	MsgAuditModifyFilesystem:        "تعديل نظام ملفات",
	MsgAuditRemoveFilesystem:        "إزالة نظام ملفات",
	MsgAuditAddRepository:           "إضافة مستودع",
	MsgAuditRemoveRepository:        "إزالة مستودع",
	MsgAuditMoveRepository:          "نقل مستودع",
	MsgAuditRefreshRepositories:     "تحديث المستودعات",
	MsgAuditInstallPackage:          "تثبيت حزمة",
	MsgAuditUninstallPackage:        "إلغاء تثبيت حزمة",
	MsgAuditPurgeUninstalledVolumes: "حذف وحدات التخزين غير المثبتة",
	MsgAuditPurgeVolumes:            "حذف وحدات التخزين",
	MsgAuditDisablePackage:          "تعطيل حزمة",
	MsgAuditEnablePackage:           "تمكين حزمة",
	MsgAuditSetUnitStatus:           "تعيين حالة الوحدة",
	MsgAuditCreateAccount:           "إنشاء حساب",
	MsgAuditUpdateAccount:           "تحديث حساب",
	MsgAuditDisableAccount:          "تعطيل حساب",
	MsgAuditAuthenticate:            "المصادقة",
	MsgAuditRevokeSession:           "إبطال جلسة",
	MsgAuditUpdateSetting:           "تحديث إعداد",
	MsgAuditDismissUpgrades:         "تجاهل ترقيات الحزم",
	MsgAuditUploadArchive:           "رفع أرشيف",
	MsgAuditDownloadArchive:         "تنزيل أرشيف",
	MsgAuditCreatePage:              "إنشاء صفحة",
	MsgAuditUpdatePage:              "تحديث صفحة",
	MsgAuditRemovePage:              "إزالة صفحة",
	MsgAuditRebuildPage:             "إعادة بناء صفحة",
	MsgAuditUploadPageArchive:       "رفع أرشيف صفحة",
	MsgAuditEnableAccount:           "تمكين حساب",
	MsgAuditRebuildGit:              "إعادة بناء git",
	MsgAuditUploadVMImage:           "رفع صورة جهاز افتراضي",
	MsgAuditDeleteVMImage:           "حذف صورة جهاز افتراضي",
	MsgAuditAddDNSRecord:            "إضافة سجل dns",
	MsgAuditRemoveDNSRecord:         "إزالة سجل dns",
	MsgAuditSetDNSTLD:               "تعيين نطاق dns العلوي",
	MsgAuditSetupDNS:                "إعداد dns",
	MsgAuditRemovePackageVolume:     "إزالة وحدة تخزين الحزمة",
	MsgAuditRemovePackageVolumeGroup: "إزالة مجموعة وحدات تخزين الحزمة",
	MsgAuditClearLastResponses:      "مسح إجابات التثبيت المخزنة",
	MsgAuditSetSystemServiceStatus:  "تعيين حالة خدمة النظام",
	MsgAuditRefreshSystemServices:   "تحديث خدمات النظام",
	MsgAuditCreateNetwork:           "إنشاء شبكة",
	MsgAuditRemoveNetwork:           "إزالة شبكة",
	MsgAuditEnableNetwork:           "تمكين شبكة",
	MsgAuditDisableNetwork:          "تعطيل شبكة",
	MsgAuditAddNetworkPeer:          "إضافة نظير شبكة",
	MsgAuditRemoveNetworkPeer:       "إزالة نظير شبكة",
	MsgAuditRefreshNetworkPeer:      "تحديث نظير شبكة",

	// WireGuard-only account restrictions.
	MsgAuthWireGuardRestricted:    "قد يستخدم هذا الحساب نقاط تسجيل wireguard فقط",
	MsgAuthWireGuardNetworkDenied: "هذا الحساب غير مسموح له على تلك الشبكة",
	MsgAuthWireGuardPeerNotOwned:  "قد يحدّث هذا الحساب النظراء الذين سجّلهم فقط",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "لا يمكن لعمليات رفع وتنزيل الأرشيف استهداف قسم تخزين الكائنات",
	MsgGfehNotConfigured:         "تخزين الكائنات غير مُهيأ",
	MsgGfehNameRequired:          "حقل الاسم مطلوب",
	MsgGfehPartitionExists:       "القسم موجود بالفعل",
	MsgGfehPartitionNotFound:     "القسم غير موجود",
	MsgGfehNetworkRequired:       "حقل الشبكة مطلوب",
	MsgGfehPrincipalRequired:     "حقل المستخدم مطلوب",
	MsgGfehPathRequired:          "حقل المسار مطلوب",
	MsgGfehUnknownAccount:        "لا يوجد حساب بهذا الاسم",
	MsgGfehServiceAccountProtected: "لا يمكن تعطيل حساب خدمة تخزين الكائنات؛ فكل قسم يصادق باستخدامه",
	MsgAuditCreateGfehPartition:  "إنشاء قسم تخزين الكائنات",
	MsgAuditModifyGfehPartition:  "تعديل قسم تخزين الكائنات",
	MsgAuditRemoveGfehPartition:  "إزالة قسم تخزين الكائنات",
	MsgAuditAddGfehPrincipal:     "إضافة مستخدم تخزين الكائنات",
	MsgAuditRemoveGfehPrincipal:  "إزالة مستخدم تخزين الكائنات",
	MsgAuditAddGfehGrant:         "إضافة صلاحية تخزين الكائنات",
	MsgAuditRevokeGfehGrant:      "إلغاء صلاحية تخزين الكائنات",
	MsgAuditWithdrawGfehExposure: "سحب رابط تخزين الكائنات",
}

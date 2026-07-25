package i18n

// bnBDMessages contains all Bengali translations.
var bnBDMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "অনুমোদন টোকেন অনুপস্থিত",
	MsgAuthInvalidSession: "অবৈধ সেশন",
	MsgAuthAdminRequired:  "অ্যাডমিন অ্যাক্সেস প্রয়োজন",

	// Authentication.
	MsgAuthInvalidCredentials: "অবৈধ শংসাপত্র",

	// Account management.
	MsgAccountAdminStatusImmutable: "অ্যাকাউন্ট তৈরির পরে অ্যাডমিন স্ট্যাটাস পরিবর্তন করা যায় না",
	MsgAccountListError:            "অ্যাকাউন্ট তালিকাভুক্ত করুন",
	MsgAccountCheckSessions:        "সক্রিয় অ্যাডমিন সেশন যাচাই করুন",
	MsgAccountCreateFailed:         "অ্যাকাউন্ট তৈরি ব্যর্থ হয়েছে",

	// Settings.
	MsgSettingNotFound:     "সেটিং %q পাওয়া যায়নি",
	MsgSettingKeyRequired:  "কী প্রয়োজন",
	MsgSettingInvalidBytes: "%q-এর জন্য অবৈধ বাইট মান: %v",
	MsgSettingsMgrMissing:  "সেটিংস ম্যানেজার উপলব্ধ নেই",

	// Audit.
	MsgAuditNotConfigured: "অডিট লগিং কনফিগার করা হয়নি",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "সক্রিয়/নিষ্ক্রিয় করার অনুমতি নেই",
	MsgUnitCannotStopController:    "systemcontroller বন্ধ করা যায় না",
	MsgUnitInvalidLines:            "অবৈধ lines প্যারামিটার",
	MsgUnitInvalidSince:            "অবৈধ since প্যারামিটার",
	MsgUnitInvalidUntil:            "অবৈধ until প্যারামিটার",
	MsgUnitInvalidPriority:         "অবৈধ priority প্যারামিটার",

	// Repository management.
	MsgRepoInvalidURL: "অবৈধ url",

	// Pages management.
	MsgPagesNotConfigured:    "pages কনফিগার করা হয়নি",
	MsgPagesGitNotConfigured: "git ক্লায়েন্ট বা pages ডিরেক্টরি কনফিগার করা হয়নি",

	// Package installation.
	MsgInstallNoRepoRoot:      "কোনো রিপোজিটরি রুট কনফিগার করা হয়নি",
	MsgInstallSummaryUpgrade:  "%s-কে %s থেকে %s-এ আপগ্রেড করুন",
	MsgInstallSummaryInstall:  "%s %s ইনস্টল করুন",
	MsgInstallSummaryImage:    "ইমেজ: %s",
	MsgInstallSummaryVolumes:  "%d টি ভলিউম",
	MsgInstallSummaryNewVols:  "%d টি নতুন",
	MsgInstallSummaryMigrated: "%d টি স্থানান্তরিত",
	MsgInstallSummaryNoVols:   "কোনো ভলিউম নেই",
	MsgInstallSummaryPorts:    "বাহ্যিক পোর্ট: %s",
	MsgInstallSummaryConfig:   "কনফিগারেশন প্রয়োজন",
	MsgInstallSummaryVMImage:  "VM ইমেজ: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name, এবং version প্রয়োজন",
	MsgManifestNotFound:       "প্যাকেজ ম্যানিফেস্ট পাওয়া যায়নি: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repo, name, এবং version প্রয়োজন",
	MsgRebuildRepoNotConfigured: "রিপোজিটরি রুট কনফিগার করা হয়নি",
	MsgRebuildGitNotConfigured:  "git ক্লায়েন্ট কনফিগার করা হয়নি",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume ফিল্ড প্রয়োজন",
	MsgArchiveFileRequired:      "আর্কাইভ ফাইল প্রয়োজন: %v",
	MsgArchiveUnsupportedFormat: "অসমর্থিত ডাউনলোড ফরম্যাট: %s",
	MsgArchiveUnpackSuccess:     "আর্কাইভ সফলভাবে আনপ্যাক করা হয়েছে",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "pages ডিরেক্টরি কনফিগার করা হয়নি",
	MsgPagesNameRequired:           "name ফিল্ড প্রয়োজন",
	MsgPagesUploadArchiveOnly:      "আপলোড শুধুমাত্র আর্কাইভ-টাইপ পৃষ্ঠার জন্য অনুমোদিত",
	MsgPagesArchiveRebuildRequired: "আর্কাইভ পৃষ্ঠাগুলি /pages/upload-এর মাধ্যমে একটি নতুন আর্কাইভ আপলোড করে পুনর্নির্মাণ করতে হবে",

	// Monitoring.
	MsgMonitoringNotConfigured: "মনিটরিং কনফিগার করা হয়নি",

	// Upgrades.
	MsgUpgradeSettingsMissing: "সেটিংস ম্যানেজার উপলব্ধ নেই",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "ফাইলসিস্টেম তৈরি করুন",
	MsgAuditModifyFilesystem:        "ফাইলসিস্টেম পরিবর্তন করুন",
	MsgAuditRemoveFilesystem:        "ফাইলসিস্টেম সরান",
	MsgAuditAddRepository:           "রিপোজিটরি যোগ করুন",
	MsgAuditRemoveRepository:        "রিপোজিটরি সরান",
	MsgAuditMoveRepository:          "রিপোজিটরি সরান",
	MsgAuditRefreshRepositories:     "রিপোজিটরি রিফ্রেশ করুন",
	MsgAuditInstallPackage:          "প্যাকেজ ইনস্টল করুন",
	MsgAuditUninstallPackage:        "প্যাকেজ আনইনস্টল করুন",
	MsgAuditPurgeUninstalledVolumes: "আনইনস্টল করা ভলিউম পার্জ করুন",
	MsgAuditPurgeVolumes:            "ভলিউম পার্জ করুন",
	MsgAuditDisablePackage:          "প্যাকেজ নিষ্ক্রিয় করুন",
	MsgAuditEnablePackage:           "প্যাকেজ সক্রিয় করুন",
	MsgAuditSetUnitStatus:           "ইউনিট স্ট্যাটাস সেট করুন",
	MsgAuditCreateAccount:           "অ্যাকাউন্ট তৈরি করুন",
	MsgAuditUpdateAccount:           "অ্যাকাউন্ট আপডেট করুন",
	MsgAuditDisableAccount:          "অ্যাকাউন্ট নিষ্ক্রিয় করুন",
	MsgAuditAuthenticate:            "প্রমাণীকরণ করুন",
	MsgAuditRevokeSession:           "সেশন প্রত্যাহার করুন",
	MsgAuditUpdateSetting:           "সেটিং আপডেট করুন",
	MsgAuditDismissUpgrades:         "প্যাকেজ আপগ্রেড খারিজ করুন",
	MsgAuditUploadArchive:           "আর্কাইভ আপলোড করুন",
	MsgAuditDownloadArchive:         "আর্কাইভ ডাউনলোড করুন",
	MsgAuditCreatePage:              "পৃষ্ঠা তৈরি করুন",
	MsgAuditUpdatePage:              "পৃষ্ঠা আপডেট করুন",
	MsgAuditRemovePage:              "পৃষ্ঠা সরান",
	MsgAuditRebuildPage:             "পৃষ্ঠা পুনর্নির্মাণ করুন",
	MsgAuditUploadPageArchive:       "পৃষ্ঠা আর্কাইভ আপলোড করুন",
	MsgAuditEnableAccount:           "অ্যাকাউন্ট সক্রিয় করুন",
	MsgAuditRebuildGit:              "git পুনর্নির্মাণ করুন",
	MsgAuditUploadVMImage:           "vm ইমেজ আপলোড করুন",
	MsgAuditDeleteVMImage:           "vm ইমেজ মুছুন",
	MsgAuditAddDNSRecord:            "dns রেকর্ড যোগ করুন",
	MsgAuditRemoveDNSRecord:         "dns রেকর্ড সরান",
	MsgAuditSetDNSTLD:               "dns tld সেট করুন",
	MsgAuditSetupDNS:                "dns সেটআপ করুন",
	MsgAuditRemovePackageVolume:     "প্যাকেজ ভলিউম সরান",
	MsgAuditRemovePackageVolumeGroup: "প্যাকেজ ভলিউম গ্রুপ সরান",
	MsgAuditClearLastResponses:      "ক্যাশ করা ইনস্টল প্রতিক্রিয়া মুছুন",
	MsgAuditSetSystemServiceStatus:  "সিস্টেম সার্ভিস স্ট্যাটাস সেট করুন",
	MsgAuditRefreshSystemServices:   "সিস্টেম সার্ভিস রিফ্রেশ করুন",
	MsgAuditCreateNetwork:           "নেটওয়ার্ক তৈরি করুন",
	MsgAuditRemoveNetwork:           "নেটওয়ার্ক সরান",
	MsgAuditEnableNetwork:           "নেটওয়ার্ক সক্রিয় করুন",
	MsgAuditDisableNetwork:          "নেটওয়ার্ক নিষ্ক্রিয় করুন",
	MsgAuditAddNetworkPeer:          "নেটওয়ার্ক পিয়ার যোগ করুন",
	MsgAuditRemoveNetworkPeer:       "নেটওয়ার্ক পিয়ার সরান",
	MsgAuditRefreshNetworkPeer:      "নেটওয়ার্ক পিয়ার রিফ্রেশ করুন",

	// WireGuard-only account restrictions.
	MsgAuthWireGuardRestricted:    "এই অ্যাকাউন্টটি শুধুমাত্র wireguard এনরোলমেন্ট এন্ডপয়েন্ট ব্যবহার করতে পারে",
	MsgAuthWireGuardNetworkDenied: "এই অ্যাকাউন্টটি সেই নেটওয়ার্কে অনুমোদিত নয়",
	MsgAuthWireGuardPeerNotOwned:  "এই অ্যাকাউন্টটি শুধুমাত্র সেই পিয়ারগুলি রিফ্রেশ করতে পারে যা এটি এনরোল করেছে",
}

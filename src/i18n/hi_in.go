package i18n

// hiINMessages contains all Hindi translations.
var hiINMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "प्राधिकरण टोकन अनुपस्थित है",
	MsgAuthInvalidSession: "अमान्य सत्र",
	MsgAuthAdminRequired:  "व्यवस्थापक पहुँच आवश्यक है",

	// Authentication.
	MsgAuthInvalidCredentials: "अमान्य प्रमाण-पत्र",

	// Account management.
	MsgAccountAdminStatusImmutable: "खाता बनने के बाद व्यवस्थापक स्थिति बदली नहीं जा सकती",
	MsgAccountListError:            "खातों की सूची बनाएँ",
	MsgAccountCheckSessions:        "सक्रिय व्यवस्थापक सत्रों की जाँच करें",
	MsgAccountCreateFailed:         "खाता बनाने में विफल",

	// Settings.
	MsgSettingNotFound:     "सेटिंग %q नहीं मिली",
	MsgSettingKeyRequired:  "कुंजी आवश्यक है",
	MsgSettingInvalidBytes: "%q के लिए अमान्य बाइट मान: %v",
	MsgSettingsMgrMissing:  "सेटिंग प्रबंधक उपलब्ध नहीं है",

	// Audit.
	MsgAuditNotConfigured: "ऑडिट लॉगिंग कॉन्फ़िगर नहीं है",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "सक्षम/अक्षम करने की अनुमति नहीं है",
	MsgUnitCannotStopController:    "systemcontroller को रोका नहीं जा सकता",
	MsgUnitInvalidLines:            "अमान्य lines पैरामीटर",
	MsgUnitInvalidSince:            "अमान्य since पैरामीटर",
	MsgUnitInvalidUntil:            "अमान्य until पैरामीटर",
	MsgUnitInvalidPriority:         "अमान्य priority पैरामीटर",

	// Repository management.
	MsgRepoInvalidURL: "अमान्य url",

	// Pages management.
	MsgPagesNotConfigured:    "pages कॉन्फ़िगर नहीं है",
	MsgPagesGitNotConfigured: "git क्लाइंट या pages निर्देशिका कॉन्फ़िगर नहीं है",

	// Package installation.
	MsgInstallNoRepoRoot:      "कोई रिपॉजिटरी रूट कॉन्फ़िगर नहीं है",
	MsgInstallSummaryUpgrade:  "%s को %s से %s में अपग्रेड करें",
	MsgInstallSummaryInstall:  "%s %s इंस्टॉल करें",
	MsgInstallSummaryImage:    "इमेज: %s",
	MsgInstallSummaryVolumes:  "%d वॉल्यूम",
	MsgInstallSummaryNewVols:  "%d नए",
	MsgInstallSummaryMigrated: "%d माइग्रेट किए गए",
	MsgInstallSummaryNoVols:   "कोई वॉल्यूम नहीं",
	MsgInstallSummaryPorts:    "बाहरी पोर्ट: %s",
	MsgInstallSummaryConfig:   "कॉन्फ़िगरेशन आवश्यक है",
	MsgInstallSummaryVMImage:  "VM इमेज: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name, और version आवश्यक हैं",
	MsgManifestNotFound:       "पैकेज मैनिफेस्ट नहीं मिला: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "repo, name, और version आवश्यक हैं",
	MsgRebuildRepoNotConfigured: "रिपॉजिटरी रूट कॉन्फ़िगर नहीं है",
	MsgRebuildGitNotConfigured:  "git क्लाइंट कॉन्फ़िगर नहीं है",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume फ़ील्ड आवश्यक है",
	MsgArchiveFileRequired:      "आर्काइव फ़ाइल आवश्यक है: %v",
	MsgArchiveUnsupportedFormat: "असमर्थित डाउनलोड प्रारूप: %s",
	MsgArchiveUnpackSuccess:     "आर्काइव सफलतापूर्वक अनपैक किया गया",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "pages निर्देशिका कॉन्फ़िगर नहीं है",
	MsgPagesNameRequired:           "name फ़ील्ड आवश्यक है",
	MsgPagesUploadArchiveOnly:      "अपलोड केवल आर्काइव-प्रकार के pages के लिए अनुमत है",
	MsgPagesArchiveRebuildRequired: "आर्काइव pages को /pages/upload के माध्यम से नया आर्काइव अपलोड करके फिर से बनाना होगा",

	// Monitoring.
	MsgMonitoringNotConfigured: "मॉनिटरिंग कॉन्फ़िगर नहीं है",

	// Upgrades.
	MsgUpgradeSettingsMissing: "सेटिंग प्रबंधक उपलब्ध नहीं है",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "फ़ाइल-सिस्टम बनाएँ",
	MsgAuditModifyFilesystem:        "फ़ाइल-सिस्टम संशोधित करें",
	MsgAuditRemoveFilesystem:        "फ़ाइल-सिस्टम हटाएँ",
	MsgAuditAddRepository:           "रिपॉजिटरी जोड़ें",
	MsgAuditRemoveRepository:        "रिपॉजिटरी हटाएँ",
	MsgAuditMoveRepository:          "रिपॉजिटरी स्थानांतरित करें",
	MsgAuditRefreshRepositories:     "रिपॉजिटरियाँ ताज़ा करें",
	MsgAuditInstallPackage:          "पैकेज इंस्टॉल करें",
	MsgAuditUninstallPackage:        "पैकेज अनइंस्टॉल करें",
	MsgAuditPurgeUninstalledVolumes: "अनइंस्टॉल किए गए वॉल्यूम हटाएँ",
	MsgAuditPurgeVolumes:            "वॉल्यूम हटाएँ",
	MsgAuditDisablePackage:          "पैकेज अक्षम करें",
	MsgAuditEnablePackage:           "पैकेज सक्षम करें",
	MsgAuditSetUnitStatus:           "यूनिट स्थिति सेट करें",
	MsgAuditCreateAccount:           "खाता बनाएँ",
	MsgAuditUpdateAccount:           "खाता अपडेट करें",
	MsgAuditDisableAccount:          "खाता अक्षम करें",
	MsgAuditAuthenticate:            "प्रमाणित करें",
	MsgAuditRevokeSession:           "सत्र रद्द करें",
	MsgAuditUpdateSetting:           "सेटिंग अपडेट करें",
	MsgAuditDismissUpgrades:         "पैकेज अपग्रेड खारिज करें",
	MsgAuditUploadArchive:           "आर्काइव अपलोड करें",
	MsgAuditDownloadArchive:         "आर्काइव डाउनलोड करें",
	MsgAuditCreatePage:              "पेज बनाएँ",
	MsgAuditUpdatePage:              "पेज अपडेट करें",
	MsgAuditRemovePage:              "पेज हटाएँ",
	MsgAuditRebuildPage:             "पेज फिर से बनाएँ",
	MsgAuditUploadPageArchive:       "पेज आर्काइव अपलोड करें",
	MsgAuditEnableAccount:           "खाता सक्षम करें",
	MsgAuditRebuildGit:              "git फिर से बनाएँ",
	MsgAuditUploadVMImage:           "vm इमेज अपलोड करें",
	MsgAuditDeleteVMImage:           "vm इमेज हटाएँ",
	MsgAuditAddDNSRecord:            "dns रिकॉर्ड जोड़ें",
	MsgAuditRemoveDNSRecord:         "dns रिकॉर्ड हटाएँ",
	MsgAuditSetDNSTLD:               "dns tld सेट करें",
	MsgAuditSetupDNS:                "dns सेटअप करें",
	MsgAuditRemovePackageVolume:     "पैकेज वॉल्यूम हटाएँ",
	MsgAuditRemovePackageVolumeGroup: "पैकेज वॉल्यूम समूह हटाएँ",
	MsgAuditClearLastResponses:      "कैश किए गए इंस्टॉल उत्तर साफ़ करें",
	MsgAuditSetSystemServiceStatus:  "सिस्टम सेवा स्थिति सेट करें",
	MsgAuditRefreshSystemServices:   "सिस्टम सेवाएँ ताज़ा करें",
	MsgAuditCreateNetwork:           "नेटवर्क बनाएँ",
	MsgAuditRemoveNetwork:           "नेटवर्क हटाएँ",
	MsgAuditEnableNetwork:           "नेटवर्क सक्षम करें",
	MsgAuditDisableNetwork:          "नेटवर्क अक्षम करें",
	MsgAuditAddNetworkPeer:          "नेटवर्क पीयर जोड़ें",
	MsgAuditRemoveNetworkPeer:       "नेटवर्क पीयर हटाएँ",
	MsgAuditRefreshNetworkPeer:      "नेटवर्क पीयर ताज़ा करें",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "यह खाता केवल नेटवर्क एनरोलमेंट और ऑब्जेक्ट स्टोरेज एंडपॉइंट का उपयोग कर सकता है",
	MsgAuthNetworkOnlyNetworkDenied: "इस खाते को उस नेटवर्क पर अनुमति नहीं है",
	MsgAuthWireGuardPeerNotOwned:  "यह खाता केवल उन्हीं पीयर को ताज़ा कर सकता है जिन्हें उसने एनरोल किया",
	MsgAuthObjectStorageRequired:  "प्रशासक या ऑब्जेक्ट स्टोरेज पहुँच आवश्यक है",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "संग्रह अपलोड और डाउनलोड ऑब्जेक्ट स्टोरेज पार्टीशन को संबोधित नहीं कर सकते",
	MsgGfehNotConfigured:         "ऑब्जेक्ट स्टोरेज कॉन्फ़िगर नहीं है",
	MsgGfehNameRequired:          "नाम फ़ील्ड आवश्यक है",
	MsgGfehPartitionExists:       "पार्टीशन पहले से मौजूद है",
	MsgGfehPartitionNotFound:     "पार्टीशन नहीं मिला",
	MsgGfehNetworkRequired:       "नेटवर्क फ़ील्ड आवश्यक है",
	MsgGfehPrincipalRequired:     "प्रिंसिपल फ़ील्ड आवश्यक है",
	MsgGfehPathRequired:          "पथ फ़ील्ड आवश्यक है",
	MsgGfehUnknownAccount:        "ऐसा कोई खाता नहीं",
	MsgAuditCreateGfehPartition:  "ऑब्जेक्ट स्टोरेज पार्टीशन बनाएँ",
	MsgAuditModifyGfehPartition:  "ऑब्जेक्ट स्टोरेज पार्टीशन बदलें",
	MsgAuditRemoveGfehPartition:  "ऑब्जेक्ट स्टोरेज पार्टीशन हटाएँ",
	MsgAuditAddGfehPrincipal:     "ऑब्जेक्ट स्टोरेज उपयोगकर्ता जोड़ें",
	MsgAuditRemoveGfehPrincipal:  "ऑब्जेक्ट स्टोरेज उपयोगकर्ता हटाएँ",
	MsgAuditAddGfehGrant:         "ऑब्जेक्ट स्टोरेज अनुमति जोड़ें",
	MsgAuditRevokeGfehGrant:      "ऑब्जेक्ट स्टोरेज अनुमति निरस्त करें",
	MsgAuditWithdrawGfehExposure: "ऑब्जेक्ट स्टोरेज लिंक वापस लें",
}

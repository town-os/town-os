package i18n

// saINMessages contains all Sanskrit translations.
var saINMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "प्राधिकरण-चिह्नकं न विद्यते",
	MsgAuthInvalidSession: "अमान्यं सत्रम्",
	MsgAuthAdminRequired:  "प्रशासकाधिकारः आवश्यकः",

	// Authentication.
	MsgAuthInvalidCredentials: "अमान्यानि प्रमाणपत्राणि",
	MsgAuthNotConfigured:      "प्रमाणीकरणं न विन्यस्तम्",

	// Account management.
	MsgAccountAdminStatusImmutable: "खातासृष्टेः अनन्तरं प्रशासकपदं परिवर्तयितुं न शक्यते",
	MsgAccountListError:            "खातानां सूची",
	MsgAccountCheckSessions:        "सक्रियाणि प्रशासकसत्राणि परीक्ष्यताम्",
	MsgAccountCreateFailed:         "खातासृष्टिः विफला",

	// Settings.
	MsgSettingNotFound:     "%q इति व्यवस्था न प्राप्ता",
	MsgSettingKeyRequired:  "कुञ्जिका आवश्यका",
	MsgSettingInvalidBytes: "%q कृते अमान्यं byte-मानम्: %v",
	MsgSettingsMgrMissing:  "व्यवस्थाप्रबन्धकः न उपलब्धः",

	// Audit.
	MsgAuditNotConfigured: "लेखापरीक्षण-अभिलेखनं न विन्यस्तम्",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "सक्रियकरणम्/निष्क्रियकरणं न अनुमतम्",
	MsgUnitCannotStopController:    "systemcontroller स्थगयितुं न शक्यते",
	MsgUnitInvalidLines:            "अमान्यः lines-प्राचलः",
	MsgUnitInvalidSince:            "अमान्यः since-प्राचलः",
	MsgUnitInvalidUntil:            "अमान्यः until-प्राचलः",
	MsgUnitInvalidPriority:         "अमान्यः priority-प्राचलः",

	// Repository management.
	MsgRepoInvalidURL: "अमान्यं url",

	// Pages management.
	MsgPagesNotConfigured:    "pages न विन्यस्तम्",
	MsgPagesGitNotConfigured: "git-ग्राहकः अथवा pages-निर्देशिका न विन्यस्ता",

	// Package installation.
	MsgInstallNoRepoRoot:      "कोऽपि repository-मूलः न विन्यस्तः",
	MsgInstallSummaryUpgrade:  "%s इति %s तः %s पर्यन्तम् उन्नयनम्",
	MsgInstallSummaryInstall:  "%s %s संस्थाप्यताम्",
	MsgInstallSummaryImage:    "प्रतिबिम्बम्: %s",
	MsgInstallSummaryVolumes:  "%d आयतन(आनि)",
	MsgInstallSummaryNewVols:  "%d नवीनानि",
	MsgInstallSummaryMigrated: "%d स्थानान्तरितानि",
	MsgInstallSummaryNoVols:   "आयतनानि न सन्ति",
	MsgInstallSummaryPorts:    "बाह्यद्वाराणि: %s",
	MsgInstallSummaryConfig:   "विन्यासः आवश्यकः",
	MsgInstallSummaryVMImage:  "VM प्रतिबिम्बम्: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, name, version च आवश्यकानि",
	MsgManifestNotFound:       "प्रावरण-प्रकटनं न प्राप्तम्: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, name, version च आवश्यकानि",
	MsgRebuildRepoNotConfigured: "repository-मूलः न विन्यस्तः",
	MsgRebuildGitNotConfigured:  "git-ग्राहकः न विन्यस्तः",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "subvolume-क्षेत्रम् आवश्यकम्",
	MsgArchiveFileRequired:      "अभिलेख-सञ्चिका आवश्यका: %v",
	MsgArchiveUnsupportedFormat: "असमर्थितं अवतरण-प्रारूपम्: %s",
	MsgArchiveUnpackSuccess:     "अभिलेखः सफलतया उद्घाटितः",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "pages-निर्देशिका न विन्यस्ता",
	MsgPagesNameRequired:           "name-क्षेत्रम् आवश्यकम्",
	MsgPagesUploadArchiveOnly:      "आरोपणम् केवलम् archive-प्रकारस्य pages कृते अनुमतम्",
	MsgPagesArchiveRebuildRequired: "archive-pages नवीन-अभिलेखं /pages/upload द्वारा आरोप्य पुनर्निर्माणीयानि",

	// Monitoring.
	MsgMonitoringNotConfigured: "अनुवीक्षणं न विन्यस्तम्",

	// Upgrades.
	MsgUpgradeSettingsMissing: "व्यवस्थाप्रबन्धकः न उपलब्धः",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "सञ्चिका-तन्त्रस्य सृष्टिः",
	MsgAuditModifyFilesystem:         "सञ्चिका-तन्त्रस्य परिवर्तनम्",
	MsgAuditRemoveFilesystem:         "सञ्चिका-तन्त्रस्य निष्कासनम्",
	MsgAuditAddRepository:            "repository-योजनम्",
	MsgAuditRemoveRepository:         "repository-निष्कासनम्",
	MsgAuditMoveRepository:           "repository-स्थानान्तरणम्",
	MsgAuditRefreshRepositories:      "repository-पुनःसंस्करणम्",
	MsgAuditInstallPackage:           "प्रावरण-संस्थापनम्",
	MsgAuditUninstallPackage:         "प्रावरण-उन्मूलनम्",
	MsgAuditPurgeUninstalledVolumes:  "उन्मूलितानां आयतनानां परिमार्जनम्",
	MsgAuditPurgeVolumes:             "आयतन-परिमार्जनम्",
	MsgAuditDisablePackage:           "प्रावरण-निष्क्रियकरणम्",
	MsgAuditEnablePackage:            "प्रावरण-सक्रियकरणम्",
	MsgAuditSetUnitStatus:            "एककस्य स्थितिः स्थाप्यताम्",
	MsgAuditCreateAccount:            "खाता-सृष्टिः",
	MsgAuditUpdateAccount:            "खाता-अद्यतनम्",
	MsgAuditDisableAccount:           "खाता-निष्क्रियकरणम्",
	MsgAuditAuthenticate:             "प्रमाणीकरणम्",
	MsgAuditRevokeSession:            "सत्र-प्रत्याहरणम्",
	MsgAuditUpdateSetting:            "व्यवस्था-अद्यतनम्",
	MsgAuditDismissUpgrades:          "प्रावरण-उन्नयनानां त्यागः",
	MsgAuditUploadArchive:            "अभिलेख-आरोपणम्",
	MsgAuditDownloadArchive:          "अभिलेख-अवतरणम्",
	MsgAuditCreatePage:               "पृष्ठ-सृष्टिः",
	MsgAuditUpdatePage:               "पृष्ठ-अद्यतनम्",
	MsgAuditRemovePage:               "पृष्ठ-निष्कासनम्",
	MsgAuditRebuildPage:              "पृष्ठ-पुनर्निर्माणम्",
	MsgAuditUploadPageArchive:        "पृष्ठ-अभिलेख-आरोपणम्",
	MsgAuditEnableAccount:            "खाता-सक्रियकरणम्",
	MsgAuditRebuildGit:               "git-पुनर्निर्माणम्",
	MsgAuditUploadVMImage:            "VM-प्रतिबिम्ब-आरोपणम्",
	MsgAuditDeleteVMImage:            "VM-प्रतिबिम्ब-निष्कासनम्",
	MsgAuditAddDNSRecord:             "DNS-अभिलेख-योजनम्",
	MsgAuditRemoveDNSRecord:          "DNS-अभिलेख-निष्कासनम्",
	MsgAuditSetDNSTLD:                "DNS TLD स्थाप्यताम्",
	MsgAuditSetupDNS:                 "DNS-व्यवस्थापनम्",
	MsgAuditRemovePackageVolume:      "प्रावरण-आयतन-निष्कासनम्",
	MsgAuditRemovePackageVolumeGroup: "प्रावरण-आयतन-समूह-निष्कासनम्",
	MsgAuditClearLastResponses:       "निहित-संस्थापन-प्रतिक्रियाणां मार्जनम्",
	MsgAuditSetSystemServiceStatus:   "तन्त्र-सेवायाः स्थितिः स्थाप्यताम्",
	MsgAuditRefreshSystemServices:    "तन्त्र-सेवानां पुनःसंस्करणम्",
	MsgAuditCreateNetwork:            "जाल-सृष्टिः",
	MsgAuditRemoveNetwork:            "जाल-निष्कासनम्",
	MsgAuditEnableNetwork:            "जाल-सक्रियकरणम्",
	MsgAuditDisableNetwork:           "जाल-निष्क्रियकरणम्",
	MsgAuditAddNetworkPeer:           "जाल-सहचर-योजनम्",
	MsgAuditRemoveNetworkPeer:        "जाल-सहचर-निष्कासनम्",
	MsgAuditRefreshNetworkPeer:       "जाल-सहचर-पुनःसंस्करणम्",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "इदं खातम् केवलं जाल-नामाङ्कनस्य वस्तु-कोशस्य च अन्तर्मुखानि उपयोक्तुं शक्नोति",
	MsgAuthNetworkOnlyNetworkDenied: "इदं खातं तस्मिन् जाले न अनुमतम्",
	MsgAuthWireGuardPeerNotOwned:    "इदं खातं केवलं स्वेन नामाङ्कितान् सहचरान् पुनःसंस्कर्तुं शक्नोति",
	MsgAuthSessionNotOwned:          "एतत् खातं स्वकीयानि सत्राणि एव निराकर्तुं शक्नोति",
	MsgAuthObjectStorageRequired:    "प्रबन्धकस्य वस्तुसंग्रहस्य वा प्रवेशः आवश्यकः",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "सञ्चयिका-आरोपणम् अवतरणं च वस्तुसंग्रहविभागं न स्पृशति",
	MsgGfehNotConfigured:         "वस्तुसंग्रहः न संविहितः",
	MsgGfehNameRequired:          "नामक्षेत्रम् आवश्यकम्",
	MsgGfehPartitionExists:       "विभागः पूर्वमेव विद्यते",
	MsgGfehPartitionNotFound:     "विभागः न प्राप्तः",
	MsgGfehNetworkRequired:       "जालक्षेत्रम् आवश्यकम्",
	MsgGfehPrincipalRequired:     "प्रधानक्षेत्रम् आवश्यकम्",
	MsgGfehPathRequired:          "मार्गक्षेत्रम् आवश्यकम्",
	MsgGfehUnknownAccount:        "तादृशं खातं नास्ति",
	MsgAuditCreateGfehPartition:  "वस्तुसंग्रहविभागस्य निर्माणम्",
	MsgAuditModifyGfehPartition:  "वस्तुसंग्रहविभागस्य परिवर्तनम्",
	MsgAuditRemoveGfehPartition:  "वस्तुसंग्रहविभागस्य अपनयनम्",
	MsgAuditAddGfehPrincipal:     "वस्तुसंग्रहप्रयोक्तुः योजनम्",
	MsgAuditRemoveGfehPrincipal:  "वस्तुसंग्रहप्रयोक्तुः अपनयनम्",
	MsgAuditAddGfehGrant:         "वस्तुसंग्रहानुज्ञायाः योजनम्",
	MsgAuditRevokeGfehGrant:      "वस्तुसंग्रहानुज्ञायाः निवर्तनम्",
	MsgAuditWithdrawGfehExposure: "वस्तुसंग्रहसम्पर्कस्य निवर्तनम्",
}

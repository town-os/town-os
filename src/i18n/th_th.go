package i18n

// thTHMessages contains all Thai translations.
var thTHMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "ไม่มีโทเคนสำหรับการอนุญาต",
	MsgAuthInvalidSession: "เซสชันไม่ถูกต้อง",
	MsgAuthAdminRequired:  "ต้องใช้สิทธิ์ผู้ดูแลระบบ",

	// Authentication.
	MsgAuthInvalidCredentials: "ข้อมูลรับรองไม่ถูกต้อง",

	// Account management.
	MsgAccountAdminStatusImmutable: "ไม่สามารถเปลี่ยนสถานะผู้ดูแลระบบได้หลังจากสร้างบัญชีแล้ว",
	MsgAccountListError:            "แสดงรายการบัญชี",
	MsgAccountCheckSessions:        "ตรวจสอบเซสชันผู้ดูแลระบบที่ใช้งานอยู่",
	MsgAccountCreateFailed:         "สร้างบัญชีไม่สำเร็จ",

	// Settings.
	MsgSettingNotFound:     "ไม่พบการตั้งค่า %q",
	MsgSettingKeyRequired:  "ต้องระบุคีย์",
	MsgSettingInvalidBytes: "ค่าไบต์ไม่ถูกต้องสำหรับ %q: %v",
	MsgSettingsMgrMissing:  "ตัวจัดการการตั้งค่าไม่พร้อมใช้งาน",

	// Audit.
	MsgAuditNotConfigured: "ยังไม่ได้กำหนดค่าการบันทึกการตรวจสอบ",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "ไม่อนุญาตให้เปิด/ปิดใช้งาน",
	MsgUnitCannotStopController:    "ไม่สามารถหยุด systemcontroller ได้",
	MsgUnitInvalidLines:            "พารามิเตอร์ lines ไม่ถูกต้อง",
	MsgUnitInvalidSince:            "พารามิเตอร์ since ไม่ถูกต้อง",
	MsgUnitInvalidUntil:            "พารามิเตอร์ until ไม่ถูกต้อง",
	MsgUnitInvalidPriority:         "พารามิเตอร์ priority ไม่ถูกต้อง",

	// Repository management.
	MsgRepoInvalidURL: "URL ไม่ถูกต้อง",

	// Pages management.
	MsgPagesNotConfigured:    "ยังไม่ได้กำหนดค่า Pages",
	MsgPagesGitNotConfigured: "ยังไม่ได้กำหนดค่าไคลเอนต์ git หรือไดเรกทอรี Pages",

	// Package installation.
	MsgInstallNoRepoRoot:      "ยังไม่ได้กำหนดค่ารากของที่เก็บ",
	MsgInstallSummaryUpgrade:  "อัปเกรด %s จาก %s เป็น %s",
	MsgInstallSummaryInstall:  "ติดตั้ง %s %s",
	MsgInstallSummaryImage:    "อิมเมจ: %s",
	MsgInstallSummaryVolumes:  "%d โวลุ่ม",
	MsgInstallSummaryNewVols:  "ใหม่ %d",
	MsgInstallSummaryMigrated: "ย้ายแล้ว %d",
	MsgInstallSummaryNoVols:   "ไม่มีโวลุ่ม",
	MsgInstallSummaryPorts:    "พอร์ตภายนอก: %s",
	MsgInstallSummaryConfig:   "ต้องมีการกำหนดค่า",
	MsgInstallSummaryVMImage:  "อิมเมจ VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "ต้องระบุ repo, name และ version",
	MsgManifestNotFound:       "ไม่พบไฟล์ประกาศแพ็กเกจ: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:   "ต้องระบุ repo, name และ version",
	MsgRebuildRepoNotConfigured: "ยังไม่ได้กำหนดค่ารากของที่เก็บ",
	MsgRebuildGitNotConfigured:  "ยังไม่ได้กำหนดค่าไคลเอนต์ git",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "ต้องระบุฟิลด์ subvolume",
	MsgArchiveFileRequired:      "ต้องมีไฟล์อาร์ไคฟ์: %v",
	MsgArchiveUnsupportedFormat: "รูปแบบการดาวน์โหลดที่ไม่รองรับ: %s",
	MsgArchiveUnpackSuccess:     "แตกอาร์ไคฟ์สำเร็จ",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "ยังไม่ได้กำหนดค่าไดเรกทอรี Pages",
	MsgPagesNameRequired:           "ต้องระบุฟิลด์ name",
	MsgPagesUploadArchiveOnly:      "อนุญาตให้อัปโหลดได้เฉพาะ Pages ประเภทอาร์ไคฟ์เท่านั้น",
	MsgPagesArchiveRebuildRequired: "ต้องสร้าง Pages ประเภทอาร์ไคฟ์ใหม่โดยการอัปโหลดอาร์ไคฟ์ใหม่ผ่าน /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "ยังไม่ได้กำหนดค่าการเฝ้าติดตาม",

	// Upgrades.
	MsgUpgradeSettingsMissing: "ตัวจัดการการตั้งค่าไม่พร้อมใช้งาน",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:        "สร้างระบบไฟล์",
	MsgAuditModifyFilesystem:        "แก้ไขระบบไฟล์",
	MsgAuditRemoveFilesystem:        "ลบระบบไฟล์",
	MsgAuditAddRepository:           "เพิ่มที่เก็บ",
	MsgAuditRemoveRepository:        "ลบที่เก็บ",
	MsgAuditMoveRepository:          "ย้ายที่เก็บ",
	MsgAuditRefreshRepositories:     "รีเฟรชที่เก็บ",
	MsgAuditInstallPackage:          "ติดตั้งแพ็กเกจ",
	MsgAuditUninstallPackage:        "ถอนการติดตั้งแพ็กเกจ",
	MsgAuditPurgeUninstalledVolumes: "ล้างโวลุ่มที่ถอนการติดตั้งแล้ว",
	MsgAuditPurgeVolumes:            "ล้างโวลุ่ม",
	MsgAuditDisablePackage:          "ปิดใช้งานแพ็กเกจ",
	MsgAuditEnablePackage:           "เปิดใช้งานแพ็กเกจ",
	MsgAuditSetUnitStatus:           "ตั้งค่าสถานะยูนิต",
	MsgAuditCreateAccount:           "สร้างบัญชี",
	MsgAuditUpdateAccount:           "อัปเดตบัญชี",
	MsgAuditDisableAccount:          "ปิดใช้งานบัญชี",
	MsgAuditAuthenticate:            "ยืนยันตัวตน",
	MsgAuditRevokeSession:           "เพิกถอนเซสชัน",
	MsgAuditUpdateSetting:           "อัปเดตการตั้งค่า",
	MsgAuditDismissUpgrades:         "ปิดการแจ้งอัปเกรดแพ็กเกจ",
	MsgAuditUploadArchive:           "อัปโหลดอาร์ไคฟ์",
	MsgAuditDownloadArchive:         "ดาวน์โหลดอาร์ไคฟ์",
	MsgAuditCreatePage:              "สร้างเพจ",
	MsgAuditUpdatePage:              "อัปเดตเพจ",
	MsgAuditRemovePage:              "ลบเพจ",
	MsgAuditRebuildPage:             "สร้างเพจใหม่",
	MsgAuditUploadPageArchive:       "อัปโหลดอาร์ไคฟ์เพจ",
	MsgAuditEnableAccount:           "เปิดใช้งานบัญชี",
	MsgAuditRebuildGit:              "สร้าง git ใหม่",
	MsgAuditUploadVMImage:           "อัปโหลดอิมเมจ VM",
	MsgAuditDeleteVMImage:           "ลบอิมเมจ VM",
	MsgAuditAddDNSRecord:            "เพิ่มระเบียน DNS",
	MsgAuditRemoveDNSRecord:         "ลบระเบียน DNS",
	MsgAuditSetDNSTLD:               "ตั้งค่า DNS TLD",
	MsgAuditSetupDNS:                "ตั้งค่า DNS",
	MsgAuditRemovePackageVolume:     "ลบโวลุ่มแพ็กเกจ",
	MsgAuditRemovePackageVolumeGroup: "ลบกลุ่มโวลุ่มแพ็กเกจ",
	MsgAuditClearLastResponses:      "ล้างคำตอบการติดตั้งที่แคชไว้",
	MsgAuditSetSystemServiceStatus:  "ตั้งค่าสถานะบริการระบบ",
	MsgAuditRefreshSystemServices:   "รีเฟรชบริการระบบ",
	MsgAuditCreateNetwork:           "สร้างเครือข่าย",
	MsgAuditRemoveNetwork:           "ลบเครือข่าย",
	MsgAuditEnableNetwork:           "เปิดใช้งานเครือข่าย",
	MsgAuditDisableNetwork:          "ปิดใช้งานเครือข่าย",
	MsgAuditAddNetworkPeer:          "เพิ่มเพียร์เครือข่าย",
	MsgAuditRemoveNetworkPeer:       "ลบเพียร์เครือข่าย",
	MsgAuditRefreshNetworkPeer:      "รีเฟรชเพียร์เครือข่าย",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "บัญชีนี้สามารถใช้ได้เฉพาะปลายทางการลงทะเบียนเครือข่ายและที่เก็บอ็อบเจ็กต์เท่านั้น",
	MsgAuthNetworkOnlyNetworkDenied: "บัญชีนี้ไม่ได้รับอนุญาตบนเครือข่ายนั้น",
	MsgAuthWireGuardPeerNotOwned:  "บัญชีนี้สามารถรีเฟรชได้เฉพาะเพียร์ที่ตนลงทะเบียนไว้เท่านั้น",
	MsgAuthObjectStorageRequired:  "ต้องมีสิทธิ์ผู้ดูแลระบบหรือสิทธิ์ที่เก็บอ็อบเจกต์",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "การอัปโหลดและดาวน์โหลดอาร์ไคฟ์ไม่สามารถระบุพาร์ทิชันที่เก็บอ็อบเจกต์ได้",
	MsgGfehNotConfigured:         "ยังไม่ได้ตั้งค่าที่เก็บอ็อบเจกต์",
	MsgGfehNameRequired:          "ต้องระบุฟิลด์ชื่อ",
	MsgGfehPartitionExists:       "มีพาร์ทิชันนี้อยู่แล้ว",
	MsgGfehPartitionNotFound:     "ไม่พบพาร์ทิชัน",
	MsgGfehNetworkRequired:       "ต้องระบุฟิลด์เครือข่าย",
	MsgGfehPrincipalRequired:     "ต้องระบุฟิลด์ผู้ใช้สิทธิ์",
	MsgGfehPathRequired:          "ต้องระบุฟิลด์เส้นทาง",
	MsgGfehUnknownAccount:        "ไม่มีบัญชีดังกล่าว",
	MsgAuditCreateGfehPartition:  "สร้างพาร์ทิชันที่เก็บอ็อบเจกต์",
	MsgAuditModifyGfehPartition:  "แก้ไขพาร์ทิชันที่เก็บอ็อบเจกต์",
	MsgAuditRemoveGfehPartition:  "ลบพาร์ทิชันที่เก็บอ็อบเจกต์",
	MsgAuditAddGfehPrincipal:     "เพิ่มผู้ใช้ที่เก็บอ็อบเจกต์",
	MsgAuditRemoveGfehPrincipal:  "ลบผู้ใช้ที่เก็บอ็อบเจกต์",
	MsgAuditAddGfehGrant:         "เพิ่มสิทธิ์ที่เก็บอ็อบเจกต์",
	MsgAuditRevokeGfehGrant:      "เพิกถอนสิทธิ์ที่เก็บอ็อบเจกต์",
	MsgAuditWithdrawGfehExposure: "ถอนลิงก์ที่เก็บอ็อบเจกต์",
}

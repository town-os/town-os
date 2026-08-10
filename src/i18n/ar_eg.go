package i18n

// arEGOverrides holds the strings where Egyptian usage departs from the
// Gulf-standard Modern Standard Arabic of ar_sa.go.
//
// Egyptian Arabic and the Arabic of the Gulf differ enormously in speech and
// almost not at all in writing: a control panel is written in MSA everywhere,
// and MSA is the same language in Cairo and Riyadh. The exception is the pair
// of verbs for moving a file, where written usage really did split. Egypt (and
// the Levant generally) writes تحميل for downloading; the Gulf and most
// standards bodies reserve تحميل for uploading and use تنزيل for downloading.
// ar_sa.go follows the latter. Following it in Egypt would name the wrong
// direction, so these six strings switch.
var arEGOverrides = map[string]string{
	MsgArchiveUnsupportedFormat: "تنسيق التحميل غير مدعوم: %s",
	MsgAuditDownloadArchive:     "تحميل أرشيف",
	MsgArchiveGfehRefused:       "لا يمكن لعمليات رفع وتحميل الأرشيف استهداف قسم تخزين الكائنات",
}

// arEGMessages is ar-SA with the Egyptian departures applied.
var arEGMessages = derive(arSAMessages, arEGOverrides)

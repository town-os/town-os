import arSA from './ar-SA.js'
import derive from './derive.js'

/**
 * Egyptian Arabic and the Arabic of the Gulf differ enormously in speech and
 * almost not at all in writing — a control panel is written in Modern Standard
 * Arabic everywhere, and MSA is the same language in Cairo and Riyadh.
 *
 * The exception is the pair of verbs for moving a file, where written usage
 * genuinely split. Egypt and the Levant write تحميل for downloading; the Gulf
 * and most standards bodies reserve تحميل for uploading and use تنزيل for
 * downloading. ar-SA.js follows the latter, so in Egypt it names the wrong
 * direction. These are the download-sense strings, switched.
 *
 * Note what is *not* here: تحميل in the sense of "loading" — جارٍ التحميل — is
 * standard everywhere and is left alone. It does collide with the Egyptian word
 * for download, and that collision is real Egyptian usage rather than something
 * this catalog should invent its way out of.
 */
export const arEGOverrides = {
  'storage.download_archive_label': 'تحميل الأرشيف',
  'storage.toast_archive_downloaded': 'تم تحميل الأرشيف',
  'archive.download_title': 'تحميل الأرشيف',
  'archive.filename_hint': 'الاسم الأساسي للملف الذي يتم تحميله. تُضاف امتداد الأرشيف تلقائيًا.',
  'archive.stop_service_download': 'إيقاف الخدمة أثناء التحميل',
  'archive.download_btn': 'تحميل',
  'dns.bl.rbl_description':
    'مناطق قائمة الثقب الأسود اللحظية التي يُستعلَم عنها عند الطلب بعنوان IP معكوس (مثال: zen.spamhaus.org). لا يتم تحميل أو تخزين أي شيء.',
  'dns.bl.dnsbl_description':
    'مناطق قائمة حظر النطاقات التي يُستعلَم عنها عند الطلب باسم النطاق (مثال: dbl.spamhaus.org). تكون لها الأولوية على الإجابات الخارجية؛ لا يتم تحميل أو تخزين أي شيء.',
  'progress.downloading_vm_image': 'جارٍ تحميل صورة الجهاز الافتراضي...',
}

/** Arabic (Egypt) — ar-SA with the Egyptian download verb. */
const arEG = derive(arSA, arEGOverrides)

export default arEG

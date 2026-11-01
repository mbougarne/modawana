# Licensing / التراخيص

[English](#english) · [العربية](#العربية)

## English

The application is licensed under [MIT](../LICENSE). Fonts and dependencies retain
their original licenses; MIT does not relicense them or an owner's uploaded content.
The current application does not include Gatsby code or a Gatsby dependency.

Keep the original OFL notices in `ui/src/fonts/LICENSES/`. After dependency
installation and `go mod download all` in `backend`, run
`node scripts/release/notices.mjs`. It collects upstream license/notice files,
declared npm license metadata and hashes under `build/notices`. Release packages
include these notices, the fonts, Go's license, and the application MIT license.
The Docker build also collects dependency notices. Review upstream terms when
changing dependencies. Third-party originals are preserved without translation.

## العربية

يخضع التطبيق لترخيص [MIT](../LICENSE). وتحتفظ الخطوط والمكتبات والتبعيات البرمجية بتراخيصها الأصلية؛ حيث لا يُغير ترخيص MIT شروط تراخيص تلك المشاريع، كما لا يسري على محتوى المالك المنشور أو وسائطه المرفوعة. ولا يتضمن التطبيق الحالي أي كود مأخوذ من إطار عمل Gatsby أو أي تبعية برمجية تعتمد عليه.

احفظ إشعارات تراخيص الخطوط الأصلية (OFL) في المجلد `ui/src/fonts/LICENSES/`. وبعد تثبيت التبعيات وتشغيل الأمر `go mod download all` داخل مجلد `backend`، شغّل الأمر: `node scripts/release/notices.mjs`. يتولى هذا السكربت جمع ملفات التراخيص والإشعارات للمشاريع المصدرية، وبيانات تراخيص حزم npm، وبصمات التجزئة الرقمية داخل المجلد `build/notices`. وتتضمن حزم الإصدارات هذه الإشعارات، وتراخيص الخطوط، وترخيص Go، إلى جانب ترخيص MIT الخاص بالتطبيق. كما تجمع عملية بناء صورة Docker إشعارات التبعيات تلقائياً. احرص على مراجعة شروط التراخيص الأصلية عند تغيير أي تبعيات برمجية، مع الاحتفاظ بنصوص تراخيص الطرف الثالث الأصلية كاملة دون ترجمة.

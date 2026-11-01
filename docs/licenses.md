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

يخضع التطبيق لـ[MIT](../LICENSE). تحتفظ الخطوط والاعتماديات بتراخيصها الأصلية؛
لا يعيد MIT ترخيصها أو ترخيص محتوى المالك المرفوع. لا يتضمن التطبيق الحالي كود
Gatsby أو اعتماداً عليه.

احفظ إشعارات OFL الأصلية في `ui/src/fonts/LICENSES/`. بعد تثبيت الاعتماديات
و`go mod download all` داخل `backend` شغّل `node scripts/release/notices.mjs`.
يجمع إشعارات وتراخيص المصدر وبيانات ترخيص npm والبصمات في `build/notices`.
تتضمن حزم الإصدار الإشعارات وتراخيص الخطوط وGo وMIT للتطبيق. يجمع بناء Docker
إشعارات الاعتماديات أيضاً. راجع الشروط عند تغيير الاعتماديات؛ تحفظ أصول تراخيص
الأطراف الأخرى دون ترجمة.

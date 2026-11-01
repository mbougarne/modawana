# المساهمة

[English](CONTRIBUTING.md)

ابدأ بـ[الإعداد المحلي](docs/getting-started.ar.md) و[البنية](docs/architecture.ar.md)
و[تعليمات المساهمة](AGENTS.ar.md). اجعل التغيير مركزاً، واشرح المشكلة والسلوك
الناتج والتحقق والحدود في طلب الدمج. ناقش نطاق التغييرات الكبيرة في issue أولاً.
لا تضف أسراراً أو محتوى إنتاجياً أو بيانات تشغيل أو بناءً مولداً أو ملاحظات خاصة.

## الفحوص

استخدم Go 1.27.3 وNode 22.23.4 وnpm 10.9.2:

```sh
node scripts/project.mjs install
node scripts/project.mjs format
node scripts/project.mjs check --race
node scripts/project.mjs build
(cd backend && go mod verify)
node scripts/smoke-production.mjs --http-only
```

يشغّل `check` فحوص ESLint وPrettier وgofmt وروابط التوثيق وتغطية اللغات والملفات
المولدة وTypeScript واختبارات الواجهة وGo وgo vet. استخدم اختبارات مركزة للتفويض
والتخزين والتزامن والفشل. تستعمل الاختبارات بيانات مؤقتة ولا تستهدف تثبيتاً حياً.
تفحص CI أيضاً الاعتماديات والتغليف وإعادة التشغيل. فحوص المتصفح محلية اختيارية؛
لا تثبت CI المتصفحات ولا تشغّلها.

```sh
npx --prefix frontend playwright install --with-deps chromium
node scripts/smoke-production.mjs
node scripts/smoke-auth.mjs
```

تحتاج بناء الإنتاج وإذن ربط المنافذ المحلية. لا تثبت WebAuthn الافتراضية توافق
العتاد. راجع [فحوص الإصدار](docs/release-readiness.ar.md).

## الكود والتوثيق

اجعل الاستيرادات قبل التصريحات وافصل التصديرات. استخدم الأقواس في التحكم بالتدفق.
استخدم المكونات والرموز المشتركة وCSS المنطقية ونصوص `t()` باللغات الثلاث.
اتبع [التصميم](docs/design.ar.md) لتوليد الرموز والنصوص ومتجهات الألوان.

أبق `/api/v1` وOpenAPI والتحقق في Go وعقود TypeScript متوافقة. الترحيلات إضافية؛
لا تعدّل ترحيلاً مطبقاً أو بصمته. تحتاج التغييرات الكاسرة في API والأرشيف
إرشادات إصدار وترحيل صريحة واختبارات.

يجب أن يتوفر التوثيق الجديد أو المعدل جوهرياً بالعربية والإنجليزية بمعنى متكافئ،
في ملفين مرتبطين أو أقسام كاملة. صف السلوك والإجراءات الحالية لا المحادثات أو
يوميات التنفيذ. أعد توليد الجرد عبر `node scripts/check-docs.mjs --write` ثم افحصه عادياً.

## تغليف الإصدارات

بعد الفحوص والبناء:

```sh
(cd backend && go mod download all)
node scripts/release/notices.mjs
node scripts/release/package.mjs VERSION
node scripts/release/smoke-package.mjs
```

استخدم اسم إصدار غير مستخدم. تحتوي الحزمة ملفات تنفيذية أصلية لمنصة البناء
وأصول الواجهة وأدلة التشغيل والإشعارات وجرد SHA-256. اختبر كل منصة وصورة مستقلة؛
برنامج macOS ليس برنامج Linux لـVPS. التغليف لا ينشر تلقائياً.
راجع [التراخيص](docs/licenses.md) و[الإبلاغ الأمني](SECURITY.ar.md).

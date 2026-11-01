# الواجهة

[English](README.md) · [المشروع](../README.ar.md)

تشغّل React وTypeScript وWebpack القارئ العام واللوحة الخاصة. يستورد المصدر
المشترك من `ui/src` الجذري عبر `@ui` بنسخة React واحدة وملف قفل npm واحد.
تستخدم العربية RTL والإنجليزية والفرنسية المكونات نفسها.

شغّل `node scripts/project.mjs dev` من الجذر لخادمي Go وWebpack.
راجع [الإعداد المحلي](../docs/getting-started.ar.md). تضم حزم الإنتاج أصولاً
مبصومة وبياناً ويخدمها Go من أصل API نفسه.

```sh
npm --prefix frontend run check
node scripts/project.mjs build
```

تفحص الأوامر أنواع التطبيق والواجهة المشتركة واختبارات DOM والعقود.
تشرح [المساهمة](../CONTRIBUTING.ar.md) فحوص المتصفح الاختيارية وحدودها.
اقرأ [التصميم](../docs/design.ar.md) و[عقود الواجهة](../docs/ui-contracts.ar.md)
و[الصفحات العامة](../docs/public-pages.ar.md) و[أدوات المالك](../docs/owner-tools.ar.md).
لا تضمّن أسرار التشغيل في المتصفح ولا تعتبر إخفاء الواجهة تفويضاً.

# التطوير المحلي

[English](getting-started.md) · [التوثيق](README.ar.md)

استخدم الإصدارات المثبتة في `.node-version` و`frontend/package.json` و`backend/go.mod`.
نفذ أوامر `scripts/project.mjs` من الجذر. يشغّل `install` الأمر `npm ci` داخل
`frontend`؛ يجلب Go الوحدات عند البناء أو الاختبار.

```sh
node scripts/project.mjs install
export BLOG_DATA_ROOT="$HOME/.local/share/personal-blog-dev"
node scripts/project.mjs dev
```

يستمع Webpack على `127.0.0.1:3000` ويوجه API والوسائط إلى Go على `127.0.0.1:8082`.
افتح `http://localhost:3000/ar` أو `/en` أو `/fr`. يوقف Ctrl+C عمليتي التطوير.
اجعل جذر البيانات مطلقاً وخاصاً وخارج المستودع. دون `BLOG_DATA_ROOT` يستخدم المشغل
مجلداً مؤقتاً خاصاً بالمستودع قد يمسحه النظام. استخدم جذر البيانات نفسه في كل طرفية.

## المالك الأول

شغّل `node scripts/project.mjs bootstrap` في طرفية ثانية. أدخل التصريح أحادي
الاستخدام الصالح لعشر دقائق في نموذج الإعداد المحلي/الاسترداد بصفحة الدخول مع
بريدك الخاص. سجّل مفتاح مرور ثم ادخل به. لا يُزرع مالك أو محتوى.
يدخل المالك الموجود أو يسترد وصوله؛ لا يستبدل bootstrap حساباً موجوداً.

يدعم Chromium تدفق التطوير على HTTP في `localhost`؛ يختلف سلوك المتصفحات.
ثبّت اسم المضيف لأن `localhost` و`127.0.0.1` أصلان مختلفان. إذا تعذر التسجيل
في المتصفح أو المزود، استخدم HTTPS الموثوق أدناه.

للتجربة المحلية الافتراضية في Chrome افتح DevTools ← More tools ← WebAuthn.
فعّل البيئة الافتراضية وأضف مصادق CTAP2 داخلياً يدعم المفاتيح المقيمة والتحقق
من المستخدم. أبق التحقق والحضور التلقائي مفعّلين وDevTools مفتوحاً أثناء التسجيل
والدخول. هذا عامل محاكى لا يثبت توافق الهاتف أو المزود أو المفاتيح العتادية.

## HTTPS محلي موثوق

أوقف تطوير HTTP أولاً. بعد تثبيت `mkcert` أنشئ شهادة localhost موثوقة خارج Git.
قد يحتاج تثبيت سلطة الشهادات المحلية إذن نظام التشغيل.

```sh
mkdir -p /tmp/personal-blog-local-tls
chmod 700 /tmp/personal-blog-local-tls
mkcert -install
mkcert -cert-file /tmp/personal-blog-local-tls/cert.pem -key-file /tmp/personal-blog-local-tls/key.pem localhost 127.0.0.1
export BLOG_DEV_TLS_CERT=/tmp/personal-blog-local-tls/cert.pem
export BLOG_DEV_TLS_KEY=/tmp/personal-blog-local-tls/key.pem
node scripts/project.mjs dev:https
```

افتح `https://localhost:3000/ar/login` واستخدم `node scripts/project.mjs bootstrap:https`
في الطرفية الثانية بجذر البيانات نفسه. يتحقق المشغل من المفتاح والشهادة قبل البدء؛
لا يثبت سلطة شهادات ولا يضعف ملفات ارتباط الإنتاج.

## الاسترداد والمشكلات الشائعة

- عند فقد عامل التطوير أو إنشاء الحساب دون إتمام التسجيل، شغّل
  `node scripts/project.mjs recover` أو `recover:https` لـHTTPS، وحدّث صفحة
  الدخول وأدخل التصريح الجديد مع **البريد نفسه**. يلغي الاسترداد الجلسات
  والتدفقات المعلقة ويحفظ المحتوى. سجّل العامل البديل واختبره.
- ينتهي التصريح بعد عشر دقائق ويستخدم مرة واحدة. لا تضعه في الروابط أو السجلات
  أو الصور أو المحادثات أو معاملات الأوامر.
- تغيير RP ID أو النطاق لا ينقل المفاتيح. اتبع [الاسترداد](owner-security.ar.md)
  ولا توسّع الأصول المسموحة.
- إن كان المنفذ مستخدماً فأوقف عملية التطوير السابقة. يحدد `BLOG_LISTEN_ADDR`
  عنواناً ومنفذاً آخر للخلفية ويتبعه وكيل التطوير.

للإنتاج اتبع [دليل النشر](deployment.ar.md). شهادات التطوير والمصادقات الافتراضية
أدوات اختبار وليست إعداد أمان إنتاجياً.

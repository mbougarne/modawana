# API المالك

[English](owner.md) · [فهرس API](README.ar.md) · [دليل التشغيل](../owner-security.ar.md)

المسارات التالية نسبية إلى `/api/v1`. تستخدم JSON سياسة `no-store` وغلاف الأخطاء
المشترك. حد الإدخال 64 KiB. الأجسام التي تحتوي مدخلات وغير الخاصة بـ WebAuthn كائنات محددة الحقول؛
تستخدم WebAuthn JSON القياسي للمكتبة. لا يعاد مسار فعلي أو مفتاح خاص أو سر ارتباط أو بذرة مشفرة.

| الطريقة/المسار | الإدخال ← حمولة النجاح | الصلاحية |
| --- | --- | --- |
| POST /auth/start | `{}` ← `{state:"anonymous",csrf}` وارتباط مقيد | أصل مطابق و`X-Blog-Request: 1` دون CORS |
| GET /session | `{state,csrf?,session?}`؛ الحالة anonymous/bootstrap/recovery/password/managed/owner | الارتباطات الحالية؛ لا ينشئ تصريحاً |
| POST /auth/grant | `{grant,email}` ← `{state:"enrollment",csrf}` وارتباط مقيد جديد | تدفق مجهول وCSRF وتصريح محلي أحادي |
| POST /auth/passkeys/login/options | `{}` ← `{publicKey:…}` | تدفق مجهول وCSRF |
| POST /auth/passkeys/login/verify | JSON إثبات المتصفح ← `{state:"owner",csrf}` | تحدٍ مطابق أحادي؛ يدوّر ارتباط المالك |
| POST /auth/passkeys/register/options | `{}` ← `{publicKey:…}` | تدفق تهيئة/استرداد أو مالك حديث مع تدفق مجهول |
| POST /auth/passkeys/register/verify | JSON تسجيل المتصفح؛ `X-Passkey-Label` هو encodeURIComponent(name) ← `{state:"sign_in_to_test"}` | التدفق المرتبط نفسه ومصادقة حديثة ثانية للإضافة؛ لا جلسة مالك من التسجيل |
| POST /session | `{email,password}` ← `{state:"password",csrf}` | تدفق مجهول وCSRF؛ دون ارتباط كامل |
| POST /session/totp | `{code}` ← `{state:"owner",csrf}` | تدفق كلمة مرور معلّق وCSRF |
| POST /session/recovery | `{code}` ← `{state:"owner" or "recovery",csrf}` | كلمة مرور معلقة عند وجود البديل، وإلا إعادة تسجيل مقيدة فقط |
| DELETE /session | `{}` ← `{ok:true}` | مالك وCSRF؛ يلغي الارتباط الحالي |
| GET /security/passkeys | `[{id,label,created_at,last_used_at,tested_at}]` | المالك |
| PATCH /security/passkeys | `{id,label}` ← `{ok:true}` | مالك حديث وCSRF |
| DELETE /security/passkeys | `{id}` ← `{ok:true}` | مالك حديث وCSRF ووسيلة أخرى مجربة؛ يلغي الجلسات |
| GET /security/factors | `{totp_enabled,totp_available,login_email,recovery_codes_remaining}` | المالك؛ البريد الخاص ليس بيانات ملف عام |
| POST /security/totp/options | `{password}` ← `{secret}` وارتباط تسجيل مقيد | مالك حديث وCSRF ومفتاح خارجي مضبوط |
| POST /security/totp/verify | `{code}` ← `{state:"sign_in"}` | المالك والجلسة نفسيهما وبذرة مشفرة معلقة؛ يفعّل ويلغي الجلسات |
| DELETE /security/totp | `{}` ← `{ok:true}` | مالك حديث وCSRF ووسيلة أخرى مجربة |
| POST /security/recovery-codes | `{}` ← `{codes:[10 strings]}` مرة واحدة | مالك حديث وCSRF؛ يستبدل الرموز القديمة |
| GET /security/sessions | `[{id,method,authenticated_at,created_at,expires_at}]` | المالك؛ حتى 100 جلسة نشطة |
| DELETE /security/sessions | `{id?}` ← `{ok:true}`؛ غياب id أو فراغه يلغي الجميع | مالك حديث وCSRF |
| GET /security/events | `[{event,request_id,created_at}]` | المالك؛ أحدث 100 من آخر 1000 حدث محفوظ |

تتطلب كل كتابة بعد `/auth/start` الأصل المطابق و`X-CSRF-Token` المرتبط بارتباطها.
يستخدم الأمان والخروج CSRF المالك وتستخدم المراسم CSRF التدفق المقيد. لا تنشئ
طلبات GET العامة حالة دخول. تصف `state` المصادقة فقط ولا تثبت وجود نقاط المحتوى
والنشر المستقبلية. تعاد مراجعة حداثة الخمس دقائق داخل معاملات التغييرات الحساسة.

401 إخفاق مصادقة عام و403 منع/حاجة لمصادقة حديثة و409 تعارض عامل و429 تقييد
(Retry-After: 900) و503 عدم توفر مفتاح المشغل/الخدمة و500 فشل داخلي. تفشل حمولات
المصادقة غير السليمة عموماً دون كشف وجود بريد أو عامل. الرد الفاشل أو المفقود لا
يثبت نجاح التغيير؛ أعد تحميل الحالة قبل المحاولة. التحدي أحادي حتى عند فشل التحقق
وينتهي بعد خمس دقائق. تبقى الرموز والبذور بحالة واجهة مؤقتة ولا تدخل localStorage.

## توضيح إجراء المالك

قد يعيد الحساب والأمان `error.fields: [{path,code}]` بمسارات آمنة:
`security.label` و`security.password` و`security.code` و`security.passkeys`
و`security.request` و`security.token_*`. تشرح الأسباب حدود الأسماء وكلمات المرور
وخطأ إعداد المصادقة أو انتهاءه وقيود الرموز وشروط آخر عامل ومفتاح المرور المختبر.
للإدخال 422 ولمنع حذف العامل 409 وللكتابة غير المؤكدة 403. تبقى فحوص الجلسة
والحداثة وحد الطلبات والمعاملات وأخطاء الدخول المجهول الموحدة. لا تُعاد كلمة مرور
أو بذرة أو رمز أو معرّف اعتماد في أخطاء التحقق.

### رسائل مراجعة التقييم

يعيد غياب مراجعة التقييم المحدد HTTP 422 بالرمز `invalid_fields` وسبب حقل آمن
`{ "path": "quiz", "code": "assessment_review" }`. تحتفظ أسباب حقول المجموعات
بمساراتها ورموزها الأدق. تبقى تجربة الاختبار ومراجعته محميتين بالمالك وOrigin وCSRF
على المراجعة المحفوظة الدقيقة؛ لا تنشران المحتوى ولا تسجلان تقدم المتصفح. يبقى شرط
الفتح المحفوظ خاصاً حتى تفعيل خطة المسار صراحة.

# Optional lesson scheduler / مجدول الدروس الاختياري

[English](#english) · [العربية](#العربية)

## English

The publisher includes an optional scheduler for generated lessons, independent of
interactive [MCP/OAuth](mcp-oauth.md). Normal blogging does not require this service.
The checked-in [schedule example](../../deploy/lesson-schedule.example.json) is disabled;
replace its placeholders and review continuity before enabling any job.

```sh
build/publisher schedule --input /private/schedule.json --state-dir /private/state
```

Supply `BLOG_URL`, `BLOG_PUBLISHER_TOKEN`, `OPENAI_API_KEY`, and `NOTION_API_KEY`
privately to this process. These are separate credentials; a blog token is not an
OpenAI key, and a chat connection is not a Notion REST credential. No real secrets
are included. Keep runtime state, generated lesson manifests and credentials outside
Git and web assets. Back up state encrypted; back up credentials separately.

Jobs use unique IDs, existing collection keys, source locale and accessible Notion
parents. Times are explicit UTC. Start date/number define calendar numbering for
selected weekdays; positions equal lesson numbers. Begin with `auto_publish:false`.
Translations, assessment reviews and path activation retain the normal owner rules.

Generation persists source manifests atomically before delivery. Retries reuse them;
completed destinations are not repeated. Later lessons use prior saved Markdown,
bounded to 48000 bytes. Missing/oversized continuity and missed dates without a
manifest require reconciliation. Do not run overlapping chat and VPS schedules.

The generator uses the Responses endpoint with `store:false`, structured output,
no tools/redirects and bounded time/output. Only the models/prices implemented in
`backend/internal/publisher` are accepted. Monthly budgets use integer micro-USD.
Before each generation attempt the runner reserves a conservative input/output
allowance; reservations are not refunded, including ambiguous failures. At most two
generation attempts are made per lesson. This local budget is not an account-wide
API spending cap; validate the implemented price assumptions before enabling it.

The global scheduler lock and per-lesson delivery lock survive crashes. Inspect
remote outcomes before removing stale locks. Uncertain Notion creates remain
reconciliation-only. A run retries at most ten saved unfinished manifests and has
a ten-minute bound. See [delivery behavior](lesson-delivery.md).

### Linux service

1. Build/test a Linux publisher binary. Install it at the path in
   [blog-lessons.service](../../deploy/blog-lessons.service); do not copy a macOS binary.
2. Create the private runner directory and state directory owned by UID/GID 65532,
   mode 0700. Store schedule.json with mode 0600 and a separate root-owned 0600
   environment file at the service's configured path. Inspect all absolute paths.
3. Install the service and [timer](../../deploy/blog-lessons.timer), reload systemd,
   and leave the timer disabled while checking the configuration.
4. Test from the actual service environment: one real lesson, both destinations,
   denied scope, one-destination failure and retries without duplicates. Check
   numbering and existing destination content before cutover.
5. Enable the timer only after verification. It invokes the bounded command every
   minute; the configured jobs determine when delivery is due. Stop it during
   upgrades, rollback or reconciliation. Blog backups do not include runner state.

Follow [deployment](../deployment.md) and [recovery](../operations/recovery.md) for
pre-upgrade backups with the old binary and matching-version restores. An automated
server test does not establish live account, network, billing or destination access.

## العربية

يتضمن الناشر مجدولاً اختيارياً لتوليد الدروس، مستقلاً عن [MCP/OAuth](mcp-oauth.md)
التفاعلي. لا تحتاج الكتابة العادية هذه الخدمة. [مثال الجدول](../../deploy/lesson-schedule.example.json)
معطّل؛ استبدل قيمه المؤقتة وراجع الاستمرارية قبل تفعيل أي مهمة.

```sh
build/publisher schedule --input /private/schedule.json --state-dir /private/state
```

مرر `BLOG_URL` و`BLOG_PUBLISHER_TOKEN` و`OPENAI_API_KEY` و`NOTION_API_KEY` سراً
لهذه العملية. هي اعتمادات منفصلة؛ رمز المدونة ليس مفتاح OpenAI، واتصال المحادثة
ليس اعتماد REST لـNotion. لا توجد أسرار فعلية مرفقة. أبق الحالة وبيانات الدروس
والاعتمادات خارج Git وأصول الويب. انسخ الحالة مشفرة والاعتمادات منفصلة.

تستخدم المهام معرّفات فريدة ومجموعات موجودة ولغة مصدر وصفحات أم يمكن الوصول
إليها. الوقت UTC صريح. يحدد تاريخ ورقم البداية الترقيم التقويمي للأيام المختارة؛
تساوي المواضع أرقام الدروس. ابدأ بـ`auto_publish:false`. تبقى الترجمات ومراجعات
التقييم وتفعيل المسار خاضعة لقواعد المالك.

يحفظ التوليد بيانات المصدر ذرياً قبل التوصيل وتعيد المحاولات استخدامها دون
تكرار الوجهات المكتملة. تستعمل الدروس اللاحقة Markdown السابق بحد 48000 بايت.
تحتاج الاستمرارية المفقودة أو الكبيرة والتواريخ الفائتة دون بيان إلى مصالحة.
لا تشغّل جداول متداخلة من المحادثة وVPS.

يستخدم المولد Responses مع `store:false` ومخرجات منظمة ودون أدوات أو إعادة
توجيه، وبحدود للوقت والمخرجات. لا يقبل إلا النماذج والأسعار المنفذة في
`backend/internal/publisher`. الميزانيات الشهرية أعداد صحيحة بوحدة micro-USD.
يحجز قبل كل محاولة حداً محافظاً للمدخلات والمخرجات ولا يعيده حتى عند الفشل
الغامض. أقصى المحاولات اثنتان لكل درس. ليست الميزانية المحلية سقفاً لحساب API
كله؛ تحقق من افتراضات الأسعار المنفذة قبل التفعيل.

يبقى قفل المجدول وقفل الدرس بعد الانهيار. افحص النتائج البعيدة قبل إزالة قفل قديم.
تبقى إنشاءات Notion غير المؤكدة للمصالحة فقط. يعيد التشغيل عشر مهام محفوظة
غير مكتملة كحد أقصى ومدته القصوى عشر دقائق. راجع [سلوك التوصيل](lesson-delivery.ar.md).

### خدمة Linux

1. ابنِ واختبر ناشراً لـLinux وثبته في مسار [الخدمة](../../deploy/blog-lessons.service)؛
   لا تنسخ برنامج macOS.
2. أنشئ مجلد المشغل والحالة بملكية UID/GID 65532 ووضع 0700. احفظ schedule.json
   بوضع 0600 وملف بيئة منفصلاً بملكية root ووضع 0600 في مسار الخدمة. افحص المسارات المطلقة.
3. ثبّت الخدمة و[المؤقت](../../deploy/blog-lessons.timer) وأعد تحميل systemd
   وأبق المؤقت معطلاً أثناء فحص الإعداد.
4. اختبر من بيئة الخدمة الفعلية درساً حقيقياً والوجهتين ورفض الصلاحيات وفشل
   إحدى الوجهتين والإعادة دون تكرار. تحقق من الترقيم والمحتوى الموجود قبل التحويل.
5. فعّل المؤقت بعد التحقق فقط. يشغّل الأمر المحدود كل دقيقة، وتحدد المهام موعد
   الاستحقاق. أوقفه أثناء الترقية والتراجع والمصالحة. لا تشمل نسخة المدونة حالة المشغل.

اتبع [النشر](../deployment.ar.md) و[الاسترداد](../operations/recovery.md) للنسخ
بالبرنامج القديم والاستعادة بالإصدار المطابق. لا يثبت اختبار الخادم الآلي وصول
الحساب أو الشبكة أو الفوترة أو الوجهة الفعلية.

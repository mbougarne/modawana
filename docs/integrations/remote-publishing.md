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

تتضمن أداة الناشر مجدولاً زمنياً اختيارياً لتوليد الدروس آلياً، يعمل بصورة مستقلة تماماً عن تكامل [MCP/OAuth](mcp-oauth.md) التفاعلي. لا تتطلب عمليات التدوين والنشر المعتادة تشغيل هذه الخدمة. ويُعد [ملف الجدول الزمني النموذجي](../../deploy/lesson-schedule.example.json) المرفق في المستودع معطلاً افتراضياً؛ لذا استبدل كافة قيمه المؤقتة وتحقق من تسلسل واستمرارية الدروس قبل تفعيل أي مهمة مجدولة.

```sh
build/publisher schedule --input /private/schedule.json --state-dir /private/state
```

مرر متغيرات البيئة السرية التالية لهذه العملية البرمجية: `BLOG_URL` و`BLOG_PUBLISHER_TOKEN` و`OPENAI_API_KEY` و`NOTION_API_KEY`. تذكر أن هذه بيانات اعتماد مستقلة ومنفصلة؛ فرمز المدونة ليس مفتاح OpenAI، وربط المحادثة ليس بيانات اعتماد REST لمنصة Notion. ولا يتضمن المستودع أي أسرار حقيقية. احرص على حفظ بيانات حالة التشغيل، وبيانات الدروس المولدة، وبيانات الاعتماد خارج شجرة مستودع Git وخارج مجلدات أصول الويب. وقم بأخذ نسخ احتياطية مشفرة لبيانات الحالة، مع حفظ بيانات الاعتماد بشكل منفصل تماماً.

تستخدم المهام معرفات فريدة، ومفاتيح مجموعات موجودة بالفعل، ولغة مصدر محددة، وصفحات أم في Notion يمكن الوصول إليها برمجياً. وتُحدد الأوقات بتوقيت UTC الصريح. ويحدد تاريخ البداية ورقمه التسلسلي الترقيم الزمني للأيام المختارة في الأسبوع؛ وتتطابق مواضع الدروس مع أرقامها. ابدأ دائماً بضبط الخيار `auto_publish:false`. وتظل ترجمات الدروس، ومراجعة الاختبارات التقييمية، وتفعيل مسارات التعلّم خاضعة لقواعد المالك المعتادة.

تقوم آلية التوليد بحفظ بيانات المصدر ذرياً على القرص قبل محاولة التوصيل؛ وتعيد المحاولات اللاحقة استخدام هذه البيانات دون تكرار الإرسال للوجهات التي اكتمل التوصيل إليها سابقاً بنجاح. وتعتمد الدروس اللاحقة على سياق نصوص Markdown السابقة بحد أقصى قدره 48,000 بايت. ويتطلب فقدان سياق الاستمرارية، أو تجاوز حجمه للحد الأقصى، أو فوات تواريخ النشر المجدولة دون وجود بيان مولد تدخلاً يدوياً لإجراء المطابقة والتسوية. احذر تماماً من تشغيل جداول زمنية متداخلة بين المحادثة وخادم VPS في الوقت نفسه.

تعتمد آلية التوليد على نقطة النهاية Responses مع ضبط الخيار `store:false`، وتوليد مخرجات بنيوية محددة (structured output)، دون أدوات خارجية أو إعادة توجيه، وضمن مهلات زمنية وحدود قصوى للمخرجات. ولا يقبل المشغل سوى النماذج البرمجية والأسعار المنفذة فعلياً في الحزمة `backend/internal/publisher`. وتُحسب الميزانيات الشهرية كأعداد صحيحة بوحدة الميكرو دولار (micro-USD). ويقوم المشغل قبل كل محاولة توليد بحجز مخصص مالي تقديري متحفظ للمدخلات والمخرجات، ولا يُسترد هذا الحجز حتى في حالات الفشل غير المؤكدة. ويقتصر النظام على محاولتي توليد كحد أقصى لكل درس. ولا تُمثل هذه الميزانية المحلية سقفاً لكامل حساب API؛ لذا تحقق من صحة افتراضات الأسعار المنفذة برمجياً قبل تفعيل الخدمة.

يصمد كل من قفل المجدول العام وقفل توصيل الدرس الفردي في حال انهيار العملية فجأة. تحقق من نتائج العمليات على الوجهات البعيدة قبل إزالة أي أقفال قديمة معلقة. وتظل حالات إنشاء الصفحات غير المؤكدة في Notion خاضعة للمطابقة والتسوية اليدوية فقط. وتُعيد دورة التشغيل الواحدة محاولة ما لا يتجاوز 10 مهام محفوظة غير مكتملة، مع حد زمني أقصى يبلغ 10 دقائق للدورة. راجع وثيقة [سلوك توصيل الدروس](lesson-delivery.ar.md).

### إعداد الخدمة على Linux

1. قم ببناء واختبار الملف التنفيذي للناشر الخاص بنظام Linux، وثبّته في المسار المحدد في [ملف الخدمة](../../deploy/blog-lessons.service)؛ ولا تنسخ الملف التنفيذي الخاص بنظام macOS.
2. أنشئ مجلد المشغل ومجلد الحالة بصلاحيات ملكية UID/GID 65532 ونمط أذونات 0700. احفظ ملف `schedule.json` بنمط أذونات 0600، وأنشئ ملف بيئة منفصلاً بملكية root ونمط 0600 في مسار الخدمة المحدد. وتأكد من صحة كافة المسارات المطلقة.
3. ثبّت ملف الخدمة و[ملف المؤقت](../../deploy/blog-lessons.timer)، وأعد تحميل إعدادات systemd، واترك المؤقت معطلاً أثناء فحص وتدقيق الإعدادات.
4. اختبر الخدمة عملياً من بيئة التشغيل الفعلية: قم بتوليد درس تجريبي حقيقي، وتأكد من التوصيل للوجهتين، واختبر رفض الصلاحيات غير الممنوحة، وتعطل إحدى الوجهتين، وإعادة المحاولة بأمان دون تكرار المحتوى. وتحقق من صحة الترقيم التسلسلي والمحتوى الموجود في الوجهتين قبل التحويل الفعلي.
5. لا تُفعّل المؤقت إلا بعد اكتمال التحقق والتأكد التام. يقوم المؤقت بتشغيل الأمر المقيد كل دقيقة، وتتولى المهام المهيأة تحديد وقت استحقاق التوصيل. أوقف المؤقت أثناء إجراء الترقيات، أو التراجع للإصدار السابق، أو أعمال المطابقة. وتذكر أن النسخ الاحتياطية للمدونة لا تشمل بيانات حالة مشغل الدروس.

اتبع إرشادات [دليل النشر](../deployment.ar.md) و[دليل الاسترداد](../operations/recovery.md) لأخذ النسخ الاحتياطية باستخدام البرنامج القديم قبل الترقية، وإجراء الاستعادة باستخدام الإصدار المطابق. وتذكر دائماً أن نجاح الاختبارات الآلية على الخادم لا يُثبت إمكانية الوصول الفعلي للحسابات أو الشبكة الخارجية أو إعدادات الفوترة أو الوجهات النهائية.

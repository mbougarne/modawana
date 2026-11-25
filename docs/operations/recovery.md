# Backup and recovery / النسخ والاستعادة

[English](#english) · [العربية](#العربية)

## English

Content export is not disaster recovery. `blog backup --file /private/snapshot.zip`
uses SQLite's online backup API, then copies every referenced retained revision and
upload under the storage maintenance lock. The ZIP includes schema history, hashes
and sizes. Markdown and standalone page block artifacts are copied with their
full history and linked draft and publication pointers. Keep it private: it contains owner credential records and private quiz
answers. The separate 32-byte `BLOG_AUTH_KEY_FILE` is deliberately excluded. Back up
that key in a separate encrypted offline location, with the age decryption identity
and independent passkey/recovery instructions. Never commit these secrets.

Backups also include checksum-matching plugin files, plugin settings and plugin
data. Plugin secret values are excluded. Re-enter secrets after restore.
A plugin without its approved files is restored disabled. [Plugin recovery](../plugins.md#owner-api-and-recovery)

Target RPO is **24 hours**, contingent on successful daily off-server transfer.
Install age, rsync, OpenSSH and GNU coreutils/findutils/flock on the Linux operator
host. Configure `BLOG_BINARY` (absolute native executable), normal BLOG environment,
`BLOG_BACKUP_DIR` (private directory outside data), `BLOG_BACKUP_RECIPIENT` (public
age recipient) and `BLOG_BACKUP_REMOTE=user@host:/private/path`. Verify the SSH host
key and provision a restricted backup account. Run `sh scripts/backup-daily.sh`.
It encrypts before transfer, retains seven daily and four weekly encrypted copies,
and prunes only after both uploads succeed. Remote storage must also provide GNU
find and sh. Install the supplied systemd service/timer with actual absolute paths
and private environment file; enable with `systemctl enable --now blog-backup.timer`.
Check `systemctl status blog-backup.timer` and `journalctl -u blog-backup.service`.
Alert on a failed run or newest off-server copy older than 24 hours. No remote
backup target is provisioned by the repository. For containers, create a native
operator binary of the **same revision** or a reviewed wrapper invoking
`docker compose exec -T blog /app/blog backup --file /backups/...`; the supplied
native runner's temporary path must be visible at the identical container path.

Restore drill (use the matching release and loaded private environment):

```sh
age -d -i /offline/age-identity -o /private/restore.zip /private/snapshot.zip.age
BLOG_DATA_ROOT=/srv/modawana/restored ./blog restore --file /private/restore.zip
```

The destination must not exist. Restore verifies the exact schema, checksums,
references and file limits before atomic installation. A backup containing
block revisions requires a block-aware release. Markdown-only backups remain
compatible when their SQLite schema matches. It removes all restored
passkeys, passwords/TOTP, sessions, API tokens, grants and recovery codes, and resets
browser learning identities/attempts/progress. A credential revoked after backup
cannot become usable again. The login email remains as the recovery identifier,
not an authentication factor. Restore writes a maintenance marker: public pages,
media, publisher and readiness return 503; owner authentication/re-enrollment stays
available. Configure the service to the restored directory and start it. Then:

```sh
./blog recover
# Redeem the printed short-lived grant on /en/login (or /ar/login, /fr/login).
# Enroll a new passkey and prove it by signing in.
./blog resume
./blog ready
./blog maintenance
```

The grant is secret: do not record it in tickets/logs. `resume` refuses until a new
passkey has been tested. Add an independent backup factor, generate new offline
recovery codes and rotate publisher credentials before normal publishing. Anonymous
readers start again; restored old cookies cannot recover their prior progress.
Verify content, uploads, gated denial, new login and a fresh encrypted backup before
retiring old storage. Delete decrypted temporary archives securely according to the
storage operator's policy. Never restore over the live directory.

Hourly maintenance removes bounded batches of expired authentication/learning data
and audit entries older than 180 days. Orphan cleanup holds the same storage lock,
protects every referenced immutable file, waits 24 hours, and examines at most
20,000 paths/removes at most 500 files per run. Unknown names are retained; symlinks
fail closed. Large directories can require operator investigation rather than an
assumption that every orphan was collected. `blog maintenance` reports actual counts.
Local tests cover corrupt/missing files, concurrent writes and post-snapshot
credential revocation. The real-server browser drill covers maintenance denial,
virtual passkey re-enrollment and reopening; it does not prove physical-key support.

### Commerce recovery and launch

Stop the original writer before restoring a store. Keep the restored checkout
and refund writes paused until the separately recorded payment review is
complete. A successful site resume after passkey recovery gives the owner
access to the recovery controls. Checkout still requires explicit activation.

1. Under **Store → Checkout settings → PayPal**, check the snapshot start
   and the restore cutoff displayed in UTC. The start is captured before the
   database snapshot. A backup made during an unresolved recovery carries the
   earlier start.
2. Reconnect the original merchant REST apps. For each recorded connection,
   use **Import the next period from PayPal** until coverage reaches the
   restore cutoff. The import uses authenticated, read-only event history.
   It shortens a saturated interval and advances coverage only for a complete
   result. For a legacy backup with an unknown start, an old or saturated
   event history, or an inaccessible app, compare the merchant reports
   manually and record the report reference or incident reference. Begin
   before the snapshot when the exact start is unknown.
3. Compare the merchant’s own activity report across the same interval,
   including apps or accounts added since the backup and delayed settlements
   around the cutoff. The PayPal event list is scoped to the REST app and can
   omit merchant activity outside that app. Check captures and refunds
   against the local operation and provider IDs.
4. Refresh until the queue drains. Known events reconcile against current
   provider data. Review unmatched captures and refunds in PayPal and record
   the disposition against each receipt. Use transaction or incident
   references without customer data. A verified notification keeps its
   provider event ID when it arrives both by history import and by webhook.
   Uncertain captures stay on hold. A missing or mismatched verified refund
   blocks further in-app refunds on that capture after checkout resumes. Manage
   any remaining disposition from the merchant’s PayPal account.
5. Confirm reconciliation with fresh owner authentication. The confirmation
   checks coverage and unresolved receipts and leaves checkout disabled.
   Enable checkout separately after the merchant checks pass.

[PayPal event history](https://developer.paypal.com/api/webhooks/v1/webhooks-events-list) ·
[PayPal Webhooks Events dashboard](https://developer.paypal.com/api/rest/webhooks/events-dashboard/)

#### Initial launch targets

Use SQLite for a single-writer store within the tested envelope, subject to a
repeat on the intended deployment with realistic data. Revisit the site database
if sustained writer contention, recovery constraints, or a need for multiple
same-site replicas exceeds that envelope.

| Measurement | Initial target |
| --- | --- |
| Local quote and stock reservation p95 | At most 250 ms each with 20 checkout attempts per second and 5 incoming webhooks per second |
| SQLite writer acquisition p95 | At most 100 ms including pool acquisition. Record `BUSY` or `LOCKED`, exact retries, and pool waits. |
| Webhook application p95 | At most 15 seconds under the mixed workload. Record the peak pending queue and verify that it drains. |
| Order notification outbox p95 | At most 5 seconds under the mixed workload |
| Off-server recovery point (RPO) | At most 15 minutes of local order state before separately reconciling PayPal |
| Merchant recovery time (RTO) | At most 60 minutes from traffic pause to a reconciled, tested store |

For a paid store, schedule and verify encrypted off-server copies frequently
enough that the **snapshot start** of the newest verified off-server archive
stays within 15 minutes. The daily timer is appropriate for the general
self-hosting baseline. For a store, start with a five-minute schedule and verify
that snapshot, encryption and transfer finish within that interval. Alert on
each failed upload or a copy approaching the age limit.
Time the whole recovery including retrieval, decryption, owner re-enrollment,
provider reconciliation and a checkout smoke test.

Run the local mixed workload and stale-backup drill from the repository root.

```sh
MODAWANA_COMMERCE_LOAD=1 go -C backend test ./internal/paypal -run '^TestCommerceMixedLoad$' -count=1 -v
go -C backend test ./internal/paypal ./internal/store ./internal/outbox -run 'TestCommerceStaleBackup|TestCheckoutLastUnit|TestPayPal|Test.*Lease' -count=1 -v
```

The mixed workload uses temporary data and simulated PayPal responses. It measures
Go service latency and excludes WAN time and browser interaction. Use a staging
repeat to measure full HTTP checkout and report provider latency separately.

Before activating a merchant, independently record a successful real Sandbox
purchase, cancellation, signed duplicate delivery, reconciliation and refund.
Record the live merchant ID, environment and approved country, store currency,
currency acceptance in PayPal, checkout tax and shipping examples, invoice and
receipt settings, and an isolated recovery drill on representative data. Confirm
that the merchant reviewed the configured tax, invoicing and shipping policies.
Keep those environment results and credentials in the operator’s private records.

## العربية

تشمل النسخ الاحتياطية ملفات الإضافات المطابقة لبصماتها وإعداداتها وبياناتها.
تُستبعد قيم الأسرار. أدخلها مجدداً بعد الاستعادة. تُستعاد الإضافة معطلة
إذا غابت ملفاتها المعتمدة. [استعادة الإضافات](../plugins.ar.md#واجهة-المالك-والاستعادة)

لا يُعد تصدير المحتوى بديلاً عن خطة التعافي من الكوارث (disaster recovery). يقوم الأمر `blog backup --file /private/snapshot.zip` بالاعتماد على واجهة النسخ الاحتياطي الحي في SQLite، ثم ينسخ كافة المراجعات والوسائط المحفوظة والمشار إليها تحت حماية قفل صيانة التخزين الحصري. يتضمن الأرشيف المضغوط (ZIP) تاريخ مخطط البيانات، وأحجام الملفات، وبصمات التجزئة؛ وتشمل المراجعات ملفات Markdown ووثائق كتل الصفحات المستقلة مع سجلها الكامل ومؤشرات المسودات والنشر؛ ويحتوي الأرشيف على سجلات بيانات اعتماد المالك وإجابات الاختبارات الخاصة؛ لذا يجب الحفاظ على سريته التامة. ولا يتضمن الأرشيف ملف مفتاح التشفير المنفصل ذي 32 بايت والمحدد في `BLOG_AUTH_KEY_FILE`؛ حيث يجب أخذ نسخة احتياطية مشفرة ومستقلة لهذا المفتاح في موقع آمن غير متصل بالإنترنت، مصحوباً بهوية فك التشفير لأداة age وتعليمات تسجيل مفتاح المرور البديل والاسترداد. احذر من وضع هذه الأسرار في مستودع Git.

يبلغ هدف نقطة التعافي المستهدف (RPO) **24 ساعة** كحد أقصى، شريطة نجاح النقل اليومي للنسخ خارج الخادم. ثبّت كلاً من age وrsync وOpenSSH وحزم أدوات GNU (coreutils وfindutils وflock) على الجهاز المضيف بنظام Linux. اضبط المتغير `BLOG_BINARY` بمسار تنفيذي أصلي ومطلق، مع متغيرات بيئة BLOG المعتادة، وحدد مجلداً خاصاً خارج مجلد البيانات في `BLOG_BACKUP_DIR`، ومفتاح المستلم العام لأداة age في `BLOG_BACKUP_RECIPIENT`، ومسار النقل البعيد في `BLOG_BACKUP_REMOTE=user@host:/private/path`. تحقق من صحة مفتاح مضيف SSH وأنشئ حساب نسخ احتياطي مخصصاً ومقيد الصلاحيات على الخادم البعيد. شغّل السكربت `sh scripts/backup-daily.sh`؛ حيث يقوم بتشفير النسخة قبل نقلها، ويحتفظ بسبع نسخ يومية وأربع نسخ أسبوعية مشفرة، ولا يبدأ بحذف النسخ القديمة إلا بعد اكتمال رفع النسختين بنجاح. ويجب أن يوفر الخادم البعيد أداتي GNU find وsh. ثبّت ملفي خدمة ومؤقت systemd المرفقين مع تعديل المسارات لتكون مطلقة، واستخدم ملف بيئة خاصاً محمياً، ثم فعّل المؤقت عبر: `systemctl enable --now blog-backup.timer`. راقب الحالة عبر `systemctl status blog-backup.timer` و`journalctl -u blog-backup.service`. واحرص على إعداد تنبيه فوري عند فشل أي تشغيل أو إذا تجاوز عمر أحدث نسخة بعيدة 24 ساعة. لا يقوم المستودع بتهيئة وجهة نسخ بعيدة نيابة عنك. وفي بيئات الحاويات، استخدم ملفاً تنفيذياً أصلياً مبنياً من **المراجعة البرمجية نفسها** أو غلافاً برمجياً موثوقاً يستدعي:
`docker compose exec -T blog /app/blog backup --file /backups/...`؛ مع مراعاة إتاحة مسار المجلد المؤقت للسكربت اليومي داخل الحاوية بالمسار المطابق نفسه.

تتطلب استعادة أرشيف يحتوي على مراجعات كتل إصداراً يدعمها. تبقى أرشيفات
Markdown فقط متوافقة عندما يطابق مخطط SQLite الإصدار المستخدم.

إجراء تجربة الاستعادة الميدانية (باستخدام الإصدار المطابق للبرنامج مع تحميل متغيرات البيئة الخاصة):

```sh
age -d -i /offline/age-identity -o /private/restore.zip /private/snapshot.zip.age
BLOG_DATA_ROOT=/srv/modawana/restored ./blog restore --file /private/restore.zip
```

يجب ألا يكون مجلد الوجهة المستهدفة موجوداً مسبقاً على القرص. يتحقق أمر الاستعادة من البنية الدقيقة لمخطط البيانات، وبصمات التجزئة، والمراجع، والحدود القصوى للملفات قبل التثبيت الذري النهائي. ويقوم الإجراء عمداً بحذف كافة مفاتيح المرور، وكلمات المرور، ورموز TOTP، والجلسات النشطة، ورموز API، وتصاريح النشر، ورموز الاسترداد السابقة المستعادة من الأرشيف، مع تصفير هويات المتصفح ومحاولات الاختبار وتقدم التعلّم للقراء. ولا يمكن لأي اعتماد تم إلغاؤه بعد أخذ النسخة أن يعود صالحاً للاستخدام. ويبقى عنوان البريد الإلكتروني معرّفاً للاسترداد فقط وليس عاملاً للمصادقة. وتُنشئ عملية الاستعادة علامة صيانة مؤقتة: حيث تُرْجع الصفحات العامة، والوسائط، وواجهات الناشر، وفحص الجاهزية رمز الخطأ 503؛ بينما تظل واجهات مصادقة المالك وإعادة التسجيل متاحة. وجّه إعدادات الخدمة إلى المجلد المستعاد الجديد وشغّلها، ثم نفّذ الأوامر التالية:

```sh
./blog recover
# استخدم تصريح الاسترداد السري المؤقت المطبوع عبر الرابط /ar/login (أو /en/login أو /fr/login)
# سجّل مفتاح مرور جديداً ثم اختبره بتسجيل الدخول الفعلي به
./blog resume
./blog ready
./blog maintenance
```

يُعد هذا التصريح سراً بالغ الأهمية: فلا تسجله في التذاكر أو ملفات السجلات. ويرفض الأمر `resume` استئناف العمل ما لم يتم اختبار مفتاح مرور جديد بنجاح. أضف عاملاً احتياطياً مستقلاً، وولّد رموز استرداد جديدة غير متصلة بالإنترنت، وقم بتدوير رموز الناشر قبل استئناف عمليات النشر العادية. وسيبدأ القراء المجهولون تقدمهم من جديد؛ حيث لا تُعيد ملفات الارتباط القديمة تقدمهم السابق. تحقق من سلامة المحتوى، والوسائط، وحجب الدروس المقفلة، وتسجيل الدخول الجديد، وتوليد نسخة مشفرة جديدة قبل التخلص من وحدة التخزين القديمة. واحذف ملفات الأرشيف المؤقتة المفكوكة بأمان وفق سياسة مشغل التخزين، وإياك والاستعادة فوق مجلد بيانات حي.

تتولى مهمة الصيانة التلقائية كل ساعة تنظيف دفعات محددة من بيانات المصادقة والتعلّم منتهية الصلاحية، وسجلات التدقيق التي تجاوز عمرها 180 يوماً. وتجري عملية تنظيف الملفات اليتيمة تحت حماية قفل التخزين نفسه؛ حيث تحمي كافة الملفات الثابتة المشار إليها، وتنتظر 24 ساعة للملفات اليتيمة كفترة أمان، وتفحص ما لا يتجاوز 20,000 مسار وتحذف 500 ملف كحد أقصى في كل دورة تشغيل. وتُترك الملفات ذات الأسماء المجهولة دون حذف، وتُرفض الروابط الرمزية بصورة آمنة. وقد تتطلب المجلدات الضخمة تدخلاً ومراجعة يدوية من المشغل؛ فلا تفترض إزالة كافة الملفات اليتيمة دفعة واحدة. ويُظهر الأمر `blog maintenance` الأعداد والإحصائيات الفعلية. وتغطي الاختبارات المحلية حالات تلف الملفات أو فقدانها، وعمليات الكتابة المتزامنة، وإلغاء الاعتمادات بعد أخذ اللقطة. وتتحقق تجربة المتصفح على خادم حقيقي من حجب الوصول أثناء الصيانة، وإعادة تسجيل مفتاح مرور افتراضي، وإعادة فتح الخدمة؛ ولكنها لا تُثبت التوافقية مع المفاتيح العتادية الحقيقية.

<a id="commerce-recovery-ar"></a>

### استعادة المتجر وإطلاقه

أوقف عملية الكتابة الأصلية قبل استعادة المتجر. أبقِ إتمام الطلبات
وكتابة عمليات ردّ المبالغ معلّقين إلى أن تكتمل مراجعة المدفوعات المسجّلة.
يتيح استئناف الموقع بعد استعادة مفتاح المرور الوصول إلى أدوات المراجعة.
يتطلّب إتمام الطلبات تفعيلًا صريحًا بعد ذلك.

1. افتح **المتجر ← إعدادات إتمام الطلب ← PayPal** وراجع وقت بدء النسخة
   وحدّ الاستعادة المعروضين بتوقيت UTC. يسجّل التطبيق البداية قبل أخذ
   لقطة قاعدة البيانات. تحتفظ النسخة المأخوذة أثناء مراجعة معلّقة بالبداية الأقدم.
2. أعد توصيل تطبيقات REST الأصلية للتاجر. استخدم **استورد الفترة التالية
   من PayPal** لكل اتصال حتى تبلغ التغطية حدّ الاستعادة. يقرأ الاستيراد
   سجل الأحداث الموثّق ويختصر الفترات المزدحمة. لا تتقدّم التغطية إلا بعد
   الحصول على نتيجة كاملة. عند غياب وقت البدء أو تعذّر الوصول إلى سجل
   الأحداث، طابق تقارير التاجر يدويًا وسجّل مرجع التقرير أو الحادثة.
   ابدأ قبل وقت النسخة عندما يتعذّر تحديد بداية دقيقة.
3. قارن تقرير نشاط التاجر للفترة نفسها، بما في ذلك التطبيقات والحسابات
   المضافة بعد النسخ والمدفوعات المتأخرة قرب الحدّ. ترتبط قائمة أحداث
   PayPal بتطبيق REST وقد تغيب عنها العمليات خارج التطبيق. طابق
   التحصيلات والمبالغ المردودة مع معرّفات العمليات المحلية ومعرّفات المزوّد.
4. حدّث الحالة حتى تفرغ قائمة الانتظار. تُطابق الأحداث المعروفة مع
   بيانات المزوّد الحالية. راجع التحصيلات والمبالغ غير المطابقة في PayPal
   وسجّل نتيجة كل إشعار بمرجع العملية أو الحادثة من دون بيانات العميل.
   يحتفظ الحدث نفسه بمعرّفه إذا وصل من الاستيراد ومن إشعار الويب.
   تظل التحصيلات غير المحسومة معلّقة. يوقف ردّ المبلغ المتحقّق منه وغير
   المطابق أي عملية ردّ أخرى داخل التطبيق للتحصيل نفسه بعد استئناف إتمام
   الطلبات. عالج ما تبقّى من حساب PayPal الخاص بالتاجر.
5. أكّد المطابقة بعد تحقق حديث من هوية المالك. يتحقّق التأكيد من تغطية
   الفترة والإشعارات العالقة ويُبقي إتمام الطلبات معطّلًا. فعّل إتمام الطلبات
   بصورة مستقلة بعد نجاح فحوص التاجر.

[سجل أحداث PayPal](https://developer.paypal.com/api/webhooks/v1/webhooks-events-list) ·
[لوحة أحداث PayPal](https://developer.paypal.com/api/rest/webhooks/events-dashboard/)

#### أهداف الإطلاق الأولية

استخدم SQLite لمتجر بعملية كتابة واحدة ضمن حدود الحمل المقاسة، بعد
تكرار القياس في بيئة النشر بالبيانات المتوقعة. أعد تقييم قاعدة بيانات الموقع
إذا تجاوز تنافس الكتابة المتواصل أو متطلبات الاستعادة أو تعدّد نُسخ
الموقع هذه الحدود.

| القياس | الهدف الأولي |
| --- | --- |
| المئين 95 لحساب الإجمالي وحجز المخزون | 250 مللي ثانية لكل منهما بحد أقصى، مع 20 محاولة طلب و5 إشعارات ويب في الثانية |
| المئين 95 لاكتساب معاملة كتابة SQLite | 100 مللي ثانية بحد أقصى مع انتظار الاتصال. سجّل حالات `BUSY` و`LOCKED` وإعادة المحاولة المطابقة وانتظار الاتصالات. |
| المئين 95 لتطبيق إشعارات الويب | 15 ثانية بحد أقصى تحت الحمل المشترك. سجّل ذروة القائمة وتحقّق من تفريغها. |
| المئين 95 لتسليم إشعارات الطلبات | 5 ثوانٍ بحد أقصى تحت الحمل المشترك |
| نقطة الاستعادة خارج الخادم (RPO) | 15 دقيقة بحد أقصى من حالة الطلبات المحلية قبل مطابقة PayPal بصورة مستقلة |
| مدة استعادة المتجر (RTO) | 60 دقيقة بحد أقصى من إيقاف الزيارات إلى استعادة متجر خضع للمطابقة والاختبار |

جدول للمتجر المدفوع نسخًا مشفّرة ومتكررة خارج الخادم، وتحقّق من أن
**بداية اللقطة** في أحدث نسخة خارجية متحقّق منها لا يتجاوز عمرها 15 دقيقة.
يناسب المؤقت اليومي خط الأساس العام للاستضافة الذاتية. ابدأ بتكرار
النسخ كل خمس دقائق، وتحقّق من اكتمال أخذ اللقطة وتشفيرها ونقلها خلال
تلك الفترة. اضبط تنبيهًا عند فشل الرفع أو اقتراب النسخة من حدّ العمر. قِس الاستعادة كاملةً، بما فيها الاسترجاع وفكّ التشفير وإعادة
تسجيل المالك ومطابقة المزوّد وفحص إتمام طلب.

شغّل اختبار الحمل المشترك وتمرين النسخة القديمة من جذر المستودع.

```sh
MODAWANA_COMMERCE_LOAD=1 go -C backend test ./internal/paypal -run '^TestCommerceMixedLoad$' -count=1 -v
go -C backend test ./internal/paypal ./internal/store ./internal/outbox -run 'TestCommerceStaleBackup|TestCheckoutLastUnit|TestPayPal|Test.*Lease' -count=1 -v
```

يستخدم اختبار الحمل بيانات مؤقتة واستجابات PayPal محاكية. يقيس زمن
خدمات Go، وتُقاس رحلة HTTP الكاملة وزمن المزوّد بصورة منفصلة في بيئة الاختبار.

قبل تفعيل التاجر، سجّل نجاح طلب وإلغاء وإشعار موقّع مكرّر ومطابقة
وردّ مبلغ في بيئة PayPal التجريبية الفعلية. سجّل معرّف التاجر وبيئته
وبلده المعتمد، وعملة المتجر وقبولها في PayPal، وأمثلة الضرائب والشحن،
وإعدادات الفواتير والإيصالات، وتمرين استعادة معزولًا ببيانات تمثيلية.
أكّد مراجعة التاجر للسياسات المضبوطة للضرائب والفواتير والشحن. احتفظ
بنتائج البيئة وبيانات الاعتماد في سجلات المشغّل الخاصة.

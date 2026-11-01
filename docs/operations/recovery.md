# Backup and recovery / النسخ والاستعادة

[English](#english) · [العربية](#العربية)

## English

Content export is not disaster recovery. `blog backup --file /private/snapshot.zip`
uses SQLite's online backup API, then copies every referenced retained revision and
upload under the storage maintenance lock. The ZIP includes schema history, hashes
and sizes. Keep it private: it contains owner credential records and private quiz
answers. The separate 32-byte `BLOG_AUTH_KEY_FILE` is deliberately excluded. Back up
that key in a separate encrypted offline location, with the age decryption identity
and independent passkey/recovery instructions. Never commit these secrets.

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
BLOG_DATA_ROOT=/srv/personal-blog/restored ./blog restore --file /private/restore.zip
```

The destination must not exist. Restore verifies the exact schema, checksums,
references and file limits before atomic installation. It removes all restored
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

## العربية

لا يُعد تصدير المحتوى بديلاً عن خطة التعافي من الكوارث (disaster recovery). يقوم الأمر `blog backup --file /private/snapshot.zip` بالاعتماد على واجهة النسخ الاحتياطي الحي في SQLite، ثم ينسخ كافة المراجعات والوسائط المحفوظة والمشار إليها تحت حماية قفل صيانة التخزين الحصري. يتضمن الأرشيف المضغوط (ZIP) تاريخ مخطط البيانات، وأحجام الملفات، وبصمات التجزئة؛ ويحتوي على سجلات بيانات اعتماد المالك وإجابات الاختبارات الخاصة؛ لذا يجب الحفاظ على سريته التامة. ولا يتضمن الأرشيف ملف مفتاح التشفير المنفصل ذي 32 بايت والمحدد في `BLOG_AUTH_KEY_FILE`؛ حيث يجب أخذ نسخة احتياطية مشفرة ومستقلة لهذا المفتاح في موقع آمن غير متصل بالإنترنت، مصحوباً بهوية فك التشفير لأداة age وتعليمات تسجيل مفتاح المرور البديل والاسترداد. احذر من وضع هذه الأسرار في مستودع Git.

يبلغ هدف نقطة التعافي المستهدف (RPO) **24 ساعة** كحد أقصى، شريطة نجاح النقل اليومي للنسخ خارج الخادم. ثبّت كلاً من age وrsync وOpenSSH وحزم أدوات GNU (coreutils وfindutils وflock) على الجهاز المضيف بنظام Linux. اضبط المتغير `BLOG_BINARY` بمسار تنفيذي أصلي ومطلق، مع متغيرات بيئة BLOG المعتادة، وحدد مجلداً خاصاً خارج مجلد البيانات في `BLOG_BACKUP_DIR`، ومفتاح المستلم العام لأداة age في `BLOG_BACKUP_RECIPIENT`، ومسار النقل البعيد في `BLOG_BACKUP_REMOTE=user@host:/private/path`. تحقق من صحة مفتاح مضيف SSH وأنشئ حساب نسخ احتياطي مخصصاً ومقيد الصلاحيات على الخادم البعيد. شغّل السكربت `sh scripts/backup-daily.sh`؛ حيث يقوم بتشفير النسخة قبل نقلها، ويحتفظ بسبع نسخ يومية وأربع نسخ أسبوعية مشفرة، ولا يبدأ بحذف النسخ القديمة إلا بعد اكتمال رفع النسختين بنجاح. ويجب أن يوفر الخادم البعيد أداتي GNU find وsh. ثبّت ملفي خدمة ومؤقت systemd المرفقين مع تعديل المسارات لتكون مطلقة، واستخدم ملف بيئة خاصاً محمياً، ثم فعّل المؤقت عبر: `systemctl enable --now blog-backup.timer`. راقب الحالة عبر `systemctl status blog-backup.timer` و`journalctl -u blog-backup.service`. واحرص على إعداد تنبيه فوري عند فشل أي تشغيل أو إذا تجاوز عمر أحدث نسخة بعيدة 24 ساعة. لا يقوم المستودع بتهيئة وجهة نسخ بعيدة نيابة عنك. وفي بيئات الحاويات، استخدم ملفاً تنفيذياً أصلياً مبنياً من **المراجعة البرمجية نفسها** أو غلافاً برمجياً موثوقاً يستدعي:
`docker compose exec -T blog /app/blog backup --file /backups/...`؛ مع مراعاة إتاحة مسار المجلد المؤقت للسكربت اليومي داخل الحاوية بالمسار المطابق نفسه.

إجراء تجربة الاستعادة الميدانية (باستخدام الإصدار المطابق للبرنامج مع تحميل متغيرات البيئة الخاصة):

```sh
age -d -i /offline/age-identity -o /private/restore.zip /private/snapshot.zip.age
BLOG_DATA_ROOT=/srv/personal-blog/restored ./blog restore --file /private/restore.zip
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

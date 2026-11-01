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

تصدير المحتوى ليس نسخة تعافٍ. يستخدم `blog backup --file /private/snapshot.zip`
واجهة النسخ الحي في SQLite ثم ينسخ جميع المراجعات والوسائط المرجعية تحت قفل
الصيانة. يشمل الأرشيف سجل المخطط والأحجام والبصمات، ويحتوي اعتمادات المالك وإجابات
خاصة؛ احفظه سرياً. لا يشمل ملف `BLOG_AUTH_KEY_FILE` المنفصل ذي 32 بايت. احتفظ بنسخة
مشفرة مستقلة غير متصلة من المفتاح وهوية فك age وتعليمات عامل الدخول الاحتياطي
والاسترداد. لا تضع الأسرار في Git.

هدف فقد البيانات **24 ساعة** بشرط نجاح النقل اليومي خارج الخادم. ثبّت age وrsync
وOpenSSH وأدوات GNU coreutils/findutils/flock على مضيف تشغيل Linux. اضبط
`BLOG_BINARY` بمسار تنفيذي أصلي مطلق، وبيئة BLOG المعتادة و`BLOG_BACKUP_DIR` خارج
البيانات و`BLOG_BACKUP_RECIPIENT` بمستلم age العام و
`BLOG_BACKUP_REMOTE=user@host:/private/path`. تحقق من مفتاح SSH وأنشئ حساب نسخ
مقيداً. شغّل `sh scripts/backup-daily.sh`. يحدث التشفير قبل النقل وتُحفظ سبع نسخ
يومية وأربع أسبوعية، ولا يبدأ الحذف إلا بعد نجاح النقلين. يحتاج الطرف البعيد GNU
find وsh. عدّل مسارات خدمة ومؤقت systemd وملف البيئة الخاص ثم شغّل
`systemctl enable --now blog-backup.timer`. افحص `systemctl status blog-backup.timer`
و`journalctl -u blog-backup.service`. أنشئ تنبيهاً عند الفشل أو تجاوز أحدث نسخة
بعيدة 24 ساعة. لا يهيئ المستودع وجهة نسخ فعلية. للحاويات استخدم ملفاً تنفيذياً
أصلياً من **نفس المراجعة** أو غلافاً مراجعاً يستدعي
`docker compose exec -T blog /app/blog backup --file /backups/...`؛ يجب إتاحة مسار
المجلد المؤقت للبرنامج اليومي داخل الحاوية بالمسار نفسه.

تجربة الاستعادة بالإصدار المطابق وبيئة التشغيل الخاصة:

```sh
age -d -i /offline/age-identity -o /private/restore.zip /private/snapshot.zip.age
BLOG_DATA_ROOT=/srv/personal-blog/restored ./blog restore --file /private/restore.zip
```

يجب ألا توجد الوجهة. تُفحص بنية المخطط الدقيقة والبصمات والمراجع والحدود قبل
التثبيت الذري. تُحذف مفاتيح المرور وكلمات السر وTOTP والجلسات ورموز API وتصاريح
ورموز الاسترداد المستعادة، وتُصفّر هويات القراء ومحاولاتهم وتقدمهم. لا يعود اعتماد
أُلغي بعد النسخ صالحاً. يبقى بريد الدخول معرّفاً للاسترداد وليس عامل مصادقة. يمنع وضع الصيانة الصفحات العامة والوسائط والناشر والجاهزية
برمز 503، مع إبقاء المصادقة وإعادة التسجيل متاحين. وجّه الخدمة إلى المجلد المستعاد
وشغّلها، ثم:

```sh
./blog recover
# استخدم التصريح السري قصير العمر في /ar/login أو /en/login أو /fr/login.
# سجّل مفتاح مرور جديداً ثم اختبره بتسجيل الدخول.
./blog resume
./blog ready
./blog maintenance
```

لا تسجل التصريح في السجلات أو التذاكر. يرفض `resume` قبل اختبار مفتاح جديد. أضف
عاملاً احتياطياً مستقلاً ورموز استرداد جديدة غير متصلة وبدّل رموز الناشر. يبدأ
القراء المجهولون من جديد؛ لا تعيد ملفات الارتباط القديمة تقدمهم. تحقق من المحتوى
والوسائط ومنع الدروس المقفلة والدخول الجديد ونسخة مشفرة جديدة قبل إيقاف التخزين
القديم. احذف الأرشيفات المفكوكة وفق سياسة مشغل التخزين، ولا تستعد فوق مجلد حي.

تنظف الصيانة كل ساعة دفعات محدودة من بيانات المصادقة والتعلم المنتهية والتدقيق
الأقدم من 180 يوماً. تحمي الملفات المرجعية تحت القفل وتنتظر 24 ساعة لليتيم، وتفحص
20,000 مسار وتحذف 500 ملف كحد أقصى لكل تشغيل. تترك الأسماء المجهولة وترفض الروابط
الرمزية. قد تتطلب المجلدات الضخمة تدخلاً؛ لا تفترض إزالة كل الملفات اليتيمة.
يعرض `blog maintenance` الأعداد الفعلية. تشمل الاختبارات التلف والفقد والكتابة
المتزامنة وإلغاء الاعتماد بعد اللقطة. تختبر تجربة المتصفح المنع وإعادة التسجيل
بمفتاح افتراضي وفتح الخدمة، ولا تثبت توافق العتاد الحقيقي.

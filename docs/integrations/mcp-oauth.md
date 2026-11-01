# MCP and OAuth / ‏MCP وOAuth

[English](#english) · [العربية](#العربية)

## English

MCP is an optional publishing interface hosted by the existing Go application at
`POST /api/v1/publisher/mcp`. It is reusable by other installations; no personal
account or credential is bundled. Normal dashboard publishing needs no MCP client,
OpenAI account, or AI API key. Requests require a scoped publisher bearer token or
an OAuth access token for this resource. Never select unauthenticated access.

Use `https://blog.example/api/v1/publisher/mcp`, replacing the origin with your
actual HTTPS origin. Configure supported bearer clients through a private token
environment variable, not a token in the URL or configuration committed to Git.
For OAuth-capable clients, discovery is served at:

- `/.well-known/oauth-authorization-server`
- `/.well-known/oauth-protected-resource`
- `/.well-known/oauth-protected-resource/api/v1/publisher/mcp`

Check the exact resource-metadata URL in the endpoint's 401 challenge. The issuer
and resource derive from the configured origin. `/oauth/register` supports bounded
dynamic public-client registration, token endpoint authentication `none`, S256 PKCE,
and exact registered callbacks. Public-client authentication `none` does not mean
that MCP is unauthenticated: the owner still signs in and authorizes the grant.

### Connection

1. Create a publisher token in **Security**, with only the scopes and collections
   the integration needs. Use draft-only permissions for the initial check.
2. In a compatible client, enter the MCP URL and choose OAuth/dynamic public-client
   registration where supported. The current callback allowlist accepts only
   `https://chatgpt.com/connector_platform_oauth_redirect` and
   `https://chatgpt.com/connector/oauth/<callback_id>` with bounded identifiers.
   Other clients can use scoped bearer authentication if supported. Arbitrary
   callback domains, loopback OAuth callbacks, CIMD and client secrets are not supported.
3. Sign in to the blog and select the parent publisher token in the consent screen.
   A fresh full owner session and same-origin CSRF are required. Restricted recovery
   sessions cannot approve. The original publisher secret is never sent to the client.
4. Verify tool discovery, save/read an unpublished draft, check denied scopes and
   stale versions, and test refresh and disconnect. Client/account eligibility varies;
   server tests alone do not establish access from a particular hosted client.

### Tools and limits

| Tool | Authority |
| --- | --- |
| `get_draft` | Read a scoped draft; quiz-bearing drafts also require quiz scope |
| `save_draft` | Save an immutable revision with expected version and idempotency key |
| `upload_media` | Upload bounded base64 content with media scope |
| `publish_revision` | Publish an exact saved revision/version with publish scope |

Every call reuses [publisher authorization](../api/publisher.md), collection limits,
media ownership, revocation, version conflicts and replay rules. Tools cannot create
tokens, review translations/assessments, alter plans or bypass lesson gates.

The stateless JSON transport bounds requests to 12 MiB, decoded attachments to
8 MiB, concurrency to four and handler contexts to 60 seconds. The multipart
publisher API supports larger media within its 50 MiB limit. Query parameters are
rejected; Origin must be absent or match the blog. Require JSON Content-Type and
Accept with `application/json` and `text/event-stream`. Responses are JSON, not a
persistent SSE connection. Protocol handling lives in `backend/internal/http/mcp.go`.

OAuth codes expire after 60 seconds and are single-use. Access tokens last 15 minutes;
refresh tokens rotate and expire absolutely after 30 days. Reusing a consumed code
or refresh token revokes the grant. Serialize refresh requests; after an ambiguous
refresh outcome, reauthorize rather than replaying a consumed token. OAuth access
tokens work only at MCP; direct publisher REST uses a publisher bearer token.

Disconnect individual grants under **Security → Connected publishing apps**.
Revoking the parent token disconnects dependent grants; owner security-version
changes invalidate them too. Restore deletes OAuth clients, requests and grants.
Logs must omit OAuth queries, authorization/cookie headers and token response bodies.
See [release readiness](../release-readiness.md) for verification boundaries.

## العربية

تُعد واجهة بروتوكول سياق النموذج (MCP) واجهة نشر اختيارية يقدمها تطبيق Go نفسه عبر المسار `POST /api/v1/publisher/mcp`. وهي مصممة لتكون قابلة لإعادة الاستخدام في مختلف التثبيتات دون تضمين أي حسابات شخصية أو بيانات اعتماد مسبقة. ولا تتطلب عمليات النشر العادية من لوحة التحكم وجود عميل MCP، أو حساب في OpenAI، أو مفتاح لواجهة برمجة ذكاء اصطناعي. تتطلب الطلبات تزويدها إما برمز ناشر حامل (bearer token) محدد الصلاحيات، أو برمز وصول OAuth مخصص لهذا المورد. احذر من اختيار نمط الوصول غير المصادق عليه إطلاقاً.

استخدم الرابط `https://blog.example/api/v1/publisher/mcp`، مع استبدال اسم النطاق بعنوان أصل HTTPS الفعلي لمدونتك. اضبط عملاء الرمز الحامل المدعومين عبر متغير بيئة خاص للرمز، وتجنب وضع الرمز في رابط URL أو في ملفات إعدادات تخضع لتتبع مستودع Git. وبالنسبة للعملاء الذين يدعمون بروتوكول OAuth، تتوفر نقاط اكتشاف الموارد الوصفية على العناوين التالية:

- `/.well-known/oauth-authorization-server`
- `/.well-known/oauth-protected-resource`
- `/.well-known/oauth-protected-resource/api/v1/publisher/mcp`

تحقق من الرابط الدقيق للبيانات الوصفية للمورد في ترويسة التحدي (401 challenge) التي تُرجعها نقطة النهاية. يُشتق كل من المُصدر (issuer) والمورد من أصل النطاق المهيأ. وتدعم نقطة النهاية `/oauth/register` التسجيل الديناميكي المنضبط للعملاء العامين (public clients)، مع استخدام نمط مصادقة نقطة الرموز `none`، وتقنية S256 PKCE، وروابط عودة (callbacks) مطابقة تماماً للقائمة المسجلة. إن استخدام نمط مصادقة العميل العام `none` لا يعني أن واجهة MCP مفتوحة بلا مصادقة؛ إذ يظل تسجيل دخول المالك وموافقته الصريحة شرطين لازمين لمنح الوصول.

### خطوات الاتصال

1. أنشئ رمز ناشر جديداً من قسم **الأمان** في لوحة التحكم، مع قصر صلاحياته على أضيق نطاق والمجموعات الضرورية للتكامل فقط. يُفضل البدء بصلاحيات المسودة فقط للفحص الأولي.
2. في العميل المتوافق، أدخل رابط واجهة MCP واختر التسجيل الديناميكي للعميل العام عبر OAuth حيثما كان ذلك مدعوماً. تسمح القائمة البيضاء الحالية لروابط العودة فقط بالعناوين:
   `https://chatgpt.com/connector_platform_oauth_redirect` و
   `https://chatgpt.com/connector/oauth/<callback_id>` بمعرفات محددة النطاق.
   ويمكن للعملاء الآخرين استخدام المصادقة عبر الرمز الحامل (bearer) إن كانوا يدعمونها. ولا يدعم النظام عناوين العودة العشوائية، أو روابط OAuth المحلية (loopback)، أو معيار CIMD، أو أسرار العملاء المشتركة.
3. سجّل الدخول إلى المدونة واختر رمز الناشر الأب في شاشة الموافقة على المنح. يتطلب ذلك وجود جلسة مالك كاملة وحديثة مع فحص الحماية من تزوير الطلبات (CSRF) لنفس النطاق. ولا تملك جلسات الاسترداد المقيدة صلاحية الموافقة. ولا يُرسل السر الأصلي لرمز الناشر إلى العميل مطلقاً.
4. تحقق من نجاح اكتشاف الأدوات، وتجربة حفظ مسودة غير منشورة وقراءتها، والتحقق من رفض الصلاحيات غير الممنوحة وتعارض الإصدارات القديمة، واختبر تجديد الرمز وفصل الاتصال. وتختلف متطلبات أهلية الحسابات والعملاء؛ فاختبارات الخادم وحدها لا تُثبت إمكانية الوصول من عميل مستضاف بعينه.

### الأدوات المتاحة والحدود المفروضة

| الأداة | الصلاحية المطلوبة |
| --- | --- |
| `get_draft` | قراءة مسودة ضمن النطاق المسموح؛ وتتطلب المسودات المحتوية على اختبارات صلاحية `quiz` أيضاً |
| `save_draft` | حفظ مراجعة ثابتة غير قابلة للتعديل مع التحقق من الإصدار المتوقع ومفتاح منع تكرار الأثر |
| `upload_media` | رفع محتوى وسائط مرمّز بنظام base64 بحدود حجم مقيدة بصلاحية `media` |
| `publish_revision` | نشر مراجعة وإصدار محفوظين بعينهما بدقة بصلاحية `publish` |

تُطبق كل عملية قواعد [تفويض الناشر](../api/publisher.ar.md)، وقيود المجموعات، وملكية الوسائط، والإلغاء، وتعارض الإصدارات، وقواعد إعادة الطلبات المتطابقة. ولا يمكن للأدوات توليد رموز برمجية، أو مراجعة الترجمات والتقييمات، أو تعديل خطط المسارات، أو تجاوز بوابات واشتراطات الدروس.

تعتمد طبقة نقل JSON عديمة الحالة (stateless) على حدود صارمة: 12 ميغابايت كحد أقصى لحجم الطلب، و8 ميغابايت للمرفقات المفكوكة، و4 طلبات متزامنة كحد أقصى، ومهلة تنفيذ تبلغ 60 ثانية لسياق المعالجة. بينما تدعم واجهة الناشر متعددة الأجزاء (multipart) وسائط بحجم أكبر يصل إلى 50 ميغابايت. تُرفض معاملات الاستعلام (query parameters) في الرابط؛ ويجب أن تكون ترويسة Origin غائبة أو مطابقة لنطاق المدونة تماماً. وتشترط الواجهة ترويسة Content-Type بتنسيق JSON وترويسة Accept تتضمن `application/json` و`text/event-stream`. وتكون الاستجابات بتنسيق JSON عادي وليست اتصال أحداث تدفقي دائم (SSE). وتوجد معالجة البروتوكول في الملف `backend/internal/http/mcp.go`.

تنتهي صلاحية رموز تفويض OAuth بعد 60 ثانية وهي صالحة للاستخدام مرة واحدة فقط. وتدوم صلاحية رموز الوصول لمدة 15 دقيقة؛ بينما تخضع رموز التحديث (refresh tokens) للتدوير الدوري وتنتهي صلاحيتها نهائياً بعد 30 يوماً. وتؤدي محاولة إعادة استخدام رمز تفويض مستهلك أو رمز تحديث قديم إلى إلغاء المنحة بالكامل. احرص على تسلسل طلبات التحديث وتفادي إرسالها بشكل متزامن؛ وعند وقوع نتيجة غير مؤكدة لطلب التحديث، أعد التفويض من جديد بدلاً من إعادة إرسال الرمز المستهلك. تعمل رموز وصول OAuth حصرياً مع واجهة MCP؛ بينما تتطلب واجهة REST المباشرة للناشر استخدام رمز الناشر الحامل (bearer).

يمكنك فصل المنح الفردية من قسم **الأمان ← تطبيقات النشر المتصلة**. ويؤدي إلغاء الرمز الأب إلى فصل كافة المنح التابعة له تلقائياً؛ كما يؤدي تغيير الإصدار الأمني للمالك إلى إبطالها أيضاً. وتؤدي عمليات استعادة البيانات إلى مسح كافة عملاء OAuth وطلباته ومنحه. ويجب أن تخلو ملفات السجلات من استعلامات OAuth، وترويسات التفويض والارتباط، ومتون استجابات الرموز. راجع دليل [جاهزية الإصدار](../release-readiness.ar.md) لمعرفة حدود التحقق.

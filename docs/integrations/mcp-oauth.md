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

MCP واجهة نشر اختيارية داخل تطبيق Go نفسه على `POST /api/v1/publisher/mcp`.
يمكن للتثبيتات الأخرى استعمالها؛ لا يُرفق حساب شخصي أو اعتماد. لا يحتاج النشر
من اللوحة عميل MCP أو حساب OpenAI أو مفتاح API للذكاء الاصطناعي. تحتاج الطلبات
رمز ناشر bearer محدد الصلاحيات أو رمز OAuth لهذا المورد. لا تختَر وصولاً بلا مصادقة.

استخدم `https://blog.example/api/v1/publisher/mcp` واستبدل الأصل بعنوان HTTPS الفعلي.
اضبط عملاء bearer المدعومين بمتغير بيئة خاص للرمز، لا بوضعه في URL أو إعداد متتبع.
للعملاء الداعمين لـOAuth تتوفر بيانات الاكتشاف في:

- `/.well-known/oauth-authorization-server`
- `/.well-known/oauth-protected-resource`
- `/.well-known/oauth-protected-resource/api/v1/publisher/mcp`

راجع عنوان بيانات المورد الدقيق في تحدي 401. يشتق المُصدر والمورد من الأصل
المضبوط. يدعم `/oauth/register` التسجيل الديناميكي المحدود لعميل عام بمصادقة
نقطة الرموز `none` وS256 PKCE وعناوين عودة مطابقة حرفياً. لا تعني `none`
أن MCP بلا مصادقة؛ يبقى دخول المالك وموافقته لازمين.

### الاتصال

1. أنشئ رمز نشر في **الأمان** بأقل الصلاحيات والمجموعات اللازمة. ابدأ بصلاحية المسودة فقط.
2. أدخل عنوان MCP في عميل متوافق واختر OAuth والتسجيل الديناميكي لعميل عام
   حيث يُدعمان. تسمح القائمة الحالية فقط بـ
   `https://chatgpt.com/connector_platform_oauth_redirect` و
   `https://chatgpt.com/connector/oauth/<callback_id>` بمعرّفات محدودة.
   يمكن لعملاء آخرين استعمال bearer إن دعموه. لا تُدعم عناوين عودة عشوائية
   أو OAuth محلي أو CIMD أو أسرار عملاء.
3. ادخل إلى المدونة واختر الرمز الأب في شاشة الموافقة. تلزم جلسة مالك كاملة
   حديثة وCSRF من الأصل نفسه. لا توافق جلسات الاسترداد المقيدة، ولا يُرسل سر الناشر الأصلي للعميل.
4. تحقق من اكتشاف الأدوات وحفظ مسودة خاصة وقراءتها ورفض الصلاحيات والنسخ القديمة
   والتجديد والفصل. تختلف أهلية الحساب والعميل؛ لا يثبت اختبار الخادم وصول عميل مستضاف بعينه.

### الأدوات والحدود

| الأداة | الصلاحية |
| --- | --- |
| `get_draft` | قراءة مسودة ضمن النطاق؛ تحتاج مسودة الاختبار صلاحية quiz أيضاً |
| `save_draft` | حفظ مراجعة ثابتة بالإصدار المتوقع ومفتاح إعادة الطلب |
| `upload_media` | رفع محتوى base64 محدود بصلاحية media |
| `publish_revision` | نشر مراجعة وإصدار محفوظين بعينهما بصلاحية publish |

تعيد كل عملية استخدام [تفويض الناشر](../api/publisher.ar.md) وحدود المجموعات
وملكية الوسائط والإلغاء والتعارض والإعادة. لا تنشئ الأدوات رموزاً ولا تراجع
الترجمات والتقييمات ولا تعدل الخطط أو تتجاوز بوابات الدروس.

نقل JSON بلا جلسة يحد الطلب إلى 12 MiB والمرفقات المفكوكة إلى 8 MiB والتزامن
إلى أربعة ومدة المعالج إلى 60 ثانية. تدعم API متعددة الأجزاء وسائط أكبر حتى
50 MiB. تُرفض معاملات الاستعلام؛ يغيب Origin أو يطابق المدونة. يلزم JSON
Content-Type وAccept يحوي `application/json` و`text/event-stream`. الردود JSON
لا اتصال SSE دائم. تنفيذ البروتوكول في `backend/internal/http/mcp.go`.

تنتهي رموز التفويض بعد 60 ثانية وتستخدم مرة واحدة. الوصول 15 دقيقة؛ تدور رموز
التجديد وتنتهي نهائياً بعد 30 يوماً. إعادة رمز مستهلك تلغي المنحة. سلسل طلبات
التجديد؛ عند غموض النتيجة أعد التفويض لا الرمز المستهلك. تعمل رموز OAuth في MCP
فقط؛ تستخدم REST المباشرة رمز الناشر bearer.

افصل المنح من **الأمان ← تطبيقات النشر المتصلة**. إلغاء الأب يفصل المنح التابعة؛
تغيير النسخة الأمنية يبطلها أيضاً. تحذف الاستعادة عملاء OAuth وطلباته ومنحه.
يجب أن تستبعد السجلات استعلامات OAuth وترويسات التفويض والارتباط وأجسام رموز
الاستجابة. راجع [جاهزية الإصدار](../release-readiness.ar.md) لحدود التحقق.

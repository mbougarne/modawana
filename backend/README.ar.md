# الخلفية

[English](README.md) · [المشروع](../README.ar.md)

تضم وحدة Go خادم التطبيق `cmd/blog` والناشر المحدد الصلاحيات `cmd/publisher`.
تنفذ الحزم الداخلية المصادقة والمحتوى والوسائط والإعدادات والتعلم والتخزين
والنقل والنسخ والاسترداد. لا يُزرع مالك أو اعتماد أو محتوى.

من هذا المجلد:

```sh
go test ./...
go test -race ./...
go vet ./...
go mod verify
```

من الجذر استخدم `node scripts/project.mjs build` لبناء البرنامجين والواجهة.
يخدم Go أصول الإنتاج دون Node. راجع [البنية](../docs/architecture.ar.md)
و[API والإعداد](../docs/api/README.ar.md) و[الترحيلات](migrations/README.ar.md)
و[النشر](../docs/deployment.ar.md).

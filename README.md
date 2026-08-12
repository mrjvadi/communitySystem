# Community System

این مخزن دو سرویس مستقل برای مدیریت درآمد کامیونیتی‌ها و تشخیص تقلب را در یک Go workspace نگهداری می‌کند:

- `community-service/`: ثبت و تأیید گروه‌ها و کانال‌ها، اعتبارسنجی اعضا و تقسیم درآمد کمپین‌ها
- `fraud-engine/`: امتیازدهی اعتماد کاربران و کیفیت کامیونیتی‌ها و ثبت رویدادهای مشکوک
- `shared/`: قراردادها و adapterهای مشترک موردنیاز همین دو سرویس

## Community Service

`community-service` هسته حسابداری کامیونیتی است. اطلاعات گروه‌ها و کانال‌ها را در PostgreSQL نگهداری می‌کند، عضویت‌های منتسب به کمپین را اعتبارسنجی می‌کند و درآمد تولیدشده را بین مالک، اعضا و پلتفرم تقسیم می‌کند.

### مسئولیت‌ها

- ثبت گروه یا کانال و نگهداری وضعیت `pending`، `active`، `suspended` یا `rejected`
- نگهداری لینک و هش دعوت برای attribution اعضای ورودی
- اعتبارسنجی ماندگاری اعضا در بازه قابل تنظیم از یک ساعت تا هفت روز
- جلوگیری از پردازش دوباره درآمد یک کمپین برای یک کامیونیتی
- تقسیم پیش‌فرض درآمد:
  - گروه: ۵۰٪ مالک، ۴۰٪ اعضای فعال، ۱۰٪ پلتفرم
  - کانال: ۹۰٪ مالک، ۰٪ اعضا، ۱۰٪ پلتفرم
- انتشار earningهای مالک، اعضا و پلتفرم از طریق NATS

### HTTP API

- `GET /health`
- `POST /community/register`
- `GET /community/:id`
- `GET /community/by-chat/:chat_id`
- `POST /community/:id/validation-window`
- `GET /owner/:telegram_id/communities`
- مسیرهای مدیریتی زیر `/admin/*` با هدر `X-Admin-Key`

### ارتباطات NATS

این سرویس رویدادهای `membership.joined`، `membership.left`، `community.activity.updated`، `campaign.revenue.generated` و `community.score.updated` را مصرف می‌کند و رویدادهای اعتبارسنجی، درآمد و توزیع را منتشر می‌کند.

## Fraud Engine

`fraud-engine` داده‌های رفتاری را در MongoDB ذخیره می‌کند و برای کاربران و کامیونیتی‌ها امتیاز ۰ تا ۱۰۰ می‌سازد. محاسبه امتیاز بر پایه ماندگاری عضویت، نسبت ورود و خروج، فعالیت، retention، تنوع عضویت و کیفیت کمپین انجام می‌شود.

### مسئولیت‌ها

- ثبت تاریخچه عضویت، فعالیت و تغییرات پروفایل کاربران
- محاسبه Trust Score کاربر با برچسب‌های `trusted`، `normal`، `suspicious` و `high_risk`
- محاسبه Quality Score کامیونیتی و وضعیت درآمد `normal`، `monitored`، `partial_hold` یا `frozen`
- تشخیص الگوهایی مانند join/leave loop، mass join، پروفایل مشکوک، رفتار رباتی و click farm
- محاسبه مجدد دوره‌ای امتیازها هر شش ساعت
- پاسخ‌گویی request/reply به سایر سرویس‌ها روی NATS

### HTTP API

- `GET /health`
- `GET /score/user/:id`
- `GET /score/community/:id`
- مسیرهای مدیریتی بازبینی و محاسبه مجدد زیر `/admin/*` با هدر `X-Admin-Key`

### ارتباطات NATS

Fraud Engine رویدادهای عضویت، فعالیت، پروفایل و کمپین را مصرف می‌کند؛ نتیجه امتیازها را با `user.score.updated` و `community.score.updated` منتشر می‌کند و رخدادهای جدی را روی `fraud.detected` می‌فرستد. درخواست امتیاز نیز از طریق `fraud.user.score.request` و `fraud.community.score.request` پشتیبانی می‌شود.

## پیش‌نیازها

- Go 1.25 یا جدیدتر
- PostgreSQL برای `community-service`
- MongoDB برای `fraud-engine`
- NATS برای ارتباط رویدادمحور بین سرویس‌ها

## تنظیمات

فایل واقعی `.env` نباید commit شود. برای هر سرویس نمونه تنظیمات را کپی کنید:

```bash
cp community-service/.env.example community-service/.env
cp fraud-engine/.env.example fraud-engine/.env
```

متغیرهای اصلی Community Service شامل `POSTGRES_DSN`، تنظیمات `NATS_*`، `PORT` و `ADMIN_KEY` است. Fraud Engine از `MONGO_URI`، `MONGO_DB`، تنظیمات `NATS_*`، `PORT` و `ADMIN_KEY` استفاده می‌کند. `ADMIN_KEY` در Fraud Engine الزامی است و سرویس بدون آن بالا نمی‌آید.

## اجرا و تست

از ریشه مخزن:

```bash
go test ./community-service/... ./fraud-engine/... ./shared/...
go run ./community-service/cmd
go run ./fraud-engine/cmd
```

ساخت imageها باید با context ریشه مخزن انجام شود:

```bash
docker build -f community-service/Dockerfile -t community-service .
docker build -f fraud-engine/Dockerfile -t fraud-engine .
```

## نکات شناخته‌شده

- مسیر فعال NATS در Community Service از `engine.RegisterNATSListeners` استفاده می‌کند؛ listener موازی موجود در API در startup ثبت نمی‌شود.
- ذخیره فعالیت اعضا در Community Service به MongoDB متکی است، اما اتصال MongoDB در startup فعلی مقداردهی نشده است؛ رسیدن `community.activity.updated` تا زمان اصلاح این مسیر می‌تواند سرویس را متوقف کند.
- بعضی ورودی‌های Fraud Engine مانند `profile.updated` و `campaign.completed` هنوز producer فعال ندارند؛ بنابراین بخشی از مؤلفه‌های امتیاز تا اتصال کامل producerها داده واقعی دریافت نمی‌کنند.
- جزئیات فنی و کارهای باقی‌مانده هر سرویس در `community-service/NEEDS.md` و `fraud-engine/NEEDS.md` ثبت شده است.

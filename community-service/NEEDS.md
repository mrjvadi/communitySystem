# community-service — چه چیزی نیاز داریم (بررسی ۲۰۲۶-۰۷-۰۴)

## وضعیت فعلی (خلاصه‌ی واقعی، نه قدیمی)

سرویس کوچک است (۶ فایل، ۱۱۶۴ خط) ولی منطق اصلی‌اش — ثبت community، پیوند
دعوت قابل‌ردیابی (`InviteHash`)، پنجره‌ی اعتبارسنجی (`ValidationWindowSec`)،
و تقسیم درآمد بر اساس نوع (گروه ۵۰/۴۰/۱۰، کانال ۹۰/۰/۱۰) — واقعاً پیاده
شده و با `campaign.revenue.generated` از ads-bot سیم‌کشی شده است.
idempotency هم روی رویداد NATS بدون auth رعایت شده
(`store.go:131-145`, `FindRevenueByCampaignCommunity`).

اما بازبینی فرش main.go در برابر دو فایل هندلر NATS، یک ناهماهنگی
ساختاری جدی پیدا کرد (بخش بعد).

## چیزی که واقعاً کم است (با file:line برای هر ادعا)

1. **کل `api.Handler.RegisterNATSListeners` (در `internal/api/api.go:198-261`)
   کد مرده است — هرگز صدا زده نمی‌شود.**
   `cmd/main.go:69` فقط `eng.RegisterNATSListeners(nc)` را صدا می‌زند
   (نسخه‌ی داخل `internal/engine/nats_handler.go`)؛ هیچ خطی در `main.go`
   `apiHandler.RegisterNATSListeners(...)` را فراخوانی نمی‌کند. یعنی
   subscribe های زیر که فقط در نسخه‌ی api.go تعریف شده‌اند هرگز فعال
   نمی‌شوند: `membership.validate_response` (تنها مسیر تأیید دستیِ اعتبار
   عضو از بیرون) و `community.revenue.generated` (نام دیگری از رویداد
   درآمد که با `campaign.revenue.generated` در نسخه‌ی engine فرق دارد).
   دو پیاده‌سازی موازی و ناهم‌خوان از همان قابلیت در یک سرویس نگه داشته
   شده؛ فقط یکی زنده است.

2. **باگ واقعی و قابل‌بازتولید: اگر `community.activity.updated` واقعاً
   برسد، سرویس پنیک می‌کند (nil pointer).**
   `cmd/main.go:51`: `st := store.New(db, nil)` — پارامتر دوم (اتصال
   MongoDB) عمداً `nil` پاس داده می‌شود، با کامنت «community-service فعلاً
   فقط PostgreSQL دارد». اما `internal/engine/nats_handler.go:71-89`
   (فعال، چون در `engine.RegisterNATSListeners` است) روی این subject
   `e.store.UpdateMemberActivity(...)` را صدا می‌زند، و
   `internal/store/store.go:171-181` (`UpdateMemberActivity`) و
   `store.go:183-194` (`GetActiveMembers`) هر دو مستقیماً
   `s.mongo.Collection(...)` را فراخوانی می‌کنند. چون `s.mongo == nil`،
   اولین رویداد واقعی `community.activity.updated` باعث nil pointer
   dereference و کرش پروسه می‌شود. (خوشبختانه در عمل هنوز رخ نداده، چون —
   طبق یافته‌ی مشترک با fraud-engine/NEEDS.md — هیچ رباتی این subject را
   publish نمی‌کند؛ ولی این یک بمب‌ساعتی واقعی است، نه فرضی.)

3. **`IncrementMemberCount` تعریف شده ولی هرگز صدا زده نمی‌شود.**
   `internal/store/store.go:250-256` — فقط `DecrementMemberCount`
   (در `HandleLeave`، `engine.go:247`) استفاده می‌شود. یعنی
   `Community.MemberCount` فقط کاهش پیدا می‌کند، هیچ‌وقت افزایش —
   بعد از مدتی این عدد نادرست و منفی‌گرا می‌شود (`GREATEST(...,0)` جلوی
   منفی شدن را می‌گیرد ولی درستی آماری را تضمین نمی‌کند).

4. **کامنت گمراه‌کننده (نه باگ):** `engine.go:246` می‌گوید «آپدیت تعداد
   اعضا در MongoDB» ولی `DecrementMemberCount` واقعاً روی PostgreSQL
   (`s.pg`, ستون `member_count`) کار می‌کند. فقط مستندسازی اشتباه است،
   رفتار درست است — لازم به تغییر کد نیست، فقط کامنت.

5. **`community.reward.created` منتشر می‌شود ولی هیچ مصرف‌کننده‌ای در کل
   مونورپو ندارد.** `engine.go:180-186` این subject را برای پاداش هر عضوِ
   فعال از استخر ۴۰٪ (فقط گروه) publish می‌کند و همزمان یک
   `CommunityDistribution` با `Status: "pending"` در دیتابیس می‌سازد
   (`engine.go:188-192`). `grep -rln "community.reward.created"
   --include="*.go" .` فقط همین یک فایل را نشان می‌دهد — نه در
   revenue-service، نه در botpay، نه جای دیگر چیزی این subject را
   subscribe نمی‌کند. یعنی اعضای گروه هیچ‌وقت واقعاً پول این سهم را
   دریافت نمی‌کنند؛ فقط رکورد "pending" در `CommunityDistribution` باقی
   می‌ماند برای همیشه. (مقایسه کنید با سهم owner/platform در همان تابع —
   خطوط ۱۲۳-۱۴۰ — که با `earning.created` واقعی به revenue-service
   می‌رسند؛ فقط مسیر اعضا این پل را ندارد.)

## این‌ها را در سرویس‌های دیگر هم نوشتم (اگر مرتبط بود)

- **fraud-engine/NEEDS.md و ads-bot/NEEDS.md** — دلیل ریشه‌ای این‌که یافته‌ی
  #2 بالا («اگر برسد پنیک می‌کند») هنوز رخ نداده: هیچ رباتی
  `community.activity.updated` را publish نمی‌کند در عمل (فقط تعریف در
  `member-bot/internal/events/publisher.go:151-166` وجود دارد، بدون
  فراخوان). جزئیات کامل در `fraud-engine/NEEDS.md`.
- **revenue-service/NEEDS.md و botpay/NEEDS.md** — این سرویس هم برای سهم
  owner/platform به `earning.created` تکیه می‌کند (`engine.go:124-140`) که
  طبق یافته‌ی بحرانی مشترک، در انتهای مسیر توسط revenue-service با یک
  HTTP client به سمت botpay ارسال می‌شود که اصلاً REST سرور ندارد — یعنی
  حتی همان سهم owner/platform هم (که برخلاف سهم اعضا واقعاً به
  revenue-service می‌رسد) در عمل هرگز واریز نمی‌شود.

## به‌روزرسانی ۲۰۲۶-۰۷-۰۶: جداسازی دیتابیس
`community-service` حالا دیتابیس Postgres مخصوص خودش (`community`) را دارد — دیگر با بقیه‌ی
سرویس‌های مرکزی مشترک نیست (بخش Mongo آن، برای فعالیت اعضا، بدون تغییر ماند).

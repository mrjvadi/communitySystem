# fraud-engine — چه چیزی نیاز داریم (بررسی ۲۰۲۶-۰۷-۰۴)

## وضعیت فعلی (خلاصه‌ی واقعی، نه قدیمی)

سرویس کاملاً روی MongoDB است (نه رابطه‌ای — `cmd/main.go:47-62` مستقیماً
`mongo.Connect` می‌کند)، برخلاف تصور اولیه‌ای که از فهرست جدول‌های CLAUDE.md
ممکن است برداشت شود (`UserProfile`, `UserProfileHistory`, ... همه
document هستند، نه GORM). امتیازدهی دوگانه (کاربر + community) با
`scorer.UserScorer`/`scorer.CommunityScorer` پیاده شده و — نکته‌ی مثبت
مهم — این تنها یکی از پنج سرویسِ من است که واقعاً فایل تست دارد:
`internal/scorer/scorer_test.go`, `user_scorer_test.go`,
`community_scorer_test.go`. auth روی endpoint های ادمین با
`X-Admin-Key` middleware درست پیاده شده (`internal/api/api.go:169-175`)
و روت‌های حساس (`recalc`, `fraud-events`, `user/:id/profile`) همه پشت آن
گروه‌بندی شده‌اند (`api.go:32-37`).

نقطه‌ی ضعف واقعی سرویس، نه در امتیازدهی، بلکه در ورودی‌های داده است — چند
مسیر NATS که قرار بوده امتیازدهی را تغذیه کنند در عمل هرگز داده‌ای دریافت
نمی‌کنند (بخش بعد).

## چیزی که واقعاً کم است (با file:line برای هر ادعا)

1. **`profile.updated` و `campaign.completed` — دو subject که fraud-engine
   subscribe می‌کند ولی هیچ‌کس در کل مونورپو منتشر نمی‌کند.**
   `internal/processor/processor.go:80-100` هندلر برای هر دو دارد
   (`handleProfileUpdate`, `handleCampaignComplete`)، ولی:
   `grep -rn "\"profile.updated\"" --include="*.go" .` و
   `grep -rn "\"campaign.completed\"" --include="*.go" .` (خارج از خود
   fraud-engine) هر دو صفر نتیجه برمی‌گردانند. یعنی `TotalCampaigns`/
   `AdCompletions` در `UserProfile` (که در محاسبه‌ی امتیاز کاربر استفاده
   می‌شوند — رجوع به `user_scorer.go`) همیشه صفر باقی می‌مانند مگر از مسیر
   دیگری (که پیدا نشد) پر شوند.

2. **`fraud.event.activity` / `community.activity.updated` — تعریف‌شده و
   قابل‌مصرف، ولی هیچ‌وقت واقعاً trigger نمی‌شود چون فرستنده‌اش صدا زده
   نمی‌شود.**
   `processor.go:66-78` (subscribe به `community.activity.updated`) و
   `processor.go:411-423` (subscribe به `fraud.event.activity`) هر دو
   فعال و درست نوشته شده‌اند. مشکل در طرف فرستنده است:
   `member-bot/internal/events/publisher.go:153-166`
   (`func (p *Publisher) PublishActivity(...)`) دقیقاً همین دو subject را
   منتشر می‌کند، اما `grep -rn "PublishActivity" member-bot --include="*.go"`
   نشان می‌دهد این تابع در کل member-bot فقط همان‌جا تعریف شده و هیچ
   handler پیام گروهی (`OnText` یا مشابه) آن را صدا نمی‌زند. نتیجه: امتیاز
   فعالیت (`ActivityScore` در community-service، و بخش فعالیت‌محور امتیاز
   کاربر در fraud-engine) امروز عملاً همیشه صفر/غیرفعال است، نه به این
   دلیل که scorer باگ دارد بلکه چون هیچ داده‌ی فعالیتی هرگز تولید نمی‌شود.

3. **Mismatch نامگذاری پارامتر در مسیر join — منشأ آن در member-bot است،
   ولی مستقیماً کیفیت attribution ورودیِ fraud-engine را کم می‌کند.**
   `shared/pkg/fraudclient/client.go:106-113` امضای
   `ReportJoin(telegramID, communityID int64, source, campaignID string)`
   دارد، اما `member-bot/internal/events/publisher.go:124-126` این تابع
   را با `p.fraud.ReportJoin(telegramID, chatID, source, inviteHash)`
   صدا می‌زند — یعنی چیزی که fraud-engine `campaign_id` تصور می‌کند
   (`processor.go:51`: `CampaignID: e.campaignID` در `UserMembership`)
   در واقع `invite_hash` گروه/کانال است، نه شناسه‌ی کمپین تبلیغاتی
   ads-bot. خود کد این را با کامنت صریح در `publisher.go:120-123`
   تأیید می‌کند («این دو مفهوم متفاوتند و فاز بعد باید...»). یعنی هر
   گزارش/تحلیل آینده که بخواهد fraud را به یک کمپین خاص ads-bot نسبت دهد،
   داده‌ی نادرست خواهد خواند.

4. **زمان‌بندی recalc دوره‌ای (`RunPeriodicRecalc`, هر ۶ ساعت،
   `processor.go:271-284`) و batch pagination (`batchRecalcUsers`,
   `processor.go:287-319`) درست نوشته شده‌اند و مشکلی در آن‌ها پیدا نشد —
   ذکر شد چون سرویس در این بخش کامل‌تر از انتظار است، نه ناقص.

## این‌ها را در سرویس‌های دیگر هم نوشتم (اگر مرتبط بود)

- **community-service/NEEDS.md** — یافته‌ی #2 بالا (عدم فراخوانی
  `PublishActivity`) دقیقاً همان دلیلی است که مسیر
  `community.activity.updated` در community-service هم هرگز واقعاً اجرا
  نمی‌شود؛ آنجا یک ریسک جدی‌تر هم هست (nil pointer در صورت رسیدن این
  event) که در `community-service/NEEDS.md` با جزئیات کامل آمده.
- **ads-bot/NEEDS.md** — یافته‌ی #3 بالا (mismatch نام‌گذاری
  campaign_id/invite_hash) در آنجا هم به‌عنوان یک وابستگی متقابل ذکر شده.

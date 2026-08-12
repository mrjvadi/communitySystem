// Package logger implements ports.Logger using go.uber.org/zap.
// To swap to slog: implement ports.Logger in a new package and wire in main.go.
package logger

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

// SubjLogEvents — همه‌ی سرویس‌ها لاگ‌های سطح Warn به بالا را روی همین
// subject منتشر می‌کنند؛ سرویس log-collector این‌ها را جمع می‌کند.
// Debug/Info هرگز روی NATS منتشر نمی‌شوند — فیلتر در همان لحظه‌ی تولید لاگ
// انجام می‌شود تا حجم پیام روی NATS زیاد نشود.
const SubjLogEvents = "logs.events"

// SubjHeartbeat — هر سرویسی که AttachNATS صدا بزند، خودکار و بدون هیچ کدِ
// اضافه‌ای، هر heartbeatInterval یک‌بار روی این subject یک پیامِ حضور
// منتشر می‌کند. log-collector از این برای ساختِ داشبوردِ وضعیتِ زنده‌ی
// سرویس‌ها استفاده می‌کند (رجوع log-collector/internal/status).
const SubjHeartbeat = "service.heartbeat"

// heartbeatInterval فاصله‌ی انتشارِ heartbeat. مصرف‌کننده (log-collector)
// اگر بیش از ~۳ برابرِ این فاصله از یک سرویس چیزی نشنود، آن را down فرض می‌کند.
const heartbeatInterval = 20 * time.Second

// HeartbeatEvent ساختار پیامِ حضورِ دوره‌ای هر سرویس. StartedAt زمانِ ساختِ
// همین logger (نزدیک‌ترین لحظه به شروعِ واقعیِ پروسه) است — نه زمانِ اولین
// heartbeatی که log-collector دیده — تا اگر خودِ log-collector ری‌استارت
// شود، آپ‌تایمِ نمایش‌داده‌شده باز هم درست بماند (چون هر heartbeat خودش
// StartedAt واقعی را حمل می‌کند، نه چیزی که log-collector محلی حساب کند).
//
// InstanceID اختیاری است — سرویس‌های تک‌نسخه‌ای (botmanager, botpay, ...)
// آن را خالی می‌گذارند. ربات‌های محصولِ چندنسخه‌ای (uploader-bot, vpn-bot,
// archive-bot, member-bot) که هر کدام می‌توانند هم‌زمان چند instance برای
// چند مشتری داشته باشند، آن را با شناسه‌ی همان instance (مثلاً "bot_12345")
// پر می‌کنند تا log-collector بتواند «۴ از ۵ instance بالا» را جدا از هم
// بشمارد، نه این‌که چند instance زیرِ یک کلیدِ Service با هم تداخل کنند.
type HeartbeatEvent struct {
	Service    string `json:"service"`
	Timestamp  int64  `json:"ts"`
	StartedAt  int64  `json:"started_at"`
	InstanceID string `json:"instance_id,omitempty"`
}

// LogEvent ساختار پیامی که برای هر لاگ Warn/Error/Fatal روی NATS می‌رود.
type LogEvent struct {
	Service   string         `json:"service"`
	Level     string         `json:"level"` // warn | error | fatal
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	Timestamp int64          `json:"ts"`
}

// ZapLogger wraps *zap.Logger and implements ports.Logger.
type ZapLogger struct {
	z *zap.Logger

	// natsSink اختیاری — اگر با AttachNATS تنظیم شود، هر Warn/Error/Fatal
	// علاوه بر خروجی محلی، روی NATS هم منتشر می‌شود (best-effort، هرگز
	// نباید چیزی را block یا panic کند — لاگ‌گیری نباید خودش خطرِ سرویس شود).
	nc          *natsclient.Client
	serviceName string
	instanceID  string
	startedAt   time.Time
}

var _ ports.Logger = (*ZapLogger)(nil)

// New creates a ZapLogger.
// production=true: JSON output, no caller info in debug.
// production=false: console output, colored, with caller.
func New(production bool) (*ZapLogger, error) {
	var z *zap.Logger
	var err error
	if production {
		z, err = zap.NewProduction()
	} else {
		cfg := zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		z, err = cfg.Build()
	}
	if err != nil {
		return nil, err
	}
	return &ZapLogger{z: z, startedAt: time.Now()}, nil
}

// MustNew panics if logger creation fails.
func MustNew(production bool) *ZapLogger {
	l, err := New(production)
	if err != nil {
		panic(err)
	}
	return l
}

func (l *ZapLogger) Debug(msg string, fields ...ports.Field) { l.z.Debug(msg, toZap(fields)...) }
func (l *ZapLogger) Info(msg string, fields ...ports.Field)  { l.z.Info(msg, toZap(fields)...) }

func (l *ZapLogger) Warn(msg string, fields ...ports.Field) {
	l.z.Warn(msg, toZap(fields)...)
	l.publish("warn", msg, fields)
}

func (l *ZapLogger) Error(msg string, fields ...ports.Field) {
	l.z.Error(msg, toZap(fields)...)
	l.publish("error", msg, fields)
}

func (l *ZapLogger) Fatal(msg string, fields ...ports.Field) {
	// نکته: l.z.Fatal خودش os.Exit(1) صدا می‌زند، پس باید publish را قبل از
	// آن انجام دهیم وگرنه هرگز اجرا نمی‌شود.
	l.publish("fatal", msg, fields)
	l.z.Fatal(msg, toZap(fields)...)
}

func (l *ZapLogger) With(fields ...ports.Field) ports.Logger {
	return &ZapLogger{z: l.z.With(toZap(fields)...), nc: l.nc, serviceName: l.serviceName, instanceID: l.instanceID, startedAt: l.startedAt}
}

// AttachNATS یک sink اختیاری به NATS وصل می‌کند — از این لحظه به بعد، هر
// Warn/Error/Fatal علاوه بر خروجی محلی (stdout/فایل)، روی SubjLogEvents هم
// منتشر می‌شود تا سرویس log-collector آن را جمع کند. صدا زدن این متد کاملاً
// اختیاری است؛ بدون آن، رفتار لاگر دقیقاً مثل قبل (فقط محلی) باقی می‌ماند.
//
// instanceID اختیاری (variadic تا امضای قبلی نشکند) — فقط برای ربات‌های
// محصولِ چندنسخه‌ای لازم است، رجوع کامنتِ HeartbeatEvent.InstanceID.
func (l *ZapLogger) AttachNATS(nc *natsclient.Client, serviceName string, instanceID ...string) {
	l.nc = nc
	l.serviceName = serviceName
	if len(instanceID) > 0 {
		l.instanceID = instanceID[0]
	}
	go l.heartbeatLoop()
}

// heartbeatLoop یک goroutine در پس‌زمینه که تا پایانِ عمرِ پروسه هر
// heartbeatInterval یک‌بار حضورِ این سرویس را منتشر می‌کند — کاملاً
// best-effort و panic-safe، دقیقاً مثلِ publish. نیازی به Stop/context ندارد
// چون با خروجِ خودِ پروسه تمام می‌شود، و اختیاری‌بودنِ AttachNATS یعنی
// سرویس‌هایی که آن را صدا نمی‌زنند هیچ goroutine اضافه‌ای ندارند.
func (l *ZapLogger) heartbeatLoop() {
	defer func() { _ = recover() }()
	l.publishHeartbeat()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.publishHeartbeat()
	}
}

func (l *ZapLogger) publishHeartbeat() {
	if l.nc == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = l.nc.PublishCore(SubjHeartbeat, HeartbeatEvent{
		Service:    l.serviceName,
		Timestamp:  time.Now().Unix(),
		StartedAt:  l.startedAt.Unix(),
		InstanceID: l.instanceID,
	})
}

// publish تلاش می‌کند لاگ را روی NATS منتشر کند — کاملاً best-effort:
// nil-safe (اگر AttachNATS صدا زده نشده باشد کاری نمی‌کند)، panic-safe
// (recover می‌کند تا یک مشکل غیرمنتظره در marshal/publish هرگز خودِ سرویس
// را crash نکند)، و هرگز مسیر اصلی برنامه را block نمی‌کند چون NATS core
// publish خودش async و بدون انتظار ack است.
func (l *ZapLogger) publish(level, msg string, fields []ports.Field) {
	if l.nc == nil {
		return
	}
	defer func() { _ = recover() }()

	fm := make(map[string]any, len(fields))
	for _, f := range fields {
		// خیلی از فراخوانی‌ها ports.F("err", err) هستند؛ نوع error معمولاً
		// struct بدون فیلد export‌شده است و json.Marshal آن را {} خالی
		// می‌کند — این‌جا صریحاً به رشته‌ی Error() تبدیل می‌شود تا در Mongo/
		// تلگرام قابل‌خواندن باشد.
		if err, ok := f.Value.(error); ok {
			fm[f.Key] = err.Error()
			continue
		}
		fm[f.Key] = f.Value
	}
	_ = l.nc.PublishCore(SubjLogEvents, LogEvent{
		Service:   l.serviceName,
		Level:     level,
		Message:   msg,
		Fields:    fm,
		Timestamp: time.Now().Unix(),
	})
}

func toZap(fields []ports.Field) []zap.Field {
	out := make([]zap.Field, len(fields))
	for i, f := range fields {
		out[i] = zap.Any(f.Key, f.Value)
	}
	return out
}

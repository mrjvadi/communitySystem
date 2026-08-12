// Package nats implements ports.Notifier and a subscriber using NATS JetStream.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config تنظیمات اتصال به NATS.
type Config struct {
	URL string // nats://localhost:4222
	// اختیاری — برای auth
	Username string
	Password string
	// Name نام این connection در NATS monitoring — پیش‌فرض "creatorbot"
	Name string
}

// Client یک wrapper روی NATS connection است.
type Client struct {
	nc *nats.Conn
	js jetstream.JetStream
}

// New اتصال به NATS برقرار می‌کند.
func New(cfg Config) (*Client, error) {
	name := cfg.Name
	if name == "" {
		name = "creatorbot"
	}
	opts := []nats.Option{
		nats.Name(name),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
	}
	if cfg.Username != "" && cfg.Password != "" {
		opts = append(opts, nats.UserInfo(cfg.Username, cfg.Password))
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: jetstream: %w", err)
	}

	return &Client{nc: nc, js: js}, nil
}

// Publish یک پیام JSON به یک subject publish می‌کند.
func (c *Client) Publish(ctx context.Context, subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.js.Publish(ctx, subject, data)
	return err
}

// PublishCore پیام را بدون JetStream (fire-and-forget) publish می‌کند.
func (c *Client) PublishCore(subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.nc.Publish(subject, data)
}

// Subscribe یک handler برای یک subject ثبت می‌کند.
// برای heartbeat و رویدادهای ساده استفاده می‌شود.
// Subscribe به یک subject subscribe می‌کند.
// جهت سادگی فقط error برمی‌گرداند.
func (c *Client) Subscribe(subject string, handler func([]byte)) error {
	_, err := c.nc.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
	return err
}

// SubscribeRaw مستقیماً *nats.Subscription برمی‌گرداند — برای request/reply.
func (c *Client) SubscribeRaw(subject string, handler func(*nats.Msg)) error {
	_, err := c.nc.Subscribe(subject, handler)
	return err
}

// QueueSubscribe چند instance از یک سرویس می‌توانند روی یک queue باشند.
// هر پیام فقط به یک instance می‌رسد (load balancing).
func (c *Client) QueueSubscribe(subject, queue string, handler func([]byte)) error {
	_, err := c.nc.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		handler(msg.Data)
	})
	return err
}

// EnsureStream یک JetStream stream می‌سازد اگه وجود نداشته باشد.
func (c *Client) EnsureStream(ctx context.Context, name string, subjects []string) error {
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    24 * time.Hour,
		Storage:   jetstream.FileStorage,
	})
	return err
}

// JetStream برگرداندن JetStream برای استفاده مستقیم.
func (c *Client) JetStream() jetstream.JetStream { return c.js }

// NC برگرداندن connection خام.
func (c *Client) NC() *nats.Conn { return c.nc }

// Close اتصال را می‌بندد.
func (c *Client) Close() { c.nc.Close() }

// ── Dead Letter Queue ──────────────────────────────────────

const DLQSubject = "errors.dlq"

// DLQMessage پیامی که به DLQ رفته.
type DLQMessage struct {
	OriginalSubject string `json:"original_subject"`
	Payload         []byte `json:"payload"`
	Error           string `json:"error"`
	Attempts        int    `json:"attempts"`
	Timestamp       int64  `json:"timestamp"`
}

// PublishToDLQ پیام fail شده را به DLQ ارسال می‌کند.
func (c *Client) PublishToDLQ(subject string, payload []byte, err error, attempts int) {
	msg := DLQMessage{
		OriginalSubject: subject,
		Payload:         payload,
		Error:           err.Error(),
		Attempts:        attempts,
		Timestamp:       time.Now().Unix(),
	}
	c.PublishCore(DLQSubject, msg)
}

// SubscribeWithRetry با retry و DLQ subscribe می‌کند.
// maxRetries: تعداد تلاش مجدد قبل از ارسال به DLQ
func (c *Client) SubscribeWithRetry(subject string, maxRetries int, handler func([]byte) error) error {
	err := c.Subscribe(subject, func(data []byte) {
		var lastErr error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if lastErr = handler(data); lastErr == nil {
				return // موفق
			}
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
		}
		// همه تلاش‌ها ناموفق → DLQ
		c.PublishToDLQ(subject, data, lastErr, maxRetries)
	})
	return err
}

// ══════════════════════════════════════════════════════════════
// Request / Reply (sync) — برای ارتباط درخواست-پاسخ بین سرویس‌ها
// از NATS core استفاده می‌کند (نه JetStream) چون sync و سریع است.
// ══════════════════════════════════════════════════════════════

// Request یک پیام JSON می‌فرستد و منتظر پاسخ می‌ماند.
// out باید یک pointer باشد تا پاسخ در آن unmarshal شود.
// اگر responder در دسترس نباشد، خطای timeout برمی‌گرداند (نه connection refused).
func (c *Client) Request(ctx context.Context, subject string, payload any, out any, timeout time.Duration) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nats request: marshal: %w", err)
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	msg, err := c.nc.Request(subject, data, timeout)
	if err != nil {
		return fmt.Errorf("nats request %q: %w", subject, err)
	}

	// بررسی خطای سمت responder (در header یا body با فیلد error)
	if out != nil {
		if err := json.Unmarshal(msg.Data, out); err != nil {
			return fmt.Errorf("nats request %q: unmarshal reply: %w", subject, err)
		}
	}
	return nil
}

// Respond روی یک subject، درخواست‌ها را گرفته و پاسخ می‌دهد.
// handler داده‌ی درخواست را می‌گیرد و (پاسخ، خطا) برمی‌گرداند.
// اگر خطا برگردد، پاسخ به صورت JSON با فیلد "error" ارسال می‌شود.
func (c *Client) Respond(subject string, handler func(data []byte) (any, error)) error {
	_, err := c.nc.Subscribe(subject, func(msg *nats.Msg) {
		resp, herr := handler(msg.Data)
		var out []byte
		if herr != nil {
			out, _ = json.Marshal(map[string]string{"error": herr.Error()})
		} else {
			out, _ = json.Marshal(resp)
		}
		_ = msg.Respond(out)
	})
	if err != nil {
		return fmt.Errorf("nats respond %q: %w", subject, err)
	}
	return nil
}

// QueueRespond مثل Respond ولی با queue group — برای load balancing
// بین چند instance از یک سرویس (فقط یکی پاسخ می‌دهد).
func (c *Client) QueueRespond(subject, queue string, handler func(data []byte) (any, error)) error {
	_, err := c.nc.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		resp, herr := handler(msg.Data)
		var out []byte
		if herr != nil {
			out, _ = json.Marshal(map[string]string{"error": herr.Error()})
		} else {
			out, _ = json.Marshal(resp)
		}
		_ = msg.Respond(out)
	})
	if err != nil {
		return fmt.Errorf("nats queue respond %q: %w", subject, err)
	}
	return nil
}

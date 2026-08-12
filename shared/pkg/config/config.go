// Package config provides a generic environment variable loader using Viper.
// Each service defines its own config struct and calls Load() to populate it.
package config

import (
	"fmt"
	"reflect"

	"github.com/spf13/viper"
)

// Load reads .env file and environment variables, then unmarshals into dst.
// dst must be a pointer to a struct with mapstructure tags.
//
// Usage in any service:
//
//	type Config struct {
//	    BotToken string `mapstructure:"BOT_TOKEN"`
//	    DSN      string `mapstructure:"POSTGRES_DSN"`
//	}
//	var C Config
//	config.Load(&C)
func Load(dst any, envFile ...string) error {
	file := ".env"
	if len(envFile) > 0 && envFile[0] != "" {
		file = envFile[0]
	}

	viper.SetConfigFile(file)
	viper.AutomaticEnv()
	_ = viper.ReadInConfig() // ok if file is missing (pure env vars)

	// باگ واقعی (۲۰۲۶-۰۷-۱۰، کشف در deploy داینامیک uploader-bot):
	// AutomaticEnv + Unmarshal فقط کلیدهایی را از env می‌خواند که viper از
	// قبل «بشناسد» (از فایل config یا default). وقتی فایل .env وجود ندارد —
	// دقیقاً حالت container هایی که agentmanager می‌سازد و همه‌چیز را با
	// env var تزریق می‌کند — هیچ کلیدی شناخته نیست و Unmarshal همه‌چیز را
	// خالی برمی‌گرداند (BOT_TOKEN خالی → crash-loop با «invalid bot token»).
	// راه‌حل استاندارد: هر کلید mapstructure صریحاً BindEnv شود.
	bindEnvs(reflect.TypeOf(dst))

	if err := viper.Unmarshal(dst); err != nil {
		return fmt.Errorf("config: unmarshal: %w", err)
	}
	return nil
}

// bindEnvs همه‌ی تگ‌های mapstructure ساختار (و ساختارهای تو در تو) را به
// viper.BindEnv می‌دهد تا env var ها حتی بدون فایل .env خوانده شوند.
func bindEnvs(t reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			bindEnvs(ft)
			continue
		}
		if tag, ok := f.Tag.Lookup("mapstructure"); ok && tag != "" && tag != "-" {
			_ = viper.BindEnv(tag)
		}
	}
}

// MustLoad is like Load but panics on error.
func MustLoad(dst any, envFile ...string) {
	if err := Load(dst, envFile...); err != nil {
		panic(err)
	}
}

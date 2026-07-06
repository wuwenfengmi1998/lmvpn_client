// Package i18n provides internationalization support for the LMVPN
// UI. It loads translation catalogs (embedded TOML files) via go-i18n
// and exposes a simple T() lookup helper. The active language is
// chosen from the user's saved preference or, when unset, detected
// from the operating system locale.
package i18n

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/jeandeaual/go-locale"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed en.toml zh-Hans.toml
var localeFS embed.FS

// Supported language identifiers. LangAuto means "detect from the OS".
const (
	LangAuto   = "auto"
	LangEn     = "en"
	LangZhHans = "zh-Hans"
)

var (
	mu          sync.RWMutex
	bundle      *i18n.Bundle
	localizer   *i18n.Localizer
	currentLang string
)

// Init loads the embedded catalogs and activates the given language.
// An empty string or LangAuto triggers OS locale detection.
func Init(lang string) error {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	for _, name := range []string{"en.toml", "zh-Hans.toml"} {
		data, err := localeFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("i18n: read %s: %w", name, err)
		}
		if _, err := bundle.ParseMessageFileBytes(data, name); err != nil {
			return fmt.Errorf("i18n: parse %s: %w", name, err)
		}
	}

	resolved := resolveLanguage(lang)
	localizer = i18n.NewLocalizer(bundle, resolved)
	currentLang = resolved
	return nil
}

// SetLanguage switches the active language at runtime. The caller is
// responsible for rebuilding any cached UI strings afterwards.
func SetLanguage(lang string) {
	mu.Lock()
	defer mu.Unlock()
	resolved := resolveLanguage(lang)
	localizer = i18n.NewLocalizer(bundle, resolved)
	currentLang = resolved
}

// CurrentLanguage returns the active language tag (e.g. "en", "zh-Hans").
func CurrentLanguage() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

// T translates a message identified by key. An optional map is used as
// template data (e.g. {"name": "work"}). If the bundle is not yet
// initialised or the key is missing, the key itself is returned.
func T(key string, data ...interface{}) string {
	mu.RLock()
	loc := localizer
	mu.RUnlock()
	if loc == nil {
		return key
	}
	cfg := &i18n.LocalizeConfig{MessageID: key}
	if len(data) > 0 {
		if m, ok := data[0].(map[string]interface{}); ok {
			cfg.TemplateData = m
		}
	}
	msg, err := loc.Localize(cfg)
	if err != nil || msg == "" {
		return key
	}
	return msg
}

// DetectSystemLanguage inspects the OS locale list and returns the
// best supported match. Any Chinese variant maps to zh-Hans; everything
// else falls back to en.
func DetectSystemLanguage() string {
	tags, err := locale.GetLocales()
	if err == nil {
		for _, t := range tags {
			if strings.HasPrefix(strings.ToLower(t), "zh") {
				return LangZhHans
			}
		}
	}
	// Fall back to the single-locale API.
	tag, err := locale.GetLocale()
	if err == nil && strings.HasPrefix(strings.ToLower(tag), "zh") {
		return LangZhHans
	}
	return LangEn
}

// resolveLanguage maps LangAuto / "" to a concrete language tag.
func resolveLanguage(lang string) string {
	if lang == "" || lang == LangAuto {
		return DetectSystemLanguage()
	}
	return lang
}

// Package i18n loads the embedded message catalogs and serves lookups.
package i18n

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

//go:embed locales/*.toml
var localeFS embed.FS

// DefaultLanguage is used when no preference is configured.
const DefaultLanguage = "en"

var (
	mu       sync.RWMutex
	current  = DefaultLanguage
	catalogs = map[string]map[string]string{}
	loadOnce sync.Once
	loadErr  error
)

// Normalize maps common language tags to a supported code.
func Normalize(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	lang = strings.ReplaceAll(lang, "_", "-")
	switch {
	case strings.HasPrefix(lang, "zh"):
		return "zh"
	case strings.HasPrefix(lang, "en"):
		return "en"
	default:
		return lang
	}
}

// SupportedLang reports whether lang is available.
func SupportedLang(lang string) bool {
	if err := load(); err != nil {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	_, ok := catalogs[Normalize(lang)]
	return ok
}

// Supported lists the available languages.
func Supported() []string {
	if err := load(); err != nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(catalogs))
	for lang := range catalogs {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// Init switches the active language. An empty value keeps the current one.
func Init(lang string) error {
	if err := load(); err != nil {
		return err
	}
	if lang == "" {
		lang = DefaultLanguage
	}
	normalized := Normalize(lang)
	if !SupportedLang(normalized) {
		return fmt.Errorf("unsupported language %q (supported: %s)", lang, strings.Join(Supported(), ", "))
	}
	mu.Lock()
	current = normalized
	mu.Unlock()
	return nil
}

// Lang returns the active language.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T looks a key up, falling back to English and finally to the key itself.
func T(key string) string {
	if err := load(); err != nil {
		return key
	}
	mu.RLock()
	defer mu.RUnlock()
	if catalog, ok := catalogs[current]; ok {
		if value, ok := catalog[key]; ok {
			return value
		}
	}
	if catalog, ok := catalogs[DefaultLanguage]; ok {
		if value, ok := catalog[key]; ok {
			return value
		}
	}
	return key
}

// Tf formats a looked up message.
func Tf(key string, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}

// Keys returns the sorted key set of a catalog; used by tests.
func Keys(lang string) []string {
	if err := load(); err != nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	catalog, ok := catalogs[Normalize(lang)]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(catalog))
	for key := range catalog {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func load() error {
	loadOnce.Do(func() {
		entries, err := localeFS.ReadDir("locales")
		if err != nil {
			loadErr = err
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
				continue
			}
			data, err := localeFS.ReadFile("locales/" + entry.Name())
			if err != nil {
				loadErr = err
				return
			}
			var catalog map[string]string
			if err := toml.Unmarshal(data, &catalog); err != nil {
				loadErr = fmt.Errorf("locales/%s: %w", entry.Name(), err)
				return
			}
			lang := Normalize(strings.TrimSuffix(entry.Name(), ".toml"))
			catalogs[lang] = catalog
		}
	})
	return loadErr
}

// userConfigPath is the per-user preference file. os.UserConfigDir resolves it
// per platform: %AppData%\qegg\config.toml on Windows,
// ~/Library/Application Support/qegg/config.toml on macOS, and
// $XDG_CONFIG_HOME/qegg/config.toml (default ~/.config/qegg/config.toml) on
// Linux and the other Unix systems.
func userConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "qegg", "config.toml"), nil
}

// LoadUserLanguage reads the persisted language preference, if any.
func LoadUserLanguage() string {
	path, err := userConfigPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var config struct {
		Language string `toml:"language"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return ""
	}
	return config.Language
}

// SaveUserLanguage persists the language preference and returns the file path.
func SaveUserLanguage(lang string) (string, error) {
	path, err := userConfigPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	content := "language = \"" + Normalize(lang) + "\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

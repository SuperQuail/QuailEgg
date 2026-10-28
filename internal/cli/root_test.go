package cli

import (
	"runtime"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// isolateUserConfig points the per-user preference file at a temporary
// directory so tests never touch the real profile.
func isolateUserConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	return dir
}

func TestResolveLanguagePrecedence(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("QEGG_LANG", "")

	lang, err := resolveLanguage("")
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if lang != i18n.DefaultLanguage {
		t.Fatalf("default lang = %q, want %q", lang, i18n.DefaultLanguage)
	}

	if _, err := i18n.SaveUserLanguage("zh"); err != nil {
		t.Fatalf("SaveUserLanguage: %v", err)
	}
	lang, err = resolveLanguage("")
	if err != nil {
		t.Fatalf("user preference: %v", err)
	}
	if lang != "zh" {
		t.Fatalf("user preference lang = %q, want zh", lang)
	}

	t.Setenv("QEGG_LANG", "en")
	lang, err = resolveLanguage("")
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if lang != "en" {
		t.Fatalf("env lang = %q, want en", lang)
	}

	lang, err = resolveLanguage("zh-CN")
	if err != nil {
		t.Fatalf("flag: %v", err)
	}
	if lang != "zh" {
		t.Fatalf("flag lang = %q, want zh", lang)
	}
}

func TestResolveLanguageRejectsUnsupported(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("QEGG_LANG", "")

	if _, err := resolveLanguage("klingon"); err == nil {
		t.Fatal("expected an error for an unsupported flag value")
	}
	t.Setenv("QEGG_LANG", "klingon")
	if _, err := resolveLanguage(""); err == nil {
		t.Fatal("expected an error for an unsupported environment value")
	}
}

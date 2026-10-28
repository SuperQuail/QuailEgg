package i18n

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogsShareKeys(t *testing.T) {
	en := Keys("en")
	zh := Keys("zh")
	if len(en) == 0 || len(zh) == 0 {
		t.Fatalf("catalogs did not load: en=%d zh=%d", len(en), len(zh))
	}
	if len(en) != len(zh) {
		t.Errorf("catalog size mismatch: en=%d zh=%d", len(en), len(zh))
	}
	zhSet := map[string]bool{}
	for _, key := range zh {
		zhSet[key] = true
	}
	for _, key := range en {
		if !zhSet[key] {
			t.Errorf("key %q missing from zh", key)
		}
	}
}

func TestDefaultsToEnglish(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatal(err)
	}
	if Lang() != "en" {
		t.Fatalf("lang = %q, want en", Lang())
	}
	if got := T("cmd.check.short"); !strings.Contains(got, "Validate") {
		t.Fatalf("T returned %q", got)
	}
	if got := T("missing.key"); got != "missing.key" {
		t.Fatalf("missing key returned %q", got)
	}
}

func TestSwitchLanguage(t *testing.T) {
	if err := Init("zh-CN"); err != nil {
		t.Fatalf("Init(zh-CN): %v", err)
	}
	if Lang() != "zh" {
		t.Fatalf("lang = %q", Lang())
	}
	if got := T("cmd.check.short"); !strings.Contains(got, "检查") {
		t.Fatalf("T returned %q", got)
	}
	if err := Init("en"); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedLanguage(t *testing.T) {
	if err := Init("klingon"); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
	if err := Init("en"); err != nil {
		t.Fatal(err)
	}
}

// TestNoHardcodedChinese guards the catalog rule: production Go sources must
// not contain CJK literals; all user-facing text belongs in locales/*.toml.
func TestNoHardcodedChinese(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "testdata" || base == "docs" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, r := range string(data) {
			if r >= 0x4e00 && r <= 0x9fff {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s contains hardcoded CJK text; move it to internal/i18n/locales", rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// isolateUserConfig points os.UserConfigDir at a temporary directory.
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

func TestUserConfigPathPortable(t *testing.T) {
	root := isolateUserConfig(t)
	path, err := userConfigPath()
	if err != nil {
		t.Fatalf("userConfigPath: %v", err)
	}
	if !strings.HasPrefix(path, root) {
		t.Fatalf("path %q is not under the isolated config dir %q", path, root)
	}
	if got, want := filepath.Base(path), "config.toml"; got != want {
		t.Fatalf("base = %q, want %q", got, want)
	}
	if got, want := filepath.Base(filepath.Dir(path)), "qegg"; got != want {
		t.Fatalf("parent = %q, want %q", got, want)
	}
}

func TestSaveAndLoadUserLanguage(t *testing.T) {
	root := isolateUserConfig(t)
	if got := LoadUserLanguage(); got != "" {
		t.Fatalf("missing config returned %q", got)
	}
	path, err := SaveUserLanguage("zh-CN")
	if err != nil {
		t.Fatalf("SaveUserLanguage: %v", err)
	}
	if !strings.HasPrefix(path, root) {
		t.Fatalf("saved path %q is not under %q", path, root)
	}
	if got := LoadUserLanguage(); got != "zh" {
		t.Fatalf("LoadUserLanguage = %q, want zh", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `language = "zh"`) {
		t.Fatalf("unexpected config content:\n%s", data)
	}
}

package config

import "testing"

func TestChangelogDefaultsEnabled(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release]\nmode = \"commit\"\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.Release.ChangelogEnabled() {
		t.Fatal("an unset switch must keep the changelog written")
	}
	if cfg.Release.Changelog != nil {
		t.Fatalf("an unset switch must stay nil, got %v", *cfg.Release.Changelog)
	}
}

func TestChangelogDisabled(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release]\nchangelog = false\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Release.ChangelogEnabled() {
		t.Fatal("changelog = false must turn the update off")
	}
}

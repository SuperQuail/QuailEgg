package assets

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// archiveTime makes archives deterministic regardless of build time.
var archiveTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// Build produces the files to attach to the release. A plan with files runs
// the configured commands and collects the matching files; a plan with a main
// package cross-compiles the Go module.
func (p *Plan) Build(ctx context.Context, run execx.Runner, root, version, outDir string) ([]string, error) {
	if err := os.RemoveAll(outDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	if len(p.Files) > 0 {
		return p.buildFiles(ctx, run, root, version, outDir)
	}
	return p.buildGo(ctx, run, root, version, outDir)
}

// buildFiles runs the configured commands and copies every file the patterns
// match into outDir.
func (p *Plan) buildFiles(ctx context.Context, run execx.Runner, root, version, outDir string) ([]string, error) {
	if len(p.Commands) > 0 {
		envRunner, ok := run.(execx.EnvRunner)
		if !ok {
			return nil, errors.New(i18n.T("release.err.assets_runner"))
		}
		plain, tag := VersionParts(version)
		env := append(os.Environ(), "QEGG_VERSION="+plain, "QEGG_TAG="+tag)
		replacer := strings.NewReplacer("{{version}}", plain, "{{tag}}", tag)
		for _, command := range p.Commands {
			line := replacer.Replace(command)
			name, args := shellCommand(line)
			if _, err := envRunner.RunEnv(ctx, root, env, name, args...); err != nil {
				return nil, fmt.Errorf("%s: %w", i18n.Tf("release.err.assets_command", line), err)
			}
		}
	}
	matched, err := matchFiles(root, p.Files)
	if err != nil {
		return nil, err
	}
	if len(matched) == 0 {
		return nil, errors.New(i18n.Tf("release.err.assets_no_match", strings.Join(p.Files, ", ")))
	}
	seen := make(map[string]bool, len(matched))
	names := make([]string, 0, len(matched))
	for _, file := range matched {
		name := filepath.Base(file)
		if seen[name] {
			return nil, errors.New(i18n.Tf("release.err.assets_duplicate", name))
		}
		seen[name] = true
		names = append(names, name)
		if err := copyFile(file, filepath.Join(outDir, name)); err != nil {
			return nil, err
		}
	}
	if err := writeChecksums(outDir, names); err != nil {
		return nil, err
	}
	return listFiles(outDir)
}

// buildGo cross-compiles every target, writes one archive per target and a
// checksums file into outDir, and returns the absolute paths in upload order.
func (p *Plan) buildGo(ctx context.Context, run execx.Runner, root, version, outDir string) ([]string, error) {
	envRunner, ok := run.(execx.EnvRunner)
	if !ok {
		return nil, errors.New(i18n.T("release.err.assets_runner"))
	}
	work, err := os.MkdirTemp(outDir, "build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	plain, _ := VersionParts(version)
	ldflags := strings.Join(expand(p.Ldflags, version), " ")
	archives := make([]string, 0, len(p.Targets))
	main := p.Main
	if main != "." {
		main = "./" + main
	}
	for _, target := range p.Targets {
		binary := p.Name
		if target.OS == "windows" {
			binary += ".exe"
		}
		built := filepath.Join(work, binary)
		env := []string{"GOOS=" + target.OS, "GOARCH=" + target.Arch, "CGO_ENABLED=0"}
		args := []string{"build", "-trimpath", "-buildvcs=false"}
		if ldflags != "" {
			args = append(args, "-ldflags", ldflags)
		}
		args = append(args, "-o", built, main)
		if _, err := envRunner.RunEnv(ctx, root, env, "go", args...); err != nil {
			return nil, fmt.Errorf("build %s: %w", target, err)
		}
		name, err := writeArchive(outDir, p.Name, target, plain, built)
		if err != nil {
			return nil, fmt.Errorf("archive %s: %w", target, err)
		}
		archives = append(archives, name)
	}
	if err := writeChecksums(outDir, archives); err != nil {
		return nil, err
	}
	return listFiles(outDir)
}

// writeArchive packages one binary and returns the archive file name.
func writeArchive(outDir, name string, target Target, version, built string) (string, error) {
	base := fmt.Sprintf("%s_%s_%s_%s", name, version, target.OS, target.Arch)
	entry := filepath.Base(built)
	if target.OS == "windows" {
		file := base + ".zip"
		return file, writeZip(filepath.Join(outDir, file), entry, built)
	}
	file := base + ".tar.gz"
	return file, writeTarGz(filepath.Join(outDir, file), entry, built)
}

func writeTarGz(dest, entry, source string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	header := &tar.Header{
		Name:     entry,
		Mode:     0o755,
		Size:     info.Size(),
		ModTime:  archiveTime,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(tw, src); err != nil {
		out.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		out.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeZip(dest, entry, source string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		out.Close()
		return err
	}
	header.Name = entry
	header.Method = zip.Deflate
	header.Modified = archiveTime
	header.SetMode(0o755)
	writer, err := zw.CreateHeader(header)
	if err != nil {
		out.Close()
		return err
	}
	src, err := os.Open(source)
	if err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(writer, src); err != nil {
		src.Close()
		out.Close()
		return err
	}
	if err := src.Close(); err != nil {
		out.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeChecksums writes checksums.txt for the given file names.
func writeChecksums(dir string, names []string) error {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var b strings.Builder
	for _, name := range sorted {
		sum, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		b.WriteString(sum)
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteByte(10)
	}
	return os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(b.String()), 0o644)
}

// fileSHA256 returns the hex digest of one file.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// listFiles returns the upload order: payload files first, checksums last.
func listFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.SliceStable(files, func(i, j int) bool {
		iSum := strings.HasSuffix(files[i], "checksums.txt")
		jSum := strings.HasSuffix(files[j], "checksums.txt")
		if iSum != jSum {
			return !iSum
		}
		return files[i] < files[j]
	})
	return files, nil
}

// copyFile writes source to dest and keeps its mode.
func copyFile(source, dest string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, info.Mode().Perm())
}

// matchFiles resolves the glob patterns to repository relative slash paths.
// "**" matches any number of path segments.
func matchFiles(root string, patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		dir := staticPrefix(pattern)
		walkRoot := filepath.Join(root, filepath.FromSlash(dir))
		err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == ".quailegg" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !matchPattern(pattern, rel) || seen[rel] {
				return nil
			}
			seen[rel] = true
			out = append(out, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// staticPrefix returns the leading path of a glob up to its first wildcard.
func staticPrefix(pattern string) string {
	parts := strings.Split(filepath.ToSlash(pattern), "/")
	var prefix []string
	for _, part := range parts {
		if strings.ContainsAny(part, "*?[") {
			break
		}
		prefix = append(prefix, part)
	}
	if len(prefix) == 0 {
		return "."
	}
	return path.Join(prefix...)
}

// matchPattern reports whether rel matches pattern, where "**" matches any
// number of segments.
func matchPattern(pattern, rel string) bool {
	return matchParts(strings.Split(filepath.ToSlash(pattern), "/"), strings.Split(rel, "/"))
}

func matchParts(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(parts); i++ {
			if matchParts(pattern[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], parts[0])
	return err == nil && ok && matchParts(pattern[1:], parts[1:])
}

// shellCommand wraps one command line for the platform shell.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}

// Collect copies every file under dir into outDir, writes checksums and
// returns the upload order. It is used when a workflow built the assets on
// other runners and handed them over as a directory.
func Collect(dir, outDir string) ([]string, error) {
	if err := os.RemoveAll(outDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if seen[name] {
			return errors.New(i18n.Tf("release.err.assets_duplicate", name))
		}
		seen[name] = true
		names = append(names, name)
		return copyFile(path, filepath.Join(outDir, name))
	})
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New(i18n.Tf("release.err.assets_no_match", dir))
	}
	if err := writeChecksums(outDir, names); err != nil {
		return nil, err
	}
	return listFiles(outDir)
}

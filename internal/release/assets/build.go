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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// archiveTime makes archives deterministic regardless of build time.
var archiveTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// Build cross-compiles every target, writes one archive per target and a
// checksums file into outDir, and returns the absolute paths in upload order.
func (p *Plan) Build(ctx context.Context, run execx.Runner, root, version, outDir string) ([]string, error) {
	envRunner, ok := run.(execx.EnvRunner)
	if !ok {
		return nil, errors.New(i18n.T("release.err.assets_runner"))
	}
	if err := os.RemoveAll(outDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(outDir, "build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	plain, _ := VersionParts(version)
	ldflags := strings.Join(expand(p.Ldflags, version), " ")
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
		if err := writeArchive(outDir, p.Name, target, plain, built); err != nil {
			return nil, fmt.Errorf("archive %s: %w", target, err)
		}
	}
	if err := writeChecksums(outDir); err != nil {
		return nil, err
	}
	return listFiles(outDir)
}

func writeArchive(outDir, name string, target Target, version, built string) error {
	base := fmt.Sprintf("%s_%s_%s_%s", name, version, target.OS, target.Arch)
	entry := filepath.Base(built)
	if target.OS == "windows" {
		return writeZip(filepath.Join(outDir, base+".zip"), entry, built)
	}
	return writeTarGz(filepath.Join(outDir, base+".tar.gz"), entry, built)
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

func writeChecksums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
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

// listFiles returns the upload order: archives first, checksums last.
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

package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// CommitOptions captures the inputs needed to write one change record.
type CommitOptions struct {
	Message  string
	Type     string
	Scope    string
	Body     string
	Packages map[string]semver.Bump
	NoCommit bool
}

// stripConventional drops the conventional commit prefix from a message.
func stripConventional(message string) string {
	_, _, description, ok := changes.SplitConventional(message)
	if !ok {
		return message
	}
	return description
}

func renderCommitMessage(opts CommitOptions) string {
	scope := ""
	if opts.Scope != "" {
		scope = "(" + opts.Scope + ")"
	}
	return opts.Type + scope + ": " + strings.TrimSpace(opts.Body)
}

// short returns the abbreviated commit sha shown to the user.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Commit writes a change record and optionally commits it.
func (a *App) Commit(ctx context.Context, opts CommitOptions) (string, error) {
	if err := a.fillFromMessage(&opts); err != nil {
		return "", err
	}
	if len(opts.Packages) == 0 {
		return "", errors.New(i18n.T("commit.err.no_bumps"))
	}
	record := &changes.Record{
		Type:     opts.Type,
		Scope:    opts.Scope,
		Packages: opts.Packages,
		Release:  true,
		Body:     strings.TrimSpace(opts.Body),
	}
	if err := record.Validate(a.Cfg.PackageNames()); err != nil {
		return "", err
	}
	slug := changes.Slugify(opts.Body)
	if opts.Message != "" {
		slug = changes.Slugify(stripConventional(opts.Message))
	}
	path := changes.FileIn(filepath.Join(a.Root, a.Cfg.Changes.Dir), slug)
	if fsx.Exists(path) {
		return "", fmt.Errorf(i18n.T("commit.err.exists"), path)
	}
	content, err := changes.Render(record)
	if err != nil {
		return "", err
	}
	if err := fsx.WriteFileAtomic(path, content, 0o644); err != nil {
		return "", err
	}
	a.UI.Success("%s", i18n.Tf("commit.written", path))
	if opts.NoCommit {
		return path, nil
	}
	if err := a.Git.Add(ctx, path); err != nil {
		return "", err
	}
	message := opts.Message
	if message == "" {
		message = renderCommitMessage(opts)
	}
	sha, err := a.Git.Commit(ctx, message)
	if err != nil {
		return "", err
	}
	a.UI.Success("%s", i18n.Tf("commit.committed", short(sha)))
	return path, nil
}

func (a *App) fillFromMessage(opts *CommitOptions) error {
	if opts.Message == "" {
		if opts.Type == "" {
			return errors.New(i18n.T("commit.err.no_type"))
		}
		if opts.Body == "" {
			opts.Body = opts.Type
		}
		return nil
	}
	typ, scope, description, ok := changes.SplitConventional(opts.Message)
	if !ok {
		if opts.Type == "" {
			return errors.New(i18n.T("commit.err.conventional"))
		}
		if opts.Body == "" {
			opts.Body = opts.Message
		}
		return nil
	}
	if opts.Type == "" {
		opts.Type = typ
	}
	if opts.Scope == "" && scope != "" {
		opts.Scope = scope
	}
	if opts.Body == "" {
		opts.Body = description
	}
	return nil
}

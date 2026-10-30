package scaffold

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Options describes what init should produce.
type Options struct {
	Root            string
	BaseBranch      string
	Repo            string
	ReleaseBranch   string
	Mode            string
	TagPrefix       string
	Remote          string
	Registries      []string
	ChangesDir      string
	Scope           []string
	VerifyCommands  []string
	Ecosystem       string
	VersionScope    string
	Environment     string
	QeggVersion     string
	WithHook        bool
	WithWorkflows   bool
	WithStatus      bool
	AddPackages     bool
	Force           bool
	Packages        map[string]config.Package
	Existing        *config.Config
	ExistingContent []byte
}

// Action names one file outcome.
type Action string

// File actions.
const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
)

// File is one planned file write.
type File struct {
	Path    string
	Action  Action
	Content []byte
	Perm    fs.FileMode
	Reason  string
}

// Plan is the full set of side effects of init.
type Plan struct {
	Root       string
	Files      []*File
	Dirs       []string
	Added      []string
	Kept       []string
	Warnings   []string
	ConfigPath string
}

// Build computes the init plan without touching the filesystem. Files backed
// by an asset are rendered here, before anything is written, so a broken
// template aborts init instead of leaving a half scaffolded repository.
func Build(opts Options) (*Plan, error) {
	plan := &Plan{Root: opts.Root, ConfigPath: filepath.Join(opts.Root, config.FileName)}
	plan.Dirs = append(plan.Dirs, filepath.Join(opts.Root, opts.ChangesDir))
	if opts.WithWorkflows {
		plan.Dirs = append(plan.Dirs, filepath.Join(opts.Root, ".github", "workflows"))
	}

	packages := map[string]config.Package{}
	for name, pkg := range opts.Packages {
		packages[name] = pkg
		if opts.Existing != nil {
			if _, ok := opts.Existing.Packages[name]; ok {
				plan.Kept = append(plan.Kept, name)
			} else {
				plan.Added = append(plan.Added, name)
			}
		}
	}
	sort.Strings(plan.Added)
	sort.Strings(plan.Kept)

	configContent := renderConfig(opts, packages)
	action := ActionCreate
	reason := "new configuration"
	if len(opts.ExistingContent) > 0 {
		merged := MergeProjectRepo(MergePackages(opts.ExistingContent, opts, packages), opts.Repo)
		configContent = merged
		if bytes.Equal(opts.ExistingContent, merged) {
			action = ActionSkip
			reason = "unchanged"
		} else {
			action = ActionUpdate
			reason = "add packages"
		}
	}
	if action == ActionSkip && !opts.Force {
		plan.Files = append(plan.Files, &File{Path: plan.ConfigPath, Action: ActionSkip, Reason: reason})
	} else {
		plan.Files = append(plan.Files, &File{Path: plan.ConfigPath, Action: action, Content: configContent, Perm: 0o644, Reason: reason})
	}

	plan.Files = append(plan.Files, planFile(opts, filepath.Join(opts.Root, opts.ChangesDir, "README.md"), []byte(changesReadme()), 0o644, "change record guide"))

	data := newView(opts)
	for _, item := range artifacts {
		if item.Enabled != nil && !item.Enabled(opts) {
			continue
		}
		dest, err := assets.eval(item.ID+" destination", item.Dest, data)
		if err != nil {
			return nil, err
		}
		content, err := assets.render(item.Source, data)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(opts.Root, filepath.FromSlash(dest))
		plan.Files = append(plan.Files, planFile(opts, path, content, item.Perm, item.Reason))
	}

	// Generated files are always written with LF, even when the embedded
	// templates were checked out with CRLF on Windows. A hook shebang or a
	// workflow parsed with CRLF would be invalid.
	for _, file := range plan.Files {
		if len(file.Content) > 0 {
			file.Content = normalizeLF(file.Content)
		}
	}

	gitignore, err := planGitignore(opts)
	if err != nil {
		return nil, err
	}
	plan.Files = append(plan.Files, gitignore)

	if opts.Existing == nil {
		plan.Warnings = append(plan.Warnings, i18n.T("init.warn_new_config"))
	}
	if opts.QeggVersion == "latest" {
		plan.Warnings = append(plan.Warnings, i18n.T("init.warn_latest_qegg"))
	}
	return plan, nil
}

// normalizeLF converts CRLF line endings to LF.
func normalizeLF(content []byte) []byte {
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}

func planFile(opts Options, path string, content []byte, perm fs.FileMode, reason string) *File {
	action := ActionCreate
	if fsx.Exists(path) {
		action = ActionSkip
		if opts.Force {
			action = ActionUpdate
		}
	}
	return &File{Path: path, Action: action, Content: content, Perm: perm, Reason: reason}
}

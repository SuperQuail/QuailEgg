package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/buildinfo"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release"
)

// App bundles the repository context shared by commands.
type App struct {
	Root       string
	ConfigPath string
	Cfg        *config.Config
	Run        execx.Runner
	Git        *git.Repo
	UI         *ui.Printer
	Now        func() time.Time
	HTTP       *http.Client
}

// New loads the repository and its configuration. A non-empty configPath
// overrides the auto-discovered .quailegg.toml.
func New(cwd, configPath string, printer *ui.Printer) (*App, error) {
	root, err := fsx.RepoRoot(cwd)
	if err != nil {
		return nil, err
	}
	run := execx.OSRunner{}
	repo, err := git.Open(context.Background(), root, run)
	if err != nil {
		return nil, err
	}
	var cfg *config.Config
	var cfgPath string
	if configPath != "" {
		cfgPath, err = filepath.Abs(configPath)
		if err != nil {
			return nil, err
		}
		cfg, err = config.LoadFile(cfgPath)
		if err != nil {
			return nil, err
		}
	} else {
		cfg, cfgPath, err = config.Load(root)
		if err != nil {
			if errors.Is(err, config.ErrNotFound) {
				return nil, errors.New(i18n.T("error.no_config"))
			}
			return nil, err
		}
	}
	return &App{
		Root:       root,
		ConfigPath: cfgPath,
		Cfg:        cfg,
		Run:        run,
		Git:        repo,
		UI:         printer,
		Now:        time.Now,
		HTTP:       &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (a *App) deps(opts release.Options) *release.Deps {
	return &release.Deps{
		Root:    a.Root,
		Cfg:     a.Cfg,
		Git:     a.Git,
		Run:     a.Run,
		UI:      a.UI,
		Now:     a.Now,
		HTTP:    a.HTTP,
		Options: opts,

		QeggVersion: buildinfo.Version(),
		QeggCommit:  buildinfo.Commit(),
		QeggDevelop: a.Cfg.Develop,
	}
}

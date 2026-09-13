package archive

import (
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/manifest"
	"github.com/hamsa/arca/internal/walk"
)

// PlanSource is what one configured source contributes.
type PlanSource struct {
	Group  string
	Path   string
	Member string
	Stats  manifest.Stats
}

// PlanResult is the dry run: what a backup would capture, without writing
// anything. It doubles as the counting pass that gives the progress display a
// total to work against.
type PlanResult struct {
	Sources  []PlanSource
	Stats    manifest.Stats
	Warnings []string
}

// Plan walks the sources reading only metadata. No file content is read, so it
// is cheap even on large trees.
func Plan(cfg *config.Config, skip map[string]bool) (*PlanResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sources, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}

	res := &PlanResult{Sources: make([]PlanSource, len(sources))}
	for i, s := range sources {
		res.Sources[i] = PlanSource{Group: s.Group, Path: s.Path, Member: s.Member}
	}

	walkRes, err := walk.Walk(sources, walk.Options{
		FollowSymlinks: cfg.Settings.FollowSymlinks,
		OneFilesystem:  cfg.Settings.OneFilesystem,
		Skip:           skip,
	}, func(e walk.Entry) error {
		if e.Source < 0 || e.Source >= len(res.Sources) {
			return nil // a synthetic parent directory belongs to no source
		}
		countEntry(&res.Sources[e.Source].Stats, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	res.Stats = manifest.StatsFrom(walkRes)
	res.Warnings = formatWarnings(walkRes.Warnings)
	return res, nil
}

func countEntry(s *manifest.Stats, e walk.Entry) {
	switch e.Kind {
	case walk.KindDir:
		s.Dirs++
	case walk.KindRegular:
		s.Files++
		s.Bytes += e.Info.Size()
	case walk.KindSymlink:
		s.Symlinks++
	case walk.KindHardlink:
		s.Hardlinks++
	}
}

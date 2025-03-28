package run

import (
	"net/url"
	"os"

	"github.com/gruntwork-io/terragrunt/internal/errors"
	"github.com/gruntwork-io/terragrunt/pkg/log"
	"github.com/gruntwork-io/terragrunt/util"
	"github.com/hashicorp/go-getter"
)

// SourceManifestName is the manifest for files copied from the URL specified in the terraform { source = "<URL>" } config
const SourceManifestName = ".terragrunt-source-manifest"

// FileCopyGetter is a custom getter.Getter implementation that uses file copying instead of symlinks. Symlinks are
// faster and use less disk space, but they cause issues in Windows and with infinite loops, so we copy files/folders
// instead.
type FileCopyGetter struct {
	Logger log.Logger
	getter.FileGetter

	// List of glob paths that should be included in the copy. This can be used to override the default behavior of
	// Terragrunt, which will skip hidden folders.
	IncludeInCopy   []string
	ExcludeFromCopy []string
}

// Get replaces the original FileGetter
// The original FileGetter does NOT know how to do folder copying (it only does symlinks), so we provide a copy
// implementation here
func (g *FileCopyGetter) Get(dst string, u *url.URL) error {
	path := u.Path
	if u.RawPath != "" {
		path = u.RawPath
	}

	// The source path must exist and be a directory to be usable.
	if fi, err := os.Stat(path); err != nil {
		return errors.Errorf("source path error: %s", err)
	} else if !fi.IsDir() {
		return errors.Errorf("source path must be a directory")
	}

	return util.CopyFolderContents(g.Logger, path, dst, SourceManifestName, g.IncludeInCopy, g.ExcludeFromCopy)
}

// GetFile The original FileGetter already knows how to do file copying so long as we set the Copy flag to true, so just
// delegate to it
func (g *FileCopyGetter) GetFile(dst string, u *url.URL) error {
	underlying := &getter.FileGetter{Copy: true}
	if err := underlying.GetFile(dst, u); err != nil {
		return errors.Errorf("failed to copy file to %s: %w", dst, err)
	}

	return nil
}

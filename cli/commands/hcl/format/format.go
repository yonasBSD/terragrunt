// Package format recursively looks for hcl files in the directory tree starting at workingDir, and formats them
// based on the language style guides provided by Hashicorp. This is done using the official hcl2 library.
package format

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gruntwork-io/terragrunt/config"
	"github.com/gruntwork-io/terragrunt/pkg/log"
	"github.com/gruntwork-io/terragrunt/pkg/log/writer"
	"golang.org/x/exp/slices"

	"github.com/gruntwork-io/terragrunt/internal/errors"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/mattn/go-zglob"

	"github.com/gruntwork-io/terragrunt/config/hclparse"
	"github.com/gruntwork-io/terragrunt/options"
	"github.com/gruntwork-io/terragrunt/util"
)

var excludePaths = []string{
	util.TerragruntCacheDir,
	util.DefaultBoilerplateDir,
	config.StackDir,
}

func Run(ctx context.Context, l log.Logger, opts *options.TerragruntOptions) error {
	workingDir := opts.WorkingDir
	targetFile := opts.HclFile
	stdIn := opts.HclFromStdin

	if stdIn {
		if targetFile != "" {
			return errors.Errorf("both stdin and path flags are specified")
		}

		return formatFromStdin(l, opts)
	}

	// handle when option specifies a particular file
	if targetFile != "" {
		if !filepath.IsAbs(targetFile) {
			targetFile = util.JoinPath(workingDir, targetFile)
		}

		l.Debugf("Formatting hcl file at: %s.", targetFile)

		return formatTgHCL(l, opts, targetFile)
	}

	l.Debugf("Formatting hcl files from the directory tree %s.", opts.WorkingDir)
	// zglob normalizes paths to "/"
	tgHclFiles, err := zglob.Glob(util.JoinPath(workingDir, "**", "*.hcl"))
	if err != nil {
		return err
	}

	filteredTgHclFiles := []string{}

	for _, fname := range tgHclFiles {
		skipFile := false
		// Ignore any files that are in the cache or scaffold dir
		pathList := strings.Split(fname, "/")

		for _, excludePath := range excludePaths {
			if slices.Contains(pathList, excludePath) {
				skipFile = true
				break
			}
		}

		for _, excludeDir := range opts.HclExclude {
			if slices.Contains(pathList, excludeDir) {
				skipFile = true
				break
			}
		}

		if skipFile {
			l.Debugf("%s was ignored", fname)
		} else {
			filteredTgHclFiles = append(filteredTgHclFiles, fname)
		}
	}

	l.Debugf("Found %d hcl files", len(filteredTgHclFiles))

	var formatErrors *errors.MultiError

	for _, tgHclFile := range filteredTgHclFiles {
		err := formatTgHCL(l, opts, tgHclFile)
		if err != nil {
			formatErrors = formatErrors.Append(err)
		}
	}

	return formatErrors.ErrorOrNil()
}

func formatFromStdin(l log.Logger, opts *options.TerragruntOptions) error {
	contents, err := io.ReadAll(os.Stdin)

	if err != nil {
		l.Errorf("Error reading from stdin: %s", err)

		return fmt.Errorf("error reading from stdin: %w", err)
	}

	if err = checkErrors(l, l.Formatter().DisabledColors(), contents, "stdin"); err != nil {
		l.Errorf("Error parsing hcl from stdin")

		return fmt.Errorf("error parsing hcl from stdin: %w", err)
	}

	newContents := hclwrite.Format(contents)

	buf := bufio.NewWriter(opts.Writer)

	if _, err = buf.Write(newContents); err != nil {
		l.Errorf("Failed to write to stdout")

		return fmt.Errorf("failed to write to stdout: %w", err)
	}

	if err = buf.Flush(); err != nil {
		l.Errorf("Failed to flush to stdout")

		return fmt.Errorf("failed to flush to stdout: %w", err)
	}

	return nil
}

// formatTgHCL uses the hcl2 library to format the hcl file. This will attempt to parse the HCL file first to
// ensure that there are no syntax errors, before attempting to format it.
func formatTgHCL(l log.Logger, opts *options.TerragruntOptions, tgHclFile string) error {
	l.Debugf("Formatting %s", tgHclFile)

	info, err := os.Stat(tgHclFile)
	if err != nil {
		l.Errorf("Error retrieving file info of %s", tgHclFile)
		return errors.Errorf("failed to get file info for %s: %v", tgHclFile, err)
	}

	contentsStr, err := util.ReadFileAsString(tgHclFile)
	if err != nil {
		l.Errorf("Error reading %s", tgHclFile)
		return err
	}

	contents := []byte(contentsStr)

	err = checkErrors(l, l.Formatter().DisabledColors(), contents, tgHclFile)
	if err != nil {
		l.Errorf("Error parsing %s", tgHclFile)
		return err
	}

	newContents := hclwrite.Format(contents)

	fileUpdated := !bytes.Equal(newContents, contents)

	if opts.Diff && fileUpdated {
		diff, err := bytesDiff(l, contents, newContents, tgHclFile)
		if err != nil {
			l.Errorf("Failed to generate diff for %s", tgHclFile)
			return err
		}

		_, err = fmt.Fprintf(opts.Writer, "%s\n", diff)
		if err != nil {
			l.Errorf("Failed to print diff for %s", tgHclFile)
			return err
		}
	}

	if opts.Check && fileUpdated {
		return fmt.Errorf("invalid file format %s", tgHclFile)
	}

	if fileUpdated {
		l.Infof("%s was updated", tgHclFile)
		return os.WriteFile(tgHclFile, newContents, info.Mode())
	}

	return nil
}

// checkErrors takes in the contents of a hcl file and looks for syntax errors.
func checkErrors(logger log.Logger, disableColor bool, contents []byte, tgHclFile string) error {
	parser := hclparse.NewParser()
	_, diags := parser.ParseHCL(contents, tgHclFile)

	writer := writer.New(writer.WithLogger(logger), writer.WithDefaultLevel(log.ErrorLevel))
	diagWriter := parser.GetDiagnosticsWriter(writer, disableColor)

	err := diagWriter.WriteDiagnostics(diags)
	if err != nil {
		return errors.New(err)
	}

	if diags.HasErrors() {
		return diags
	}

	return nil
}

// bytesDiff uses GNU diff to display the differences between the contents of HCL file before and after formatting
func bytesDiff(l log.Logger, b1, b2 []byte, path string) ([]byte, error) {
	f1, err := os.CreateTemp("", "")
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := f1.Close(); err != nil {
			l.Warnf("Failed to close file %s %v", f1.Name(), err)
		}

		if err := os.Remove(f1.Name()); err != nil {
			l.Warnf("Failed to remove file %s %v", f1.Name(), err)
		}
	}()

	f2, err := os.CreateTemp("", "")
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := f2.Close(); err != nil {
			l.Warnf("Failed to close file %s %v", f2.Name(), err)
		}

		if err := os.Remove(f2.Name()); err != nil {
			l.Warnf("Failed to remove file %s %v", f2.Name(), err)
		}
	}()

	if _, err := f1.Write(b1); err != nil {
		return nil, err
	}

	if _, err := f2.Write(b2); err != nil {
		return nil, err
	}

	data, err := exec.Command("diff", "--label="+filepath.Join("old", path), "--label="+filepath.Join("new/", path), "-u", f1.Name(), f2.Name()).CombinedOutput()
	if len(data) > 0 {
		// diff exits with a non-zero status when the files don't match.
		// Ignore that failure as long as we get output.
		err = nil
	}

	return data, err
}

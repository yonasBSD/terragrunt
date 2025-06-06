//go:build linux || darwin
// +build linux darwin

package shell_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/gruntwork-io/terragrunt/shell"
	"github.com/gruntwork-io/terragrunt/test/helpers/logger"
	"github.com/gruntwork-io/terragrunt/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gruntwork-io/terragrunt/options"
)

func TestCommandOutputOrder(t *testing.T) {
	t.Parallel()

	t.Run("withPtty", func(t *testing.T) {
		t.Parallel()
		testCommandOutputOrder(t, true,
			[]string{"stdout1", "stderr1", "stdout2", "stderr2", "stderr3"},
			[]string{"stdout1", "stdout2"},
			[]string{"stderr1", "stderr2", "stderr3"},
		)
	})
	t.Run("withoutPtty", func(t *testing.T) {
		t.Parallel()
		testCommandOutputOrder(t, false,
			[]string{"stderr1", "stderr2", "stderr3"},
			[]string{"stdout1", "stdout2"},
			[]string{"stderr1", "stderr2", "stderr3"},
		)
	})
}

func noop[T any](t T) {}

func testCommandOutputOrder(t *testing.T, withPtty bool, fullOutput []string, stdout []string, stderr []string) {
	t.Helper()

	testCommandOutput(t, noop[*options.TerragruntOptions], assertOutputs(t, fullOutput, stdout, stderr), withPtty)
}

func testCommandOutput(t *testing.T, withOptions func(*options.TerragruntOptions), assertResults func(string, *util.CmdOutput), allocateStdout bool) {
	t.Helper()

	terragruntOptions, err := options.NewTerragruntOptionsForTest("")
	require.NoError(t, err, "Unexpected error creating NewTerragruntOptionsForTest: %v", err)

	// Specify a single (locking) buffer for both as a way to check that the output is being written in the correct
	// order
	var allOutputBuffer BufferWithLocking
	terragruntOptions.Writer = &allOutputBuffer
	terragruntOptions.ErrWriter = &allOutputBuffer

	terragruntOptions.TerraformCliArgs = append(terragruntOptions.TerraformCliArgs, "same")

	withOptions(terragruntOptions)

	l := logger.CreateLogger()

	out, err := shell.RunCommandWithOutput(t.Context(), l, terragruntOptions, "", !allocateStdout, false, "testdata/test_outputs.sh", "same")

	assert.NotNil(t, out, "Should get output")
	require.NoError(t, err, "Should have no error")

	assert.NotNil(t, out, "Should get output")
	assertResults(allOutputBuffer.String(), out)
}

func assertOutputs(
	t *testing.T,
	expectedAllOutputs []string,
	expectedStdOutputs []string,
	expectedStdErrs []string,
) func(string, *util.CmdOutput) {
	t.Helper()

	return func(allOutput string, out *util.CmdOutput) {
		allOutputs := strings.Split(strings.TrimSpace(allOutput), "\n")
		assert.Len(t, allOutputs, len(expectedAllOutputs))
		for i := range allOutputs {
			assert.Contains(t, allOutputs[i], expectedAllOutputs[i], allOutputs[i])
		}

		stdOutputs := strings.Split(strings.TrimSpace(out.Stdout.String()), "\n")
		assert.Equal(t, expectedStdOutputs, stdOutputs)

		stdErrs := strings.Split(strings.TrimSpace(out.Stderr.String()), "\n")
		assert.Equal(t, expectedStdErrs, stdErrs)
	}
}

// A goroutine-safe bytes.Buffer
type BufferWithLocking struct {
	buffer bytes.Buffer
	mutex  sync.Mutex
}

// Write appends the contents of p to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
func (s *BufferWithLocking) Write(p []byte) (n int, err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.Write(p)
}

// String returns the contents of the unread portion of the buffer
// as a string.  If the Buffer is a nil pointer, it returns "<nil>".
func (s *BufferWithLocking) String() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.String()
}

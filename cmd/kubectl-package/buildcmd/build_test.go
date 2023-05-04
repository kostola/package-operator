package buildcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"
)

func TestBuildOutput(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "pko-*.tar.gz")
	require.Nil(t, err)

	defer func() { require.Nil(t, os.Remove(f.Name())) }()
	defer func() { require.Nil(t, f.Close()) }()

	wd, err := os.Getwd()
	require.Nil(t, err)
	packagePath := filepath.Join(wd, "testdata")

	cmd := NewCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{packagePath, "--tag", "chicken:oldest", "--output", f.Name()})

	require.Nil(t, cmd.Execute())
	require.Len(t, stdout.String(), 0)
	require.Len(t, stderr.String(), 0)

	i, err := tarball.ImageFromPath(f.Name(), nil)
	require.Nil(t, err)
	_, err = i.Manifest()
	require.Nil(t, err)
}

func TestBuildEmptySource(t *testing.T) {
	t.Parallel()
	cmd := NewCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{""})

	require.NotNil(t, cmd.Execute())
}

func TestBuildNoSource(t *testing.T) {
	t.Parallel()
	cmd := NewCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	require.NotNil(t, cmd.Execute())
}

func TestBuildPushWOTags(t *testing.T) {
	t.Parallel()
	cmd := NewCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{".", "--push"})

	require.NotNil(t, cmd.Execute())
}

func TestBuildOutputWOTags(t *testing.T) {
	t.Parallel()
	cmd := NewCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{".", "--output /tmp/yes"})

	require.NotNil(t, cmd.Execute())
}

func TestBuildInvalidTag(t *testing.T) {
	t.Parallel()
	cmd := NewCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{".", "--tag", "bread:a:b"})

	require.NotNil(t, cmd.Execute())
}
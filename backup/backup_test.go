/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package backup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoop_AllOps(t *testing.T) {
	n := &Noop{}
	ctx := context.Background()

	assert.NoError(t, n.Init(ctx))
	assert.NoError(t, n.Pull(ctx))
	assert.NoError(t, n.Push(ctx))
	assert.NoError(t, n.Close())
}

func TestNew_Noop(t *testing.T) {
	p := New("none", "/tmp", "testuser", nil)
	_, ok := p.(*Noop)
	assert.True(t, ok)
}

func TestNew_Unknown(t *testing.T) {
	p := New("unknown", "/tmp", "testuser", nil)
	_, ok := p.(*Noop)
	assert.True(t, ok)
}

func TestNew_GitHub(t *testing.T) {
	p := New("github", "/tmp", "testuser", map[string]string{
		"git_base_url": "git@github.com:org",
	})
	_, ok := p.(*GitHub)
	assert.True(t, ok)
}

func TestNew_S3(t *testing.T) {
	p := New("s3", "/tmp", "testuser", map[string]string{
		"s3_endpoint": "http://localhost:9000",
		"s3_region":   "us-east-1",
	})
	_, ok := p.(*S3)
	assert.True(t, ok)
}

func TestGitHub_InitCreatesRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	storagePath := filepath.Join(dir, "storage")

	remote := filepath.Join(dir, "remote.git")
	cmd := exec.Command("git", "init", "--bare", remote)
	require.NoError(t, cmd.Run())

	g := NewGitHub(storagePath, "testuser", remote)
	g.repoPath = filepath.Join(storagePath, "testuser_smriti")

	ctx := context.Background()

	cmd = exec.Command("git", "clone", remote, g.repoPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	_, err = os.Stat(filepath.Join(g.repoPath, ".git"))
	assert.NoError(t, err)

	assert.NoError(t, g.Init(ctx))
}

func TestGitHub_PushAndPull(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()

	remote := filepath.Join(dir, "remote.git")
	cmd := exec.Command("git", "init", "--bare", remote)
	require.NoError(t, cmd.Run())

	repoPath := filepath.Join(dir, "repo")
	cmd = exec.Command("git", "clone", remote, repoPath)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "config", "user.name", "test")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	g := &GitHub{
		storagePath: dir,
		user:        "testuser",
		gitBaseURL:  remote,
		repoPath:    repoPath,
	}

	ctx := context.Background()

	testFile := filepath.Join(repoPath, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello"), 0o644))

	err := g.Push(ctx)
	assert.NoError(t, err)

	assert.NoError(t, g.Pull(ctx))
	assert.NoError(t, g.Close())
}

func TestS3_NewWithDefaults(t *testing.T) {
	s := NewS3("/tmp/storage", "alice", map[string]string{})
	assert.Equal(t, "alice_smriti", s.bucket)
	assert.Equal(t, "/tmp/storage", s.storagePath)
}

func TestS3_NewWithCustomBucket(t *testing.T) {
	s := NewS3("/tmp/storage", "alice", map[string]string{
		"s3_bucket": "custom-bucket",
	})
	assert.Equal(t, "custom-bucket", s.bucket)
}

func TestS3_Close(t *testing.T) {
	s := NewS3("/tmp", "test", map[string]string{})
	assert.NoError(t, s.Close())
}

func TestFileMD5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o644))

	hash, err := fileMD5(path)
	require.NoError(t, err)
	assert.Len(t, hash, 32)

	hash2, err := fileMD5(path)
	require.NoError(t, err)
	assert.Equal(t, hash, hash2, "same file should produce same hash")
}

func TestFileMD5_NotFound(t *testing.T) {
	_, err := fileMD5("/nonexistent/file.txt")
	assert.Error(t, err)
}

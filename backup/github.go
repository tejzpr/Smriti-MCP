// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type GitHub struct {
	storagePath string
	user        string
	gitBaseURL  string
	repoPath    string
}

func NewGitHub(storagePath, user, gitBaseURL string) *GitHub {
	repoName := user + "_smriti"
	return &GitHub{
		storagePath: storagePath,
		user:        user,
		gitBaseURL:  gitBaseURL,
		repoPath:    filepath.Join(storagePath, repoName),
	}
}

func (g *GitHub) Init(ctx context.Context) error {
	repoURL := fmt.Sprintf("%s/%s_smriti.git", g.gitBaseURL, g.user)

	if _, err := os.Stat(filepath.Join(g.repoPath, ".git")); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(g.repoPath), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, g.repoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		if err := os.MkdirAll(g.repoPath, 0o755); err != nil {
			return fmt.Errorf("create repo dir: %w", err)
		}
		initCmd := exec.CommandContext(ctx, "git", "init")
		initCmd.Dir = g.repoPath
		if out, err := initCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git init: %s: %w", string(out), err)
		}
		remoteCmd := exec.CommandContext(ctx, "git", "remote", "add", "origin", repoURL)
		remoteCmd.Dir = g.repoPath
		remoteCmd.CombinedOutput()
	} else {
		_ = out
	}
	return nil
}

func (g *GitHub) Pull(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.repoPath, ".git")); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "pull", "--rebase", "origin", "main")
	cmd.Dir = g.repoPath
	cmd.CombinedOutput()
	return nil
}

func (g *GitHub) Push(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.repoPath, ".git")); err != nil {
		return fmt.Errorf("repo not initialized: %w", err)
	}

	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = g.repoPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", string(out), err)
	}

	ts := time.Now().Format(time.RFC3339)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", fmt.Sprintf("smriti sync %s", ts), "--allow-empty")
	commitCmd.Dir = g.repoPath
	commitCmd.CombinedOutput()

	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = g.repoPath
	if out, err := pushCmd.CombinedOutput(); err != nil {
		pushCmd2 := exec.CommandContext(ctx, "git", "push", "-u", "origin", "main")
		pushCmd2.Dir = g.repoPath
		if out2, err2 := pushCmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("git push: %s / %s: %w", string(out), string(out2), err2)
		}
	}
	return nil
}

func (g *GitHub) Close() error {
	return nil
}

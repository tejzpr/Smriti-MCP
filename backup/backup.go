/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package backup

import "context"

type Provider interface {
	Init(ctx context.Context) error
	Pull(ctx context.Context) error
	Push(ctx context.Context) error
	Close() error
}

func New(backupType, storagePath, user string, opts map[string]string) Provider {
	switch backupType {
	case "github":
		gitBaseURL := opts["git_base_url"]
		return NewGitHub(storagePath, user, gitBaseURL)
	case "s3":
		return NewS3(storagePath, user, opts)
	default:
		return &Noop{}
	}
}

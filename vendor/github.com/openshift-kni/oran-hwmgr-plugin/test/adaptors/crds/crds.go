/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crds

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/sethvargo/go-retry"
)

const (
	ImsRepoName   = "oran-o2ims"
	ImsRepoPath   = "github.com/openshift-kni"
	ImsHwrMgtPath = "api/hardwaremanagement"
	retries       = uint64(4)
)

// GetRequiredCRDsFromGit clones an oran-o2ims repo in a specific path and checks out a specific commit
// to fetch required CRDs from there
func GetRequiredCRDsFromGit(crdRepo, commit, crdPath string) error {

	var r *git.Repository
	cloneFn := func(ctx context.Context) error {

		// clone the repo
		var err error
		r, err = git.PlainClone(crdPath, false, &git.CloneOptions{
			URL:      crdRepo,
			Progress: os.Stdout,
		})

		if err != nil {
			return retry.RetryableError(fmt.Errorf("failed to clone repo:%s, commit:%s, path:%s, err:%w",
				crdRepo, commit, crdPath, err))
		}
		return nil
	}

	// configure the retries
	ctx := context.Background()
	backoff := retry.NewFibonacci(1 * time.Second)
	err := retry.Do(ctx, retry.WithMaxRetries(retries, backoff), cloneFn)
	if err != nil {
		return fmt.Errorf("failed to clone repo:%s, err:%w", crdRepo, err)
	}

	// checkout the specific commit
	w, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get git worktree repo:%s, err:%w", crdRepo, err)
	}

	commitLong, err := r.ResolveRevision(plumbing.Revision(commit))
	if err != nil {
		return fmt.Errorf("failed to get long format for commit:%s repo:%s, err:%w",
			commit, crdRepo, err)
	}

	err = w.Checkout(&git.CheckoutOptions{
		Hash: *commitLong,
	})
	if err != nil {
		return fmt.Errorf("failed to checkout commit:%s repo:%s, err:%w",
			commitLong.String(), crdRepo, err)
	}
	return nil
}

// Copyright 2020 OpenSSF Scorecard Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package checks

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/ossf/scorecard/v5/checker"
	"github.com/ossf/scorecard/v5/clients"
	mockrepo "github.com/ossf/scorecard/v5/clients/mockclients"
	sce "github.com/ossf/scorecard/v5/errors"
	scut "github.com/ossf/scorecard/v5/utests"
)

func getBranchName(branch *clients.BranchRef) string {
	if branch == nil || branch.Name == nil {
		return ""
	}
	return *branch.Name
}

func getBranch(branches []*clients.BranchRef, name string, isNonAdmin bool) *clients.BranchRef {
	for _, branch := range branches {
		branchName := getBranchName(branch)
		if branchName == name {
			if !isNonAdmin {
				return branch
			}
			return scrubBranch(branch)
		}
	}
	return nil
}

func scrubBranch(branch *clients.BranchRef) *clients.BranchRef {
	ret := branch
	ret.BranchProtectionRule = clients.BranchProtectionRule{}
	return ret
}

func TestReleaseAndDevBranchProtected(t *testing.T) {
	t.Parallel()

	rel1 := "release/v.1"
	sha := "8fb3cb86082b17144a80402f5367ae65f06083bd"
	main := "main"
	trueVal := true
	falseVal := false
	var zeroVal int32

	var oneVal int32 = 1

	tests := []struct {
		name          string
		defaultBranch string
		branches      []*clients.BranchRef
		releases      []string
		repoFiles     []string
		expected      scut.TestReturn
		nonadmin      bool
	}{
		{
			name: "Nil release and main branch names",
			expected: scut.TestReturn{
				Error:         nil,
				Score:         checker.InconclusiveResultScore,
				NumberOfWarn:  0,
				NumberOfInfo:  0,
				NumberOfDebug: 0,
			},
			defaultBranch: main,
			branches: []*clients.BranchRef{
				{
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &trueVal,
							Contexts:             []string{"foo"},
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &trueVal,
							RequireCodeOwnerReviews:      &trueVal,
							RequiredApprovingReviewCount: &oneVal,
						},
						EnforceAdmins:        &trueVal,
						RequireLinearHistory: &trueVal,
						AllowForcePushes:     &falseVal,
						AllowDeletions:       &falseVal,
					},
				},
				{
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &falseVal,
							Contexts:             nil,
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &falseVal,
							RequireCodeOwnerReviews:      &falseVal,
							RequiredApprovingReviewCount: &zeroVal,
						},
						EnforceAdmins:        &falseVal,
						RequireLinearHistory: &falseVal,
						AllowForcePushes:     &falseVal,
						AllowDeletions:       &falseVal,
					},
				},
				nil,
			},
			releases: []string{},
		},
		{
			name: "Only development branch",
			expected: scut.TestReturn{
				Error:         nil,
				Score:         3,
				NumberOfWarn:  7,
				NumberOfInfo:  3,
				NumberOfDebug: 0,
			},
			defaultBranch: main,
			branches: []*clients.BranchRef{
				{
					Name:      &rel1,
					Protected: &falseVal,
				},
				{
					Name:      &main,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &falseVal,
							Contexts:             nil,
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &falseVal,
							RequireCodeOwnerReviews:      &falseVal,
							RequiredApprovingReviewCount: &zeroVal,
						},
						EnforceAdmins:           &falseVal,
						RequireLinearHistory:    &falseVal,
						RequireLastPushApproval: &falseVal,
						AllowForcePushes:        &falseVal,
						AllowDeletions:          &falseVal,
					},
				},
			},
			releases: nil,
		},
		{
			name: "Take worst of release and development",
			expected: scut.TestReturn{
				Error:         nil,
				Score:         4,
				NumberOfWarn:  9,
				NumberOfInfo:  11,
				NumberOfDebug: 0,
			},
			defaultBranch: main,
			branches: []*clients.BranchRef{
				{
					Name:      &main,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &trueVal,
							Contexts:             []string{"foo"},
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &trueVal,
							RequireCodeOwnerReviews:      &trueVal,
							RequiredApprovingReviewCount: &oneVal,
						},
						EnforceAdmins:           &trueVal,
						RequireLastPushApproval: &trueVal,
						RequireLinearHistory:    &trueVal,
						AllowForcePushes:        &falseVal,
						AllowDeletions:          &falseVal,
					},
				},
				{
					Name:      &rel1,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &falseVal,
							Contexts:             nil,
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &falseVal,
							RequireCodeOwnerReviews:      &falseVal,
							RequiredApprovingReviewCount: &zeroVal,
						},
						EnforceAdmins:           &falseVal,
						RequireLastPushApproval: &falseVal,
						RequireLinearHistory:    &falseVal,
						AllowForcePushes:        &falseVal,
						AllowDeletions:          &falseVal,
					},
				},
			},
			releases: []string{rel1},
		},
		{
			name: "Both release and development are OK",
			expected: scut.TestReturn{
				Error:         nil,
				Score:         8,
				NumberOfWarn:  4,
				NumberOfInfo:  16,
				NumberOfDebug: 0,
			},
			defaultBranch: main,
			branches: []*clients.BranchRef{
				{
					Name:      &main,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &trueVal,
							Contexts:             []string{"foo"},
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &trueVal,
							RequireCodeOwnerReviews:      &trueVal,
							RequiredApprovingReviewCount: &oneVal,
						},
						EnforceAdmins:           &trueVal,
						RequireLastPushApproval: &trueVal,
						RequireLinearHistory:    &trueVal,
						AllowForcePushes:        &falseVal,
						AllowDeletions:          &falseVal,
					},
				},
				{
					Name:      &rel1,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &trueVal,
							Contexts:             []string{"foo"},
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &trueVal,
							RequireCodeOwnerReviews:      &trueVal,
							RequiredApprovingReviewCount: &oneVal,
						},
						EnforceAdmins:           &trueVal,
						RequireLastPushApproval: &trueVal,
						RequireLinearHistory:    &trueVal,
						AllowForcePushes:        &falseVal,
						AllowDeletions:          &falseVal,
					},
				},
			},
			releases: []string{rel1},
		},
		{
			name: "Ignore a non-branch targetcommitish",
			expected: scut.TestReturn{
				Error:         nil,
				Score:         3,
				NumberOfWarn:  7,
				NumberOfInfo:  3,
				NumberOfDebug: 0,
			},
			defaultBranch: main,
			releases:      []string{sha},
			branches: []*clients.BranchRef{
				{
					Name:      &main,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &falseVal,
							Contexts:             nil,
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &falseVal,
							RequireCodeOwnerReviews:      &falseVal,
							RequiredApprovingReviewCount: &zeroVal,
						},
						EnforceAdmins:           &falseVal,
						RequireLastPushApproval: &falseVal,
						RequireLinearHistory:    &falseVal,
						AllowForcePushes:        &falseVal,
						AllowDeletions:          &falseVal,
					},
				}, {
					Name:      &rel1,
					Protected: &falseVal,
				},
			},
		},
		{
			name: "TargetCommittish nil",
			expected: scut.TestReturn{
				Error:         sce.ErrScorecardInternal,
				Score:         checker.InconclusiveResultScore,
				NumberOfWarn:  0,
				NumberOfInfo:  0,
				NumberOfDebug: 0,
			},
			defaultBranch: main,
			releases:      []string{""},
			branches: []*clients.BranchRef{
				{
					Name:      &main,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &falseVal,
							Contexts:             nil,
						},
						PullRequestRule: clients.PullRequestRule{
							Required:                     &trueVal,
							DismissStaleReviews:          &falseVal,
							RequireCodeOwnerReviews:      &falseVal,
							RequiredApprovingReviewCount: &zeroVal,
						},
						EnforceAdmins:        &falseVal,
						RequireLinearHistory: &falseVal,
						AllowForcePushes:     &falseVal,
						AllowDeletions:       &falseVal,
					},
				},
			},
		},
		{
			name: "Non-admin check with protected release and development",
			expected: scut.TestReturn{
				Error:         nil,
				Score:         0,
				NumberOfWarn:  4,
				NumberOfInfo:  0,
				NumberOfDebug: 10,
			},
			nonadmin:      true,
			defaultBranch: main,
			// branches:      []*string{&rel1, &main},
			releases: []string{rel1},
			branches: []*clients.BranchRef{
				{
					Name:      &main,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &trueVal,
							Contexts:             []string{"foo"},
						},
					},
				},
				{
					Name:      &rel1,
					Protected: &trueVal,
					BranchProtectionRule: clients.BranchProtectionRule{
						CheckRules: clients.StatusChecksRule{
							RequiresStatusChecks: &trueVal,
							UpToDateBeforeMerge:  &trueVal,
							Contexts:             []string{"foo"},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockRepoClient := mockrepo.NewMockRepoClient(ctrl)
			mockRepoClient.EXPECT().GetDefaultBranch().
				DoAndReturn(func() (*clients.BranchRef, error) {
					defaultBranch := getBranch(tt.branches, tt.defaultBranch, tt.nonadmin)
					return defaultBranch, nil
				}).AnyTimes()
			mockRepoClient.EXPECT().ListReleases().
				DoAndReturn(func() ([]clients.Release, error) {
					var ret []clients.Release
					for _, rel := range tt.releases {
						ret = append(ret, clients.Release{
							TargetCommitish: rel,
						})
					}
					return ret, nil
				}).AnyTimes()
			mockRepoClient.EXPECT().GetBranch(gomock.Any()).
				DoAndReturn(func(b string) (*clients.BranchRef, error) {
					return getBranch(tt.branches, b, tt.nonadmin), nil
				}).AnyTimes()
			mockRepoClient.EXPECT().ListFiles(gomock.Any()).AnyTimes().Return(tt.repoFiles, nil)
			dl := scut.TestDetailLogger{}
			req := checker.CheckRequest{
				Dlogger:    &dl,
				RepoClient: mockRepoClient,
			}
			r := BranchProtection(&req)
			scut.ValidateTestReturn(t, tt.name, &tt.expected, &r, &dl)
			ctrl.Finish()
		})
	}
}

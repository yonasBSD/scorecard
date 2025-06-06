// Copyright 2024 OpenSSF Scorecard Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package patch

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/rhysd/actionlint"

	"github.com/ossf/scorecard/v5/checker"
	"github.com/ossf/scorecard/v5/checks/fileparser"
	sce "github.com/ossf/scorecard/v5/errors"
)

type unsafePattern struct {
	idRegex      *regexp.Regexp
	replaceRegex *regexp.Regexp
	envvarName   string
}

// Fixes the script injection identified by the finding and returns a unified diff users can apply (with `git apply` or
// `patch`) to fix the workflow themselves. Should an error occur, an empty patch is returned.
func GeneratePatch(
	f checker.File,
	content []byte,
	workflow *actionlint.Workflow,
	workflowErrs []*actionlint.Error,
) (string, error) {
	patchedWorkflow, err := patchWorkflow(f, content, workflow)
	if err != nil {
		return "", err
	}
	errs := validatePatchedWorkflow(patchedWorkflow, workflowErrs)
	if len(errs) > 0 {
		return "", fileparser.FormatActionlintError(errs)
	}
	return getDiff(f.Path, content, patchedWorkflow)
}

// Returns a patched version of the workflow without the script injection finding.
func patchWorkflow(f checker.File, content []byte, workflow *actionlint.Workflow) ([]byte, error) {
	unsafeVar := strings.TrimSpace(f.Snippet)

	lines := bytes.Split(content, []byte("\n"))
	runCmdIndex := int(f.Offset - 1)

	if runCmdIndex < 0 || runCmdIndex >= len(lines) {
		return []byte(""), sce.WithMessage(sce.ErrScorecardInternal, "Invalid dangerous workflow offset")
	}

	unsafePattern, err := getUnsafePattern(unsafeVar)
	if err != nil {
		return []byte(""), err
	}

	existingEnvvars := parseExistingEnvvars(workflow)
	unsafePattern, err = useExistingEnvvars(unsafePattern, existingEnvvars, unsafeVar)
	if err != nil {
		return []byte(""), err
	}

	replaceUnsafeVarWithEnvvar(lines, unsafePattern, runCmdIndex)

	lines, err = addEnvvarToGlobalEnv(lines, existingEnvvars, unsafePattern, unsafeVar)
	if err != nil {
		return []byte(""), sce.WithMessage(sce.ErrScorecardInternal,
			fmt.Sprintf("Unknown dangerous variable: %s", unsafeVar))
	}

	return bytes.Join(lines, []byte("\n")), nil
}

func getUnsafePattern(unsafeVar string) (unsafePattern, error) {
	unsafePatterns := []unsafePattern{
		newUnsafePattern("AUTHOR_EMAIL", `github\.event\.commits.*?\.author\.email`),
		newUnsafePattern("AUTHOR_EMAIL", `github\.event\.head_commit\.author\.email`),
		newUnsafePattern("AUTHOR_NAME", `github\.event\.commits.*?\.author\.name`),
		newUnsafePattern("AUTHOR_NAME", `github\.event\.head_commit\.author\.name`),
		newUnsafePattern("COMMENT_BODY", `github\.event\.comment\.body`),
		newUnsafePattern("COMMIT_MESSAGE", `github\.event\.commits.*?\.message`),
		newUnsafePattern("COMMIT_MESSAGE", `github\.event\.head_commit\.message`),
		newUnsafePattern("DISCUSSION_TITLE", `github\.event\.discussion\.title`),
		newUnsafePattern("DISCUSSION_BODY", `github\.event\.discussion\.body`),
		newUnsafePattern("ISSUE_BODY", `github\.event\.issue\.body`),
		newUnsafePattern("ISSUE_COMMENT_BODY", `github\.event\.issue_comment\.comment\.body`),
		newUnsafePattern("ISSUE_TITLE", `github\.event\.issue\.title`),
		newUnsafePattern("PAGE_NAME", `github\.event\.pages.*?\.page_name`),
		newUnsafePattern("PR_BODY", `github\.event\.pull_request\.body`),
		newUnsafePattern("PR_DEFAULT_BRANCH", `github\.event\.pull_request\.head\.repo\.default_branch`),
		newUnsafePattern("PR_HEAD_LABEL", `github\.event\.pull_request\.head\.label`),
		newUnsafePattern("PR_HEAD_REF", `github\.event\.pull_request\.head\.ref`),
		newUnsafePattern("PR_TITLE", `github\.event\.pull_request\.title`),
		newUnsafePattern("REVIEW_BODY", `github\.event\.review\.body`),
		newUnsafePattern("REVIEW_COMMENT_BODY", `github\.event\.review_comment\.body`),

		newUnsafePattern("HEAD_REF", `github\.head_ref`),
	}

	for _, p := range unsafePatterns {
		if p.idRegex.MatchString(unsafeVar) {
			arrayVarRegex := regexp.MustCompile(`\[(.+?)\]`)
			arrayIdx := arrayVarRegex.FindStringSubmatch(unsafeVar)
			if len(arrayIdx) < 2 {
				// not an array variable, the default envvar name is sufficient.
				return p, nil
			}
			// Array variable (i.e. `github.event.commits[0].message`), must avoid potential conflicts.
			// Add the array index to the name as a suffix, and use the exact unsafe variable name instead of the
			// default, which includes a regex that will catch all instances of the array.
			envvarName := fmt.Sprintf("%s_%s", p.envvarName, arrayIdx[1])
			return newUnsafePattern(envvarName, regexp.QuoteMeta(unsafeVar)), nil
		}
	}

	return unsafePattern{}, sce.WithMessage(sce.ErrScorecardInternal,
		fmt.Sprintf("Unknown dangerous variable: %s", unsafeVar))
}

func newUnsafePattern(e, p string) unsafePattern {
	return unsafePattern{
		envvarName: e,
		// Regex to simply identify the unsafe variable that triggered the finding.
		// Must use a regex and not a simple string to identify possible uses of array variables
		// (i.e. `github.event.commits[0].author.email`).
		idRegex: regexp.MustCompile(p),
		// Regex to replace the unsafe variable in a `run` command with the envvar name.
		replaceRegex: regexp.MustCompile(`{{\s*.*?` + p + `.*?\s*}}`),
	}
}

// Parses the envvars from the existing global `env:` block.
// Returns a map from the GitHub variable name to the envvar name (i.e. "github.event.issue.body": "ISSUE_BODY").
func parseExistingEnvvars(workflow *actionlint.Workflow) map[string]string {
	envvars := make(map[string]string)

	if workflow.Env == nil {
		return envvars
	}

	r := regexp.MustCompile(`\$\{\{\s*(github\.[^\s]*?)\s*}}`)
	for _, v := range workflow.Env.Vars {
		value := v.Value.Value

		if strings.Contains(value, "${{") {
			// extract simple variable definition (without brackets, etc)
			m := r.FindStringSubmatch(value)
			if len(m) == 2 {
				value = m[1]
				envvars[value] = v.Name.Value
			} else {
				envvars[v.Value.Value] = v.Name.Value
			}
		} else {
			envvars[v.Value.Value] = v.Name.Value
		}
	}

	return envvars
}

// Identifies whether the original workflow contains envvars which may conflict with our patch.
// Should an existing envvar already handle our dangerous variable, it will be used in the patch instead of creating a
// new envvar with the same value.
// Should an existing envvar have the same name as the one that would ordinarily be used by the patch, the patch appends
// a suffix to the patch's envvar name to avoid conflicts.
//
// Returns the unsafePattern, possibly updated to consider the existing envvars.
func useExistingEnvvars(
	pattern unsafePattern,
	existingEnvvars map[string]string,
	unsafeVar string,
) (unsafePattern, error) {
	if envvar, ok := existingEnvvars[unsafeVar]; ok {
		// There already exists an envvar handling our unsafe variable.
		// Use that envvar instead of creating another one with the same value.
		pattern.envvarName = envvar
		return pattern, nil
	}

	// If there's an envvar with the same name as what we'd use, add a hard-coded suffix to our name to avoid conflicts.
	// Clumsy but works in almost all cases, and should be rare.
	for _, e := range existingEnvvars {
		if e == pattern.envvarName {
			pattern.envvarName += "_1"
			return pattern, nil
		}
	}

	return pattern, nil
}

// Replaces all instances of the given script injection variable with the safe environment variable.
func replaceUnsafeVarWithEnvvar(lines [][]byte, pattern unsafePattern, runIndex int) {
	runIndent := getIndent(lines[runIndex])
	for i, line := range lines[runIndex:] {
		currLine := runIndex + i
		if i > 0 && isParentLevelIndent(lines[currLine], runIndent) {
			// anything at the same indent as the first line of the  `- run:` block will mean the end of the run block.
			break
		}
		lines[currLine] = pattern.replaceRegex.ReplaceAll(line, []byte(pattern.envvarName))
	}
}

// Adds the necessary environment variable to the global `env:` block.
// If the `env:` block does not exist, it is created right above the `jobs:` block.
//
// Returns the new array of lines describing the workflow after inserting the new envvar.
func addEnvvarToGlobalEnv(
	lines [][]byte,
	existingEnvvars map[string]string,
	pattern unsafePattern, unsafeVar string,
) ([][]byte, error) {
	globalIndentation, err := findGlobalIndentation(lines)
	if err != nil {
		return lines, err
	}

	if _, ok := existingEnvvars[unsafeVar]; ok {
		// an existing envvar already handles this unsafe var, we can simply use it
		return lines, nil
	}

	var insertPos, envvarIndent int
	if len(existingEnvvars) > 0 {
		insertPos, envvarIndent = findExistingEnv(lines, globalIndentation)
	} else {
		lines, insertPos, err = addNewGlobalEnv(lines, globalIndentation)
		if err != nil {
			return lines, err
		}

		// position now points to `env:`, insert variables below it
		insertPos++
		envvarIndent = globalIndentation + getDefaultIndentStep(lines)
	}

	envvarDefinition := fmt.Sprintf("%s: ${{ %s }}", pattern.envvarName, unsafeVar)
	lines = slices.Insert(lines, insertPos, append(bytes.Repeat([]byte(" "), envvarIndent), []byte(envvarDefinition)...))

	return lines, nil
}

// Detects where the existing global `env:` block is located.
//
// Returns:
//   - int: the index for the line where a new global envvar should be added (after the last existing envvar)
//   - int: the indentation used for the declared environment variables
//
// Both values return -1 if the `env` block doesn't exist or is invalid.
func findExistingEnv(lines [][]byte, globalIndent int) (int, int) {
	var currPos int
	var line []byte
	envRegex := labelRegex("env", globalIndent)
	for currPos, line = range lines {
		if envRegex.Match(line) {
			break
		}
	}

	if currPos >= len(lines)-1 {
		// Invalid env, there must be at least one more line for an existing envvar. Shouldn't happen.
		return -1, -1
	}

	currPos++            // move to line after `env:`
	insertPos := currPos // marks the position where new envvars will be added
	var envvarIndent int
	for i, line := range lines[currPos:] {
		if isBlankOrComment(line) {
			continue
		}

		if isParentLevelIndent(line, globalIndent) {
			// no longer declaring envvars
			break
		}

		envvarIndent = getIndent(line)
		insertPos = currPos + i + 1
	}

	return insertPos, envvarIndent
}

// Adds a new global environment followed by a blank line to a workflow.
// Assumes a global environment does not yet exist.
//
// Returns:
//   - []string: the new array of lines describing the workflow, now with the global `env:` inserted.
//   - int: the row where the `env:` block was added
func addNewGlobalEnv(lines [][]byte, globalIndentation int) ([][]byte, int, error) {
	envPos, err := findNewEnvPos(lines, globalIndentation)
	if err != nil {
		return nil, -1, err
	}

	label := append(bytes.Repeat([]byte(" "), globalIndentation), []byte("env:")...)
	content := [][]byte{label}

	numBlankLines := getDefaultBlockSpacing(lines, globalIndentation)
	for i := 0; i < numBlankLines; i++ {
		content = append(content, []byte(""))
	}

	lines = slices.Insert(lines, envPos, content...)
	return lines, envPos, nil
}

// Returns the line where a new `env:` block should be inserted: right above the `jobs:` label.
func findNewEnvPos(lines [][]byte, globalIndent int) (int, error) {
	jobsRegex := labelRegex("jobs", globalIndent)
	for i, line := range lines {
		if jobsRegex.Match(line) {
			return i, nil
		}
	}

	return -1, sce.WithMessage(sce.ErrScorecardInternal, "Could not determine location for new environment")
}

// Returns the "global" indentation, as defined by the indentation on the required `on:` block.
// Will equal 0 in almost all cases.
func findGlobalIndentation(lines [][]byte) (int, error) {
	r := regexp.MustCompile(`^\s*on:`)
	for _, line := range lines {
		if r.Match(line) {
			return getIndent(line), nil
		}
	}

	return -1, sce.WithMessage(sce.ErrScorecardInternal, "Could not determine global indentation")
}

// Returns the indentation of the given line. The indentation is all leading whitespace and dashes.
func getIndent(line []byte) int {
	return len(line) - len(bytes.TrimLeft(line, " -"))
}

// Returns the "default" number of blank lines between blocks.
// The default is taken as the number of blank lines between the `jobs` label and the end of the preceding block.
func getDefaultBlockSpacing(lines [][]byte, globalIndent int) int {
	jobsRegex := labelRegex("jobs", globalIndent)

	var jobsIdx int
	var line []byte
	for jobsIdx, line = range lines {
		if jobsRegex.Match(line) {
			break
		}
	}

	numBlanks := 0
	for i := jobsIdx - 1; i >= 0; i-- {
		line := lines[i]

		if isBlank(line) {
			numBlanks++
		} else if !isComment(line) {
			// If the line is neither blank nor a comment, then we've reached the end of the previous block.
			break
		}
	}

	return numBlanks
}

// Returns whether the given line is a blank line (empty or only whitespace).
func isBlank(line []byte) bool {
	blank := regexp.MustCompile(`^\s*$`)
	return blank.Match(line)
}

// Returns whether the given line only contains comments.
func isComment(line []byte) bool {
	comment := regexp.MustCompile(`^\s*#`)
	return comment.Match(line)
}

func isBlankOrComment(line []byte) bool {
	return isBlank(line) || isComment(line)
}

// Returns whether the given line is at the same indentation level as the parent scope.
// For example, when walking through the document, parsing `job_foo`:
//
//	job_foo:
//	  runs-on: ubuntu-latest  # looping over these lines, we have
//	  uses: ./actions/foo     # parent_indent = 2 (job_foo's indentation)
//	  ...                     # we know these lines belong to job_foo because
//	  ...                     # they all have indent = 4
//	job_bar:  # this line has job_foo's indentation, so we know job_foo is done
//
// Blank lines and those containing only comments are ignored and always return false.
func isParentLevelIndent(line []byte, parentIndent int) bool {
	if isBlankOrComment(line) {
		return false
	}
	return getIndent(line) <= parentIndent
}

func labelRegex(label string, indent int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf("^%s%s:", strings.Repeat(" ", indent), label))
}

// Returns the default indentation step adopted in the document.
// This is taken from the difference in indentation between the `jobs:` label and the first job's label.
func getDefaultIndentStep(lines [][]byte) int {
	jobs := regexp.MustCompile(`^\s*jobs:`)
	var jobsIndex, jobsIndent int
	for i, line := range lines {
		if jobs.Match(line) {
			jobsIndex = i
			jobsIndent = getIndent(line)
			break
		}
	}

	jobIndent := jobsIndent + 2 // default value, should never be used
	for _, line := range lines[jobsIndex+1:] {
		if isBlankOrComment(line) {
			continue
		}
		jobIndent = getIndent(line)
		break
	}

	return jobIndent - jobsIndent
}

// Validates that the patch does not add any new syntax errors to the workflow. If the original workflow contains
// errors, then the patched version also might. As long as all the patch's errors match the original's, it is validated.
//
// Returns the array of new parsing errors caused by the patch.
func validatePatchedWorkflow(content []byte, originalErrs []*actionlint.Error) []*actionlint.Error {
	_, patchedErrs := actionlint.Parse(content)
	if len(patchedErrs) == 0 {
		return []*actionlint.Error{}
	}
	if len(originalErrs) == 0 {
		return patchedErrs
	}

	normalizeMsg := func(msg string) string {
		// one of the error messages contains line metadata that may legitimately change after a patch.
		// Only looking at the errors' first sentence eliminates this.
		return strings.Split(msg, ".")[0]
	}

	var newErrs []*actionlint.Error

	o := 0
	orig := originalErrs[o]
	origMsg := normalizeMsg(orig.Message)

	for _, patched := range patchedErrs {
		if o == len(originalErrs) {
			// no more errors in the original workflow, must be an error from our patch
			newErrs = append(newErrs, patched)
			continue
		}

		msg := normalizeMsg(patched.Message)
		if orig.Column == patched.Column && orig.Kind == patched.Kind && origMsg == msg {
			// Matched error, therefore not due to our patch.
			o++
			if o < len(originalErrs) {
				orig = originalErrs[o]
				origMsg = normalizeMsg(orig.Message)
			}
		} else {
			newErrs = append(newErrs, patched)
		}
	}

	return newErrs
}

// Returns the changes between the original and patched workflows as a unified diff (same as `git diff` or `diff -u`).
func getDiff(path string, original, patched []byte) (string, error) {
	// initialize an in-memory repository
	repo, err := newInMemoryRepo()
	if err != nil {
		return "", err
	}

	// commit original workflow
	originalCommit, err := commitWorkflow(path, original, repo)
	if err != nil {
		return "", err
	}

	// commit patched workflow
	patchedCommit, err := commitWorkflow(path, patched, repo)
	if err != nil {
		return "", err
	}

	// get diff between those commits
	return toUnifiedDiff(originalCommit, patchedCommit)
}

func newInMemoryRepo() (*git.Repository, error) {
	repo, err := git.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, fmt.Errorf("git.Init: %w", err)
	}

	return repo, nil
}

// Commits the workflow at the given path to the in-memory repository.
func commitWorkflow(path string, contents []byte, repo *git.Repository) (*object.Commit, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("repo.Worktree: %w", err)
	}
	filesystem := worktree.Filesystem

	// create (or overwrite) file
	df, err := filesystem.Create(path)
	if err != nil {
		return nil, fmt.Errorf("filesystem.Create: %w", err)
	}

	_, err = df.Write(contents)
	if err != nil {
		return nil, fmt.Errorf("df.Write: %w", err)
	}
	df.Close()

	// commit file to in-memory repository
	_, err = worktree.Add(path)
	if err != nil {
		return nil, fmt.Errorf("worktree.Add: %w", err)
	}

	hash, err := worktree.Commit("x", &git.CommitOptions{})
	if err != nil {
		return nil, fmt.Errorf("worktree.Commit: %w", err)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("repo.CommitObject: %w", err)
	}

	return commit, nil
}

// Returns a unified diff describing the difference between the given commits.
func toUnifiedDiff(originalCommit, patchedCommit *object.Commit) (string, error) {
	patch, err := originalCommit.Patch(patchedCommit)
	if err != nil {
		return "", fmt.Errorf("originalCommit.Patch: %w", err)
	}
	builder := strings.Builder{}
	err = patch.Encode(&builder)
	if err != nil {
		return "", fmt.Errorf("patch.Encode: %w", err)
	}

	return builder.String(), nil
}

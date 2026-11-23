// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	allowed "bt-code-review/internal/config/allowlist"
	"bt-code-review/internal/config/rules"
)

type change struct {
	Path  string
	Diff  string
	Added map[int]bool
}
type repository struct {
	rules        rules.Resolver
	filter       *rules.FileFilter
	dir, ref     string
	changedCount int
	root         *os.Root
	files        map[string]bool
	paths        []string
	changes      []change
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "--literal-pathspecs", "-C", dir}, args...)...)
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	if e := cmd.Run(); e != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], e, strings.TrimSpace(diagnostic.String()))
	}
	return out.String(), nil
}
func paths0(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\x00"), "\x00")
}
func (r *repository) eligible(p string) bool {
	if allowed.IsSecretPath(p) {
		return false
	}
	first, _, _ := strings.Cut(p, "/")
	switch first {
	case ".git", ".idea", ".vscode", ".svn", "vendor", "node_modules", "target", ".happypack", ".cachefile", "_packages", "rpm":
		return false
	}
	if r.filter != nil {
		if r.filter.IsUserExcluded(p) {
			return false
		}
		if r.filter.IsUserIncluded(p) {
			return true
		}
	}
	return allowed.IsAllowedExt(strings.ToLower(filepath.Ext(p))) && !allowed.IsExcludedPath(p)
}
func resolve(ctx context.Context, dir, ref string) (string, error) {
	s, e := git(ctx, dir, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(s), e
}

const maxFileBytes = 1024 * 1024

func (r *repository) read(ctx context.Context, p string) (string, error) {
	if !r.files[p] {
		return "", errors.New("path outside reviewable files")
	}
	if r.ref != "" {
		size, e := git(ctx, r.dir, "cat-file", "-s", r.ref+":"+p)
		if e != nil {
			return "", e
		}
		n, e := strconv.Atoi(strings.TrimSpace(size))
		if e != nil || n > maxFileBytes {
			return "", errors.New("file exceeds 1 MiB")
		}
		return git(ctx, r.dir, "cat-file", "blob", r.ref+":"+p)
	}
	st, e := r.root.Lstat(p)
	if e != nil {
		return "", e
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("expected a regular source file")
	}
	f, e := r.root.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if e != nil {
		return "", e
	}
	if len(b) > maxFileBytes {
		return "", errors.New("file exceeds 1 MiB")
	}
	return string(b), nil
}
func (r *repository) addPath(p string) {
	if r.eligible(p) && !r.files[p] {
		r.files[p] = true
		r.paths = append(r.paths, p)
	}
}
func inspectRepo(ctx context.Context, dir, commit, from, to, rulePath string) (*repository, error) {
	if commit != "" && (from != "" || to != "") || to != "" && from == "" {
		return nil, errors.New("use --commit or --from [--to]")
	}
	top, e := git(ctx, dir, "rev-parse", "--show-toplevel")
	if e != nil {
		return nil, e
	}
	r := &repository{dir: strings.TrimRight(top, "\r\n"), files: map[string]bool{}}
	base := ""
	untracked := map[string]bool{}
	if commit != "" || from != "" {
		target := commit
		if from != "" {
			target = to
			if target == "" {
				target = "HEAD"
			}
		}
		r.ref, e = resolve(ctx, r.dir, target)
		if e != nil {
			return nil, e
		}
	}
	r.rules, r.filter, e = rules.NewResolver(r.dir, rulePath, rules.ResolverOptions{Ref: r.ref})
	if e != nil {
		return nil, e
	}
	if r.ref != "" {
		if from != "" {
			ancestor, e := resolve(ctx, r.dir, from)
			if e != nil {
				return nil, e
			}
			base, e = git(ctx, r.dir, "merge-base", ancestor, r.ref)
			if e != nil {
				return nil, e
			}
		} else {
			line, e := git(ctx, r.dir, "rev-list", "--parents", "-n", "1", r.ref)
			if e != nil {
				return nil, e
			}
			ids := strings.Fields(line)
			if len(ids) > 1 {
				base = ids[1]
			}
		}
		list, e := git(ctx, r.dir, "ls-tree", "-rz", r.ref)
		if e != nil {
			return nil, e
		}
		for _, row := range paths0(list) {
			meta, p, ok := strings.Cut(row, "\t")
			if ok && strings.HasPrefix(meta, "100") {
				r.addPath(p)
			}
		}
	} else {
		base, e = resolve(ctx, r.dir, "HEAD")
		if e != nil {
			if _, err := git(ctx, r.dir, "symbolic-ref", "-q", "HEAD"); err != nil {
				return nil, e
			}
		}
		list, e := git(ctx, r.dir, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
		if e != nil {
			return nil, e
		}
		for _, p := range paths0(list) {
			if !r.eligible(p) {
				continue
			}
			st, e := os.Lstat(filepath.Join(r.dir, p))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return nil, e
			}
			if st.Mode().IsRegular() {
				r.addPath(p)
			}
		}
		list, e = git(ctx, r.dir, "ls-files", "--others", "--exclude-standard", "-z")
		if e != nil {
			return nil, e
		}
		for _, p := range paths0(list) {
			untracked[p] = true
		}
	}
	if strings.TrimSpace(base) == "" {
		base, e = git(ctx, r.dir, "hash-object", "-t", "tree", "--stdin")
		if e != nil {
			return nil, e
		}
	}
	refs := []string{strings.TrimSpace(base)}
	if r.ref != "" {
		refs = append(refs, r.ref)
	}
	common := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--diff-filter=ADMT"}
	names, e := git(ctx, r.dir, append(append(append([]string{}, common...), "--name-only", "-z"), refs...)...)
	if e != nil {
		return nil, e
	}
	changed := map[string]bool{}
	for _, p := range paths0(names) {
		changed[p] = true
	}
	for p := range untracked {
		changed[p] = true
	}
	r.changedCount = len(changed)
	r.root, e = os.OpenRoot(r.dir)
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			r.root.Close()
		}
	}()
	sort.Strings(r.paths)
	for _, p := range r.paths {
		if !changed[p] && !untracked[p] {
			continue
		}
		content, e := r.read(ctx, p)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", p, e)
		}
		if strings.ContainsRune(content, 0) || !utf8.ValidString(content) {
			continue
		}
		var patch string
		if untracked[p] {
			lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
			if content == "" {
				continue
			}
			patch = fmt.Sprintf("@@ -0,0 +1,%d @@\n+", len(lines)) + strings.Join(lines, "\n+") + "\n"
		} else {
			args := append(append(append([]string{}, common...), "--no-color"), refs...)
			patch, e = git(ctx, r.dir, append(args, "--", p)...)
			if e != nil {
				return nil, e
			}
		}
		if len(patch) > maxFileBytes {
			return nil, fmt.Errorf("%s: diff exceeds 1 MiB", p)
		}
		if patch != "" {
			r.changes = append(r.changes, change{p, patch, addedLines(patch)})
		}
	}
	success = true
	return r, nil
}

var hunk = regexp.MustCompile("^@@ -[0-9]+(?:,[0-9]+)? [+]([0-9]+)(?:,[0-9]+)? @@")

func addedLines(patch string) map[int]bool {
	lines := map[int]bool{}
	n := -1
	for _, s := range strings.Split(patch, "\n") {
		if m := hunk.FindStringSubmatch(s); m != nil {
			n, _ = strconv.Atoi(m[1])
			continue
		}
		if n < 0 || s == "" {
			continue
		}
		switch s[0] {
		case '+':
			lines[n] = true
			n++
		case ' ':
			n++
		}
	}
	return lines
}

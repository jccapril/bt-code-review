// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bt-code-review/internal/config/rules"
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
)

func reviewInstructions(rule, language string) string {
	if language == "" {
		language = "English"
	}
	return instructions + "\n\nReview criteria for this file:\n" + rule + "\n\nAlways respond in " + language + "."
}
func checkRules(ctx context.Context, args []string) error {
	const usage = "usage: btcr rules check [--repo DIR] [--rule FILE] PATH"
	if len(args) == 0 || args[0] != "check" {
		return errors.New(usage)
	}
	f := flag.NewFlagSet("rules check", flag.ContinueOnError)
	dir := f.String("repo", ".", "Git repository")
	custom := f.String("rule", "", "custom OCR rule JSON file")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return errors.New(usage)
	}
	root, err := git(ctx, *dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	path := filepath.Clean(f.Arg(0))
	if !filepath.IsLocal(path) {
		return errors.New("use a repository-relative file path")
	}
	resolver, _, err := rules.NewResolver(strings.TrimRight(root, "\r\n"), *custom, rules.ResolverOptions{})
	if err != nil {
		return err
	}
	detail := resolver.(rules.DetailResolver).ResolveDetail(filepath.ToSlash(path))
	fmt.Printf("File: %s\nSource: %s\nPattern: %s\n", path, detail.Source, detail.Pattern)
	if detail.SniffedAs != "" {
		fmt.Println("Detected:", detail.SniffedAs)
	}
	fmt.Println("Rule:")
	fmt.Println(detail.Rule)
	return nil
}

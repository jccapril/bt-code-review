// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Finding struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
type ToolRecord struct {
	Request    int
	ID         string
	Name       string
	Arguments  string
	Result     string
	OK         bool
	Started    time.Time
	DurationMS int64
}
type FileReport struct {
	Path      string
	Diff      string
	Status    string
	Findings  []Finding
	Tools     []string
	ToolCalls []ToolRecord `json:",omitempty"`
	Error     string       `json:",omitempty"`
}
type Report struct {
	ID, Repo, Revision, Mode, Model, Status string
	Started                                 time.Time
	Files                                   []FileReport
}

func definition(name, desc string, props object, required ...string) object {
	if required == nil {
		required = []string{}
	}
	return object{"name": name, "description": desc, "parameters": object{"type": "object", "properties": props, "required": required, "additionalProperties": false}}
}
func toolDefinitions() []object {
	str := object{"type": "string"}
	num := object{"type": "integer"}
	return []object{
		definition("file_find", "Find reviewable repository paths containing a substring.", object{"query": str}, "query"),
		definition("code_search", "Search source lines for a literal substring. Optionally restrict to a path.", object{"query": str, "path": str}, "query"),
		definition("file_read", "Read numbered lines from a reviewable source file. Lines are one-based.", object{"path": str, "start": num, "end": num}, "path"),
		definition("file_read_diff", "Read the current review diff for a file.", object{"path": str}, "path"),
		definition("code_comment", "Report a confirmed defect in the current file, anchored on an added line.", object{"line": num, "severity": object{"type": "string", "enum": []string{"low", "medium", "high", "critical"}}, "message": str}, "line", "severity", "message"),
		definition("task_done", "Finish reviewing the current file.", object{}),
	}
}
func clip(s string) string {
	const n = 32000
	if len(s) <= n {
		return s
	}
	end := n
	for !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "\n[truncated]"
}
func execute(ctx context.Context, r *repository, c change, fr *FileReport, fn function) (string, bool, error) {
	var a struct {
		Path, Query, Message, Severity string
		Line, Start, End               int
	}
	if e := json.Unmarshal([]byte(fn.Arguments), &a); e != nil {
		return "", false, fmt.Errorf("invalid tool arguments: %w", e)
	}
	switch fn.Name {
	case "task_done":
		return "Review complete.", true, nil
	case "code_comment":
		if !c.Added[a.Line] || strings.TrimSpace(a.Message) == "" {
			return "", false, errors.New("Supply an added line number and a nonempty message.")
		}
		switch a.Severity {
		case "low", "medium", "high", "critical":
		default:
			return "", false, errors.New("Invalid severity.")
		}
		f := Finding{a.Line, a.Severity, a.Message}
		for _, old := range fr.Findings {
			if old == f {
				return "Already recorded.", false, nil
			}
		}
		fr.Findings = append(fr.Findings, f)
		return "Recorded.", false, nil
	case "file_read_diff":
		for _, f := range r.changes {
			if f.Path == a.Path {
				return clip(f.Diff), false, nil
			}
		}
		return "", false, errors.New("No diff for that path.")
	case "file_read":
		s, e := r.read(ctx, a.Path)
		if e != nil {
			return "", false, e
		}
		lines := strings.Split(s, "\n")
		start := max(1, a.Start)
		end := a.End
		if end == 0 {
			end = start + 149
		}
		if start > len(lines) || end < start {
			return "", false, errors.New("Invalid line range.")
		}
		var out strings.Builder
		for n := start; n <= min(end, len(lines), start+499); n++ {
			fmt.Fprintf(&out, "%d| %s\n", n, lines[n-1])
		}
		return clip(out.String()), false, nil
	case "file_find", "code_search":
		if a.Query == "" {
			return "", false, errors.New("Supply a nonempty query.")
		}
		var out strings.Builder
		count := 0
		for _, p := range r.paths {
			if ctx.Err() != nil {
				return "", false, ctx.Err()
			}
			if fn.Name == "file_find" {
				if strings.Contains(strings.ToLower(p), strings.ToLower(a.Query)) {
					fmt.Fprintln(&out, p)
					count++
				}
			} else {
				if a.Path != "" && p != a.Path {
					continue
				}
				s, e := r.read(ctx, p)
				if e != nil {
					continue
				}
				for i, l := range strings.Split(s, "\n") {
					if strings.Contains(l, a.Query) {
						fmt.Fprintf(&out, "%s:%d: %s\n", p, i+1, l)
						count++
						if count >= 80 || out.Len() > 32000 {
							break
						}
					}
				}
			}
			if count >= 80 || out.Len() > 32000 {
				out.WriteString("[truncated]")
				break
			}
		}
		if out.Len() == 0 {
			return "No matches.", false, nil
		}
		return clip(out.String()), false, nil
	default:
		return "", false, errors.New("Unknown tool.")
	}
}
func reviewFile(ctx context.Context, r *repository, c change, m *modelClient, background string, fr *FileReport, progress terminal, checkpoint func() error) error {
	h := conversation{instructions: reviewInstructions(r.rules.Resolve(c.Path), m.cfg.Language), messages: []any{object{"role": "user", "content": fmt.Sprintf("Background: %s\nReview file: %s\nUnified diff:\n%s", background, c.Path, c.Diff)}}}
	for round := 0; round < 24; round++ {
		progress.log("Model request %d/24", round+1)
		calls, e := m.next(ctx, &h)
		if e != nil {
			return e
		}
		if len(calls) == 0 {
			h.messages = append(h.messages, object{"role": "user", "content": "Use the available review tools. Finish with task_done."})
			continue
		}
		done := false
		for _, c1 := range calls {
			if c1.ID == "" {
				return errors.New("tool call missing ID")
			}
			name := cleanText(c1.Function.Name, false)
			progress.log("  %s %s%s", progress.paint("36", "▶"), name, toolArgs(c1.Function))
			started := time.Now()
			fr.Tools = append(fr.Tools, c1.Function.Name)
			result, completed, toolErr := execute(ctx, r, c, fr, c1.Function)
			duration := time.Since(started)
			if toolErr != nil {
				result = toolErr.Error()
				progress.log("  %s %s failed: %s (%s)", progress.paint("31", "✘"), name, cleanText(result, false), elapsed(duration))
			} else {
				progress.log("  %s %s (%s)", progress.paint("32", "✔"), name, elapsed(duration))
			}
			fr.ToolCalls = append(fr.ToolCalls, ToolRecord{Request: round + 1, ID: c1.ID, Name: c1.Function.Name, Arguments: c1.Function.Arguments, Result: result, OK: toolErr == nil, Started: started, DurationMS: duration.Milliseconds()})
			if err := checkpoint(); err != nil {
				return fmt.Errorf("save tool record: %w", err)
			}
			m.addResult(&h, c1.ID, result)
			done = done || completed
		}
		if done {
			return nil
		}
	}
	return errors.New("tool round limit reached before task_done")
}
func review(ctx context.Context, dir string, args []string) error {
	f := flag.NewFlagSet("review", flag.ContinueOnError)
	repo := f.String("repo", ".", "Git repository")
	commit := f.String("commit", "", "review a commit")
	from := f.String("from", "", "base ref")
	to := f.String("to", "", "target ref (default HEAD)")
	bg := f.String("background", "", "review context")
	rulePath := f.String("rule", "", "custom OCR rule JSON file")
	preview := f.Bool("preview", false, "show selected files without a model")
	format := f.String("format", "text", "text or json")
	if e := parse(f, args); e != nil {
		return e
	}
	if *format != "text" && *format != "json" {
		return errors.New("--format must be text or json")
	}
	r, e := inspectRepo(ctx, *repo, *commit, *from, *to, *rulePath)
	if e != nil {
		return e
	}
	defer r.root.Close()
	progress := term(os.Stderr)
	output := term(os.Stdout)
	progress.log("%d file(s) changed, reviewing %d in %s", r.changedCount, len(r.changes), cleanText(r.dir, false))
	if *preview || len(r.changes) == 0 {
		paths := []string{}
		for _, c := range r.changes {
			paths = append(paths, c.Path)
		}
		if *format == "json" {
			return json.NewEncoder(os.Stdout).Encode(paths)
		}
		fmt.Printf("Will review (%d):\n", len(paths))
		for _, c := range r.changes {
			a, d := churn(c.Diff)
			fmt.Printf("  %s  +%d  -%d\n", cleanText(c.Path, false), a, d)
		}
		return nil
	}
	cfg, e := loadConfig(dir)
	if e != nil {
		return e
	}
	m, e := newModel(ctx, dir, cfg)
	if e != nil {
		return e
	}
	report := Report{ID: uuid.NewString(), Repo: r.dir, Revision: r.ref, Mode: cfg.Mode, Model: m.cfg.Model, Status: "running", Started: time.Now()}
	for _, c := range r.changes {
		report.Files = append(report.Files, FileReport{Path: c.Path, Diff: c.Diff, Status: "pending", Findings: []Finding{}})
	}
	path := filepath.Join(dir, "sessions", report.ID+".json")
	checkpoint := func() error { return saveJSON(path, &report) }
	if e = checkpoint(); e != nil {
		return e
	}
	progress.log("Reviewing each file separately · %s / %s", cfg.Mode, cleanText(m.cfg.Model, false))
	completed, count := 0, 0
	var failure error
	for i, c := range r.changes {
		fr := &report.Files[i]
		added, removed := churn(c.Diff)
		progress.log("[%d/%d] %s  +%d  -%d", i+1, len(r.changes), cleanText(c.Path, false), added, removed)
		fr.Status = "running"
		if e = reviewFile(ctx, r, c, m, *bg, fr, progress, checkpoint); e != nil {
			fr.Status = "failed"
			fr.Error = e.Error()
			progress.log("  %s %s failed: %s", progress.paint("31", "✘"), cleanText(fr.Path, false), cleanText(fr.Error, false))
			failure = e
		} else {
			fr.Status = "complete"
			completed++
		}
		count += len(fr.Findings)
		if e = checkpoint(); e != nil {
			return e
		}
		if failure != nil {
			break
		}
	}
	report.Status = "complete"
	if failure != nil {
		report.Status = "incomplete"
	}
	if e = checkpoint(); e != nil {
		return e
	}
	progress.log("Summary: %d/%d file(s) completed, %d comment(s)%s, %s elapsed", completed, len(report.Files), count, m.usage.String(), elapsed(time.Since(report.Started)))
	progress.log("Session: %s", report.ID)
	if *format == "json" {
		if e = json.NewEncoder(os.Stdout).Encode(report); e != nil {
			return e
		}
	} else {
		fmt.Printf("Review %s: %d finding(s) across %d selected file(s).\n", report.Status, count, len(report.Files))
		for _, fr := range report.Files {
			for _, v := range fr.Findings {
				output.finding(fr.Path, v)
			}
		}

	}
	return failure
}

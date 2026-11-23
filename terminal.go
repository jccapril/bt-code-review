// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type terminal struct {
	out   io.Writer
	color bool
}

func term(f *os.File) terminal {
	st, e := f.Stat()
	return terminal{f, e == nil && st.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}
func (t terminal) paint(code, s string) string {
	if !t.color {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}
func cleanText(s string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' && multiline {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func (t terminal) log(format string, args ...any) {
	fmt.Fprintf(t.out, "%s %s\n", t.paint("2", "[btcr]"), fmt.Sprintf(format, args...))
}
func elapsed(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}
func toolArgs(fn function) string {
	var a struct {
		Path, Query, Severity string
		Start, End, Line      int
	}
	if json.Unmarshal([]byte(fn.Arguments), &a) != nil {
		return ""
	}
	var fields []string
	add := func(k, v string) {
		if v != "" {
			v = cleanText(v, false)
			if strings.ContainsAny(v, " \"") {
				v = strconv.Quote(v)
			}
			fields = append(fields, k+"="+v)
		}
	}
	number := func(k string, n int) {
		if n != 0 {
			add(k, strconv.Itoa(n))
		}
	}
	switch fn.Name {
	case "code_search":
		add("search_text", a.Query)
		add("path", a.Path)
	case "file_find":
		add("query_name", a.Query)
	case "file_read", "file_read_diff":
		add("file_path", a.Path)
		number("start_line", a.Start)
		number("end_line", a.End)
	case "code_comment":
		number("line", a.Line)
		add("severity", a.Severity)
	}
	if len(fields) == 0 {
		return ""
	}
	return " " + strings.Join(fields, " ")
}
func churn(diff string) (added, removed int) {
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			continue
		}
		if !inHunk || len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return
}
func (t terminal) finding(path string, f Finding) {
	c := map[string]string{"critical": "1;91", "high": "91", "medium": "93", "low": "36"}[f.Severity]
	fmt.Fprintf(t.out, "\n%s\n%s %s\n", t.paint("2", fmt.Sprintf("─── %s:%d ───", cleanText(path, false), f.Line)), t.paint(c, "["+f.Severity+"]"), cleanText(f.Message, true))
}

type tokenUsage struct {
	Input  int64
	Output int64
	Total  int64
}

func (u *tokenUsage) add(v tokenUsage) { u.Input += v.Input; u.Output += v.Output; u.Total += v.Total }
func (u tokenUsage) String() string {
	if u.Total == 0 && u.Input == 0 && u.Output == 0 {
		return ""
	}
	if u.Total == 0 {
		u.Total = u.Input + u.Output
	}
	return fmt.Sprintf(", ~%d token(s) used (input: ~%d, output: ~%d)", u.Total, u.Input, u.Output)
}

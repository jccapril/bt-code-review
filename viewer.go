// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed viewer.html
var viewerHTML string
var reportPage = template.Must(template.New("page").Funcs(template.FuncMap{"prettyArgs": func(raw string) string {
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(raw), "", "  "); err != nil {
		return raw
	}
	return b.String()
}, "callTime": func(t time.Time) string { return t.Local().Format("15:04:05.000") }, "base": filepath.Base, "stamp": func(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }, "lines": func(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }, "lineClass": func(s string) string {
	switch {
	case strings.HasPrefix(s, "+"):
		return "added"
	case strings.HasPrefix(s, "-"):
		return "removed"
	case strings.HasPrefix(s, "@@"):
		return "hunk"
	}
	return ""
}}).Parse(viewerHTML))

type pageData struct {
	Sessions []Report
	Selected *Report
}

func loadReport(path string) (Report, error) {
	var r Report
	b, e := os.ReadFile(path)
	if e == nil {
		e = json.Unmarshal(b, &r)
	}
	return r, e
}
func reportHandler(dir, host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "Invalid host", 403)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		var data pageData
		if r.URL.Path == "/" {
			entries, e := os.ReadDir(dir)
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				http.Error(w, "Cannot read sessions", 500)
				return
			}
			for _, entry := range entries {
				if !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				s, e := loadReport(filepath.Join(dir, entry.Name()))
				if e != nil {
					http.Error(w, "Cannot read review report", 500)
					return
				}
				s.Files = nil
				data.Sessions = append(data.Sessions, s)
			}
			sort.Slice(data.Sessions, func(i, j int) bool { return data.Sessions[i].Started.After(data.Sessions[j].Started) })
		} else {
			id, ok := strings.CutPrefix(r.URL.Path, "/sessions/")
			parsed, e := uuid.Parse(id)
			if !ok || e != nil || parsed.String() != id {
				http.NotFound(w, r)
				return
			}
			report, e := loadReport(filepath.Join(dir, id+".json"))
			if errors.Is(e, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			if e != nil {
				http.Error(w, "Cannot read review report", 500)
				return
			}
			data.Selected = &report
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if e := reportPage.Execute(w, data); e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
	})
}
func openBrowser(ctx context.Context, url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", url).Run()
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return exec.CommandContext(ctx, "xdg-open", url).Run()
	}
}
func viewer(ctx context.Context, dir string, args []string) error {
	f := flag.NewFlagSet("viewer", flag.ContinueOnError)
	port := f.Int("port", 5483, "local TCP port (0 for a free port)")
	open := f.Bool("open", false, "open the viewer in a browser")
	if e := parse(f, args); e != nil {
		return e
	}
	l, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if e != nil {
		return e
	}
	defer l.Close()
	host := l.Addr().String()
	url := "http://" + host
	fmt.Fprintln(os.Stdout, url)
	srv := http.Server{Handler: reportHandler(filepath.Join(dir, "sessions"), host), ReadHeaderTimeout: 5 * time.Second}
	stop := context.AfterFunc(ctx, func() { _ = srv.Close() })
	defer stop()
	if *open {
		go func() {
			if e := openBrowser(ctx, url); e != nil {
				fmt.Fprintln(os.Stderr, "Open in browser:", url)
			}
		}()
	}
	e = srv.Serve(l)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", d}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(string(b), e)
		}
	}
	g("init", "-q")
	for p, s := range map[string]string{"orders.swift": "let title = orders.first ?? \"Empty\"\n", "guest.swift": "let orders: [String] = []\n", ".env": "SECRET", "unrelated.txt": "untouched"} {
		if e := os.WriteFile(filepath.Join(d, p), []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	g("add", ".")
	g("commit", "-qm", "fixture")
	if e := os.WriteFile(filepath.Join(d, "orders.swift"), []byte("let title = orders.first!\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return d
}
func toolCall(id, name, args string) object {
	return object{"id": id, "type": "function", "function": object{"name": name, "arguments": args}}
}
func TestReviewAndViewer(t *testing.T) {
	ctx := context.Background()
	repo := fixture(t)
	dir := t.TempDir()
	round := 0
	t.Setenv("BTCR_API_KEY", "fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("wrong endpoint or credential")
		}
		var req struct {
			Messages []object
			Tools    []object
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		prompt, _ := req.Messages[0]["content"].(string)
		for _, want := range []string{"System-Specific Rules", "Inspect guest orders", "Always respond in Chinese."} {
			if !strings.Contains(prompt, want) {
				t.Errorf("model request missing %q", want)
			}
		}
		if len(req.Tools) != 6 {
			t.Error("missing tools")
		}
		calls := []object{}
		switch round {
		case 0:
			calls = append(calls, toolCall("missing", "file_read", `{"path":"missing.swift"}`), toolCall("one", "file_read", `{"path":"guest.swift"}`), toolCall("two", "code_search", `{"query":"title"}`))
		case 1:
			paths, err := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
			if err != nil || len(paths) != 1 {
				t.Error("missing in-progress report")
			} else if live, err := loadReport(paths[0]); err != nil || len(live.Files) != 1 || len(live.Files[0].ToolCalls) != 3 {
				t.Error("missing in-progress tool history", err)
			} else {
				for i, tc := range live.Files[0].ToolCalls {
					message := req.Messages[len(req.Messages)-3+i]
					if tc.ID != message["tool_call_id"] || tc.Result != message["content"] || tc.Request != 1 || tc.OK != (i != 0) || tc.Started.IsZero() {
						t.Errorf("incorrect recorded tool result: %+v", tc)
					}
				}
			}
			n := len(req.Messages)
			if n < 2 || req.Messages[n-2]["tool_call_id"] != "one" || req.Messages[n-1]["tool_call_id"] != "two" {
				t.Error("batch output not correlated")
			}
			calls = append(calls, toolCall("three", "code_comment", `{"line":1,"severity":"high","message":"Empty orders <script> crash."}`), toolCall("four", "task_done", "{}"))
		default:
			t.Error("unexpected round")
			http.Error(w, "unexpected round", 500)
			return
		}
		round++
		_ = json.NewEncoder(w).Encode(object{"usage": object{"prompt_tokens": 20, "completion_tokens": 5, "total_tokens": 25}, "choices": []object{{"message": object{"role": "assistant", "tool_calls": calls}}}})
	}))
	defer server.Close()
	ruleFile := filepath.Join(dir, "custom.json")
	if err := saveJSON(ruleFile, object{"rules": []object{{"path": "**/*.swift", "rule": "Inspect guest orders", "merge_system_rule": true}}}); err != nil {
		t.Fatal(err)
	}
	if e := configure(dir, []string{"set", "language", "Chinese"}); e != nil {
		t.Fatal(e)
	}
	if e := configure(dir, []string{"api", "--base-url", server.URL + "/v1", "--model", "test"}); e != nil {
		t.Fatal(e)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	func() {
		oldOut, oldErr := os.Stdout, os.Stderr
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
		os.Stdout, os.Stderr = stdout, stderr
		if e := review(ctx, dir, []string{"--repo", repo, "--rule", ruleFile, "--format", "json"}); e != nil {
			t.Fatal(e)
		}
	}()
	raw, err := os.ReadFile(stdout.Name())
	if err != nil || !json.Valid(raw) {
		t.Fatal("invalid JSON stdout", err)
	}
	log, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 file(s) changed, reviewing 1", "▶ file_read file_path=guest.swift", "✔ file_read (", "▶ code_search search_text=title", "Summary: 1/1 file(s) completed, 1 comment(s), ~50 token(s)", "Session: "} {
		if !strings.Contains(string(log), want) {
			t.Errorf("missing %q in %s", want, log)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	s, e := loadReport(files[0])
	if e != nil || s.Status != "complete" || len(s.Files) != 1 || len(s.Files[0].Findings) != 1 {
		t.Fatal(s, e)
	}
	records := s.Files[0].ToolCalls
	if len(records) != 5 || records[0].Arguments != `{"path":"missing.swift"}` || records[3].Request != 2 || records[4].Name != "task_done" {
		t.Fatal(records)
	}
	handler := reportHandler(filepath.Dir(files[0]), "example.com")
	for _, path := range []string{"/", "/sessions/" + s.ID} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if path != "/" {
			for _, want := range []string{"模型请求 2 次 · 输入 40 · 输出 10 · 合计 50 token", "请求参数", "工具响应", "错误响应", "missing.swift", "let orders: [String] = []", "模型请求 2", "&lt;script&gt;"} {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("viewer missing %q", want)
				}
			}
		}
		if w.Code != 200 || strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), filepath.Base(repo)) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://foreign.example/", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestHistoricalSourcesAndSecrets(t *testing.T) {
	repo := fixture(t)
	r, e := inspectRepo(context.Background(), repo, "HEAD", "", "", "")
	if e != nil {
		t.Fatal(e)
	}
	defer r.root.Close()
	if r.files[".env"] {
		t.Fatal("included secret")
	}
	s, e := r.read(context.Background(), "orders.swift")
	if e != nil || !strings.Contains(s, "Empty") {
		t.Fatal(s, e)
	}
	if _, e = r.read(context.Background(), "../x"); e == nil {
		t.Fatal("outside path accepted")
	}
}
func TestSubscriptionToolBatch(t *testing.T) {
	h := conversation{}
	items := []object{{"type": "reasoning", "encrypted_content": "opaque"}, {"type": "function_call", "call_id": "one", "name": "file_read", "arguments": "{}"}, {"type": "function_call", "call_id": "two", "name": "code_search", "arguments": "{}"}}
	b, _ := json.Marshal(object{"type": "response.completed", "response": object{"status": "completed", "output": items, "usage": object{"input_tokens": 30, "output_tokens": 4, "input_tokens_details": object{"cached_tokens": 12, "cache_creation_tokens": 8}}}})
	calls, e := responsesTurn(strings.NewReader(fmt.Sprintf("data: %s\n\n", b)), &h)
	if e != nil || len(calls) != 2 || len(h.messages) != 3 {
		t.Fatal(e, calls)
	}
	summary := FileReport{Requests: []RequestRecord{h.last, {}}}.Usage()
	if summary.Total != 34 || summary.CacheWrite != 8 || summary.Cached != 12 || summary.Reported != 1 || summary.Requests != 2 {
		t.Fatalf("unexpected usage: %+v", summary)
	}
	m := modelClient{cfg: config{Mode: "chatgpt"}}
	m.addResult(&h, calls[1].ID, "matches")
	last := h.messages[3].(object)
	if last["call_id"] != "two" {
		t.Fatal(last)
	}
	if _, e = responsesTurn(strings.NewReader("data: [DONE]\n\n"), &h); e == nil {
		t.Fatal("accepted truncated stream")
	}
}

func TestSubscriptionItemEvents(t *testing.T) {
	const stream = "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"name\":\"task_done\",\"call_id\":\"done\",\"arguments\":\"{}\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"
	var h conversation
	calls, err := responsesTurn(strings.NewReader(stream), &h)
	if err != nil || len(calls) != 1 || calls[0].ID != "done" || len(h.messages) != 1 {
		t.Fatal(calls, err)
	}
}

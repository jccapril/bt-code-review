// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/AlecAivazis/survey/v2"
	surveyterm "github.com/AlecAivazis/survey/v2/terminal"
)

func TestProviderWizardSelectAndCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer saved" {
			t.Error("unexpected catalog request")
		}
		w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"}]}`))
	}))
	defer server.Close()
	t.Setenv("BTCR_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	for _, cancel := range []bool{true, false} {
		dir := t.TempDir()
		cfg := config{Mode: "api", Model: "a", BaseURL: server.URL, APIKey: "saved", Language: "Chinese"}
		if err := saveJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
			t.Fatal(err)
		}
		step := 0
		err := selectProvider(context.Background(), dir, func(prompt survey.Prompt, dest any) error {
			menu := prompt.(*survey.Select)
			if step == 0 && (len(menu.Options) != 3 || menu.Default != "OpenAI API") {
				t.Fatalf("provider menu: %v", menu)
			}
			if step == 1 && cancel {
				return surveyterm.InterruptErr
			}
			*dest.(*int) = 1
			step++
			return nil
		})
		if cancel && !errors.Is(err, surveyterm.InterruptErr) || !cancel && err != nil {
			t.Fatal(err)
		}
		if !cancel {
			cfg.Model = "b"
		}
		got, err := loadConfig(dir)
		if err != nil || got != cfg {
			t.Fatal("incorrect saved configuration", err)
		}
	}
}

func TestResponsesAPIReviewTurns(t *testing.T) {
	t.Setenv("BTCR_API_KEY", "test-key")
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("unexpected Responses request")
		}
		var body struct {
			Stream bool
			Input  []struct {
				Type   string
				ID     string `json:"call_id"`
				Output string
			}
			Tools []struct{ Type, Name string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if !body.Stream || len(body.Tools) == 0 || body.Tools[0].Type != "function" || body.Tools[0].Name == "" {
			t.Error("expected streaming and flat function definitions")
		}
		name := "file_read"
		if turn == 1 {
			if len(body.Input) != 3 || body.Input[1].Type != "function_call" || body.Input[2].Type != "function_call_output" || body.Input[2].ID != "call-0" || body.Input[2].Output != "file contents" {
				t.Error("missing previous call or tool output")
			}
			name = "task_done"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"call-%d\",\"name\":%q,\"arguments\":\"{}\"}]}}\n\n", turn, name)
		turn++
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := configure(dir, []string{"responses", "--base-url", srv.URL + "/v1", "--model", "test"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	client, err := newModel(ctx, dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := conversation{messages: []any{object{"role": "user", "content": "review"}}}
	for _, want := range []string{"file_read", "task_done"} {
		calls, err := client.next(ctx, &h)
		if err != nil || len(calls) != 1 || calls[0].Function.Name != want {
			t.Fatalf("unexpected calls %v, %v", calls, err)
		}
		client.addResult(&h, calls[0].ID, "file contents")
	}
}

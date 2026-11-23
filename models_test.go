// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestChooseAPIModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer saved" {
			t.Errorf("unexpected catalog request")
		}
		w.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a"}]}`))
	}))
	defer server.Close()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("BTCR_API_KEY", "")
	dir := t.TempDir()
	cfg := config{Mode: "api", BaseURL: server.URL + "/v1", APIKey: "saved"}
	if err := saveJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args            []string
		input, expected string
	}{{[]string{"--list"}, "2\n", ""}, {nil, "\n", ""}, {nil, "9\n2\n", "model-b"}} {
		var out bytes.Buffer
		if err := chooseModel(context.Background(), dir, tc.args, strings.NewReader(tc.input), &out, false); err != nil {
			t.Fatal(err)
		}
		got, err := loadConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		want := cfg
		want.Model = tc.expected
		if got != want || !strings.Contains(out.String(), "model-a") {
			t.Errorf("got model %q, output %s", got.Model, out.String())
		}
	}
}

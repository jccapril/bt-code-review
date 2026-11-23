// SPDX-License-Identifier: Apache-2.0
package main

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestViewerHistoryAndComparison(t *testing.T) {
	dir := t.TempDir()
	repo := "/work/app & lib"
	older := Report{ID: uuid.NewString(), Repo: repo, Started: time.Unix(1, 0), Files: []FileReport{
		{Path: "a", Status: "complete", Findings: []Finding{{Message: "old"}, {Message: "persists", Line: 1}}},
		{Path: "b", Status: "complete", Findings: []Finding{{Message: "unchecked"}}},
	}}
	newer := Report{ID: uuid.NewString(), Repo: repo, Started: time.Unix(2, 0), Files: []FileReport{
		{Path: "a", Status: "complete", Findings: []Finding{{Message: "new"}, {Message: "persists", Line: 9}}},
		{Path: "b", Status: "failed"},
	}}
	other := Report{ID: uuid.NewString(), Repo: "/work/other"}
	for _, r := range []Report{older, newer, other} {
		if err := saveJSON(filepath.Join(dir, r.ID+".json"), r); err != nil {
			t.Fatal(err)
		}
	}
	h := reportHandler(dir, "example.com")
	compare := "/compare?before=" + older.ID + "&after="
	for _, tc := range []struct {
		path   string
		status int
		text   []string
	}{
		{"/repo?path=" + url.QueryEscape(repo), 200, []string{newer.ID, older.ID, "与上次比较", "2 条意见"}},
		{compare + newer.ID, 200, []string{"新增（1）", "持续存在（1）", "已解决（1）", "未复审（1）", "unchecked"}},
		{compare + other.ID, 400, nil}, {compare + "invalid", 404, nil},
		{"/sessions/" + older.ID, 200, []string{"persists", "a", "审核覆盖"}},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status {
			t.Errorf("%s: %d", tc.path, w.Code)
		}
		for _, want := range tc.text {
			if !strings.Contains(w.Body.String(), want) {
				t.Errorf("%s missing %q", tc.path, want)
			}
		}
	}
}

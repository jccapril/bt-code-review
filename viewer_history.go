// SPDX-License-Identifier: Apache-2.0
package main

import (
	"sort"
	"strings"
	"time"
)

type sessionRow struct {
	ID, Repo, Branch, Mode, Model, Status, Duration, Previous string
	Started                                                   time.Time
	Files, Findings                                           int
}

func (r Report) Duration() string {
	if r.Finished == nil {
		return ""
	}
	return elapsed(r.Finished.Sub(r.Started))
}
func (r Report) Coverage() map[string]int {
	counts := map[string]int{}
	for _, f := range r.Files {
		counts[f.Status]++
		counts["findings"] += len(f.Findings)
	}
	return counts
}

type repoRow struct {
	Path     string
	Sessions []sessionRow
}

func history(reports []sessionRow) []repoRow {
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Started.After(reports[j].Started) })
	indices := map[string]int{}
	var repos []repoRow
	for _, s := range reports {
		i, ok := indices[s.Repo]
		if !ok {
			i = len(repos)
			indices[s.Repo] = i
			repos = append(repos, repoRow{Path: s.Repo})
		}
		repo := &repos[i]
		if n := len(repo.Sessions); n > 0 {
			repo.Sessions[n-1].Previous = s.ID
		}
		repo.Sessions = append(repo.Sessions, s)
	}
	return repos
}

type locatedFinding struct {
	Path string
	Finding
}
type findingGroup struct {
	Title string
	Items []locatedFinding
}
type comparison struct {
	Before, After Report
	Groups        []findingGroup
}

func (f locatedFinding) key() [3]string {
	text := f.ExistingCode
	if text == "" {
		text = f.Message
	}
	return [3]string{f.Path, strings.ToLower(f.Category), strings.Join(strings.Fields(text), " ")}
}
func compareReports(before, after Report) comparison {
	groups := []findingGroup{{Title: "新增"}, {Title: "持续存在"}, {Title: "已解决"}, {Title: "未复审"}}
	remaining := map[[3]string]int{}
	matched := map[[3]string]int{}
	reviewed := map[string]bool{}
	for _, file := range before.Files {
		for _, v := range file.Findings {
			remaining[(locatedFinding{file.Path, v}).key()]++
		}
	}
	for _, file := range after.Files {
		reviewed[file.Path] = file.Status == "complete"
		for _, v := range file.Findings {
			f := locatedFinding{file.Path, v}
			k := f.key()
			bucket := 0
			if remaining[k] > 0 {
				remaining[k]--
				matched[k]++
				bucket = 1
			}
			groups[bucket].Items = append(groups[bucket].Items, f)
		}
	}
	for _, file := range before.Files {
		for _, v := range file.Findings {
			f := locatedFinding{file.Path, v}
			k := f.key()
			if matched[k] > 0 {
				matched[k]--
				continue
			}
			bucket := 3
			if reviewed[file.Path] {
				bucket = 2
			}
			groups[bucket].Items = append(groups[bucket].Items, f)
		}
	}
	return comparison{before, after, groups}
}

type requestCard struct {
	RequestRecord
	Number   int
	Tools    []ToolRecord
	Recorded bool
}

func (f FileReport) Cards() []requestCard {
	cards := make([]requestCard, len(f.Requests))
	byNumber := map[int]int{}
	for i, req := range f.Requests {
		cards[i] = requestCard{RequestRecord: req, Number: i + 1, Recorded: true}
		byNumber[i+1] = i
	}
	for _, tool := range f.ToolCalls {
		i, ok := byNumber[tool.Request]
		if !ok {
			i = len(cards)
			byNumber[tool.Request] = i
			cards = append(cards, requestCard{Number: tool.Request})
		}
		cards[i].Tools = append(cards[i].Tools, tool)
	}
	return cards
}

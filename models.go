// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"bt-code-review/internal/chatgptauth"
	surveyterm "github.com/AlecAivazis/survey/v2/terminal"
)

func (m *modelClient) catalog(ctx context.Context) ([]chatgptauth.Model, error) {
	if m.cfg.Mode == "chatgpt" {
		account, e := m.auth.Credentials(ctx, m.store, "")
		if e != nil {
			return nil, e
		}
		return m.auth.Models(ctx, account)
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.cfg.BaseURL, "/")+"/models", nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	res, e := m.http.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("API model catalog returned %s; use btcr config %s --model NAME if your endpoint has no catalog", res.Status, m.cfg.Mode)
	}
	var catalog struct{ Data []struct{ ID string } }
	if e = json.NewDecoder(res.Body).Decode(&catalog); e != nil {
		return nil, e
	}
	if catalog.Data == nil {
		return nil, errors.New("invalid API catalog: missing data array")
	}
	var models []chatgptauth.Model
	for _, item := range catalog.Data {
		if item.ID == "" {
			return nil, errors.New("invalid API catalog: empty model ID")
		}
		models = append(models, chatgptauth.Model{Slug: item.ID})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Slug < models[j].Slug })
	return models, nil
}

func chooseModel(ctx context.Context, dir string, args []string, in io.Reader, out io.Writer, arrows bool) error {
	flags := flag.NewFlagSet("config model", flag.ContinueOnError)
	list := flags.Bool("list", false, "display the account catalog without choosing")
	if e := parse(flags, args); e != nil {
		return e
	}
	cfg, e := loadConfig(dir)
	if e != nil {
		return e
	}
	client, e := providerClient(dir, cfg)
	if e != nil {
		return e
	}
	models, e := client.catalog(ctx)
	if e != nil {
		return e
	}
	if len(models) == 0 {
		return errors.New("no models listed for this account")
	}
	if arrows && !*list {
		cfg.Model, e = pickModel(models, cfg.Model, askTerminal)
		if errors.Is(e, surveyterm.InterruptErr) {
			return nil
		}
		if e != nil {
			return e
		}
		return saveSelectedModel(dir, cfg, out)
	}
	for i, m := range models {
		label := strconv.QuoteToGraphic(m.Slug)
		if m.DisplayName != "" && m.DisplayName != m.Slug {
			label += " (" + strconv.QuoteToGraphic(m.DisplayName) + ")"
		}
		if m.Slug == cfg.Model {
			label += " [current]"
		} else if cfg.Mode == "chatgpt" && cfg.Model == "" && i == 0 {
			label += " [automatic]"
		}
		fmt.Fprintf(out, "%d. %s\n", i+1, label)
	}
	if *list {
		return nil
	}
	reader := bufio.NewScanner(in)
	for {
		fmt.Fprintf(out, "Select 1-%d (Enter to cancel): ", len(models))
		if !reader.Scan() {
			return reader.Err()
		}
		value := strings.TrimSpace(reader.Text())
		if value == "" {
			return nil
		}
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > len(models) {
			fmt.Fprintln(out, "Invalid selection.")
			continue
		}
		cfg.Model = models[n-1].Slug
		return saveSelectedModel(dir, cfg, out)
	}
}

func saveSelectedModel(dir string, cfg config, out io.Writer) error {
	if err := saveJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "Selected %q.\n", cfg.Model)
	return nil
}

// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"bt-code-review/internal/chatgptauth"
	"github.com/AlecAivazis/survey/v2"
	surveyterm "github.com/AlecAivazis/survey/v2/terminal"
)

func providerWizard(ctx context.Context, dir string, args []string) error {
	flags := flag.NewFlagSet("config provider", flag.ContinueOnError)
	if err := parse(flags, args); err != nil {
		return err
	}
	err := selectProvider(ctx, dir, askTerminal)
	if errors.Is(err, surveyterm.InterruptErr) {
		return nil
	}
	return err
}

func askTerminal(p survey.Prompt, dest any) error { return survey.AskOne(p, dest) }

func selectProvider(ctx context.Context, dir string, ask func(survey.Prompt, any) error) error {
	old, err := readSettings(dir)
	if err != nil {
		return err
	}
	options := []string{"ChatGPT plan (Sign in with ChatGPT preview)", "OpenAI API", "OpenAI Responses API"}
	choice := 0
	modes := []string{"chatgpt", "api", "responses"}
	for i, mode := range modes {
		if old.Mode == mode {
			choice = i
		}
	}
	if err := ask(&survey.Select{Message: "Select provider", Options: options, Default: options[choice]}, &choice); err != nil {
		return err
	}
	mode := modes[choice]
	cfg := config{Mode: mode, Language: old.Language}
	if old.Mode == mode {
		cfg = old
	}
	if cfg.usesAPIKey() {
		if old.usesAPIKey() {
			cfg.BaseURL, cfg.APIKey = old.BaseURL, old.APIKey
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.openai.com/v1"
		}
		if err := cfg.validate(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "API endpoint: %s\n", cfg.BaseURL)
		if cfg.APIKey == "" && os.Getenv("BTCR_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
			if err := ask(&survey.Password{Message: "OpenAI API key"}, &cfg.APIKey); err != nil {
				return err
			}
			cfg.APIKey = strings.TrimSpace(cfg.APIKey)
		}
	}
	client, err := providerClient(dir, cfg)
	if err != nil {
		return err
	}
	if mode == "chatgpt" {
		account, err := client.store.Selected("")
		if err != nil && !errors.Is(err, chatgptauth.ErrNotFound) {
			return err
		}
		if account == nil || !account.PlanEnabled() || account.RefreshToken == "" && account.AccessToken == "" {
			if _, err = client.auth.Login(ctx, client.store, "", false, false, account != nil, os.Stderr); err != nil {
				return err
			}
		}
	}
	models, err := client.catalog(ctx)
	if err != nil {
		return err
	}
	cfg.Model, err = pickModel(models, cfg.Model, ask)
	if err != nil {
		return err
	}
	if err := saveJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		return err
	}
	fmt.Printf("Configured %s with %q.\n", options[choice], cfg.Model)
	return nil
}

func pickModel(models []chatgptauth.Model, current string, ask func(survey.Prompt, any) error) (string, error) {
	if len(models) == 0 {
		return "", errors.New("no models listed for this account")
	}
	labels := make([]string, len(models))
	selected := 0
	for i, m := range models {
		labels[i] = strconv.QuoteToGraphic(m.Slug)
		if m.DisplayName != "" && m.DisplayName != m.Slug {
			labels[i] += " (" + strconv.QuoteToGraphic(m.DisplayName) + ")"
		}
		if m.Slug == current {
			selected = i
		}
	}
	if err := ask(&survey.Select{Message: "Select model", Options: labels, Default: labels[selected]}, &selected); err != nil {
		return "", err
	}
	return models[selected].Slug, nil
}

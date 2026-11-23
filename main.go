// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"bt-code-review/internal/chatgptauth"
)

const version = "0.1.5"

const help = `btcr - local AI code review

  btcr config provider
  btcr config api [--model NAME] [--base-url URL] [--api-key-stdin]
  btcr config responses [--model NAME] [--base-url URL] [--api-key-stdin]
  btcr config chatgpt [--model NAME]
  btcr config set language NAME
  btcr rules check [--repo DIR] [--rule FILE] PATH
  btcr config show
  btcr config model [--list]
  btcr login [--no-browser] [--enable-plan] [--account ID]
  btcr logout
  btcr review [--repo DIR] [--commit REF | --from REF [--to REF]]
              [--preview] [--rule FILE] [--background TEXT] [--format text|json]
  btcr viewer [--port 5483]

API mode accepts OPENAI_API_KEY or BTCR_API_KEY. ChatGPT uses browser login.
Workspace reviews include staged, unstaged and non-ignored untracked source files.
`

type config struct {
	Language string `json:"language,omitempty"`
	Mode     string `json:"mode"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

func home() (string, error) { h, e := os.UserHomeDir(); return filepath.Join(h, ".btcr"), e }
func saveJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	dir := filepath.Dir(path)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".btcr-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func readSettings(dir string) (config, error) {
	var c config
	b, e := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func loadConfig(dir string) (config, error) {
	c, e := readSettings(dir)
	if e != nil {
		return c, e
	}
	if c.Mode == "" {
		return c, errors.New("configure a provider with btcr config provider")
	}
	return c, c.validate()
}
func (c config) usesAPIKey() bool { return c.Mode == "api" || c.Mode == "responses" }
func (c config) validate() error {
	switch c.Mode {
	case "chatgpt":
		if c.BaseURL != "" || c.APIKey != "" {
			return errors.New("ChatGPT mode uses its own endpoint and browser credentials")
		}
	case "api", "responses":
		u, e := url.Parse(c.BaseURL)
		if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
			return errors.New("use an HTTPS API base URL or loopback HTTP URL")
		}
	default:
		return fmt.Errorf("unsupported mode %q", c.Mode)
	}
	return nil
}
func parse(f *flag.FlagSet, args []string) error {
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", f.Arg(0))
	}
	return nil
}
func configure(dir string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: btcr config provider|model|api|responses|chatgpt|set|show")
	}
	old, e := readSettings(dir)
	if e != nil {
		return e
	}
	if args[0] == "set" {
		if len(args) != 3 || args[1] != "language" || strings.TrimSpace(args[2]) == "" {
			return errors.New("usage: btcr config set language NAME")
		}
		old.Language = strings.TrimSpace(args[2])
		return saveJSON(filepath.Join(dir, "config.json"), old)
	}
	if args[0] == "show" {
		if len(args) != 1 {
			return errors.New("usage: btcr config show")
		}
		c := old
		if c.APIKey != "" {
			c.APIKey = "[configured]"
		}
		return json.NewEncoder(os.Stdout).Encode(c)
	}
	c := config{Mode: args[0], Language: old.Language}
	f := flag.NewFlagSet("config", flag.ContinueOnError)
	f.StringVar(&c.Model, "model", "", "model identifier")
	var stdin bool
	if c.usesAPIKey() {
		f.StringVar(&c.BaseURL, "base-url", "https://api.openai.com/v1", "OpenAI-compatible API base URL")
		f.BoolVar(&stdin, "api-key-stdin", false, "read and save API key from stdin")
	}
	if e := parse(f, args[1:]); e != nil {
		return e
	}
	if e := c.validate(); e != nil {
		return e
	}
	if stdin {
		b, e := io.ReadAll(os.Stdin)
		if e != nil {
			return e
		}
		c.APIKey = strings.TrimSpace(string(b))
		if c.APIKey == "" {
			return errors.New("empty API key")
		}
	} else if c.usesAPIKey() {
		if old.usesAPIKey() && old.BaseURL == c.BaseURL {
			c.APIKey = old.APIKey
		}
	}
	return saveJSON(filepath.Join(dir, "config.json"), c)
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(help)
		return nil
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Println("btcr", version)
		return nil
	}
	dir, e := home()
	if e != nil {
		return e
	}
	store := chatgptauth.Store{Dir: filepath.Join(dir, "auth", "chatgpt")}
	switch args[0] {
	case "rules":
		return checkRules(ctx, args[1:])
	case "models":
		return chooseModel(ctx, dir, args[1:], os.Stdin, os.Stdout, false)
	case "config":
		if len(args) >= 2 && args[1] == "model" {
			return chooseModel(ctx, dir, args[2:], os.Stdin, os.Stdout, true)
		}
		if len(args) >= 2 && args[1] == "provider" {
			return providerWizard(ctx, dir, args[2:])
		}
		return configure(dir, args[1:])
	case "login":
		f := flag.NewFlagSet("login", flag.ContinueOnError)
		noBrowser := f.Bool("no-browser", false, "print authorization URL")
		consent := f.Bool("enable-plan", false, "request plan consent for an existing registration")
		id := f.String("account", "", "existing account identifier")
		if e = parse(f, args[1:]); e != nil {
			return e
		}
		a, e := chatgptauth.NewClient().Login(ctx, store, *id, false, *noBrowser, *consent, os.Stderr)
		if e != nil {
			return e
		}
		if !a.PlanEnabled() {
			return errors.New("signed in; run btcr login --enable-plan to enable subscription usage")
		}
		fmt.Println("Signed in with ChatGPT.")
		return nil
	case "logout":
		if len(args) != 1 {
			return errors.New("usage: btcr logout")
		}
		return chatgptauth.NewClient().Logout(ctx, store, "")
	case "review":
		return review(ctx, dir, args[1:])
	case "viewer":
		return viewer(ctx, dir, args[1:])
	default:
		return fmt.Errorf("unknown command %q; see btcr --help", args[0])
	}
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if e := run(ctx, os.Args[1:]); e != nil && !errors.Is(e, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "btcr:", e)
		os.Exit(1)
	}
}

// Copyright 2026 Sylvester Francis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/model"
	"github.com/sylvester-francis/drover/provider"
	"github.com/sylvester-francis/drover/runner"
	"github.com/sylvester-francis/drover/tools"
	"github.com/sylvester-francis/rerun"
	"github.com/sylvester-francis/rerun/sqlite"
)

const defaultSystem = "You are drover, a careful autonomous agent. Work step by " +
	"step, calling the available tools when they help. When you have the answer, " +
	"reply with it directly and stop."

// jobFlags are the settings shared by run and resume.
type jobFlags struct {
	provider   string
	model      string
	system     string
	leashURL   string
	db         string
	apiKey     string
	leashToken string
	maxSteps   int
}

func registerJobFlags(fs *flag.FlagSet) *jobFlags {
	c := &jobFlags{}
	fs.StringVar(&c.provider, "provider", envStr("DROVER_PROVIDER", "openai"), "model provider: openai, anthropic, gemini, ollama, or fake")
	fs.StringVar(&c.model, "model", envStr("DROVER_MODEL", ""), "model id (required for openai/anthropic/gemini/ollama)")
	fs.StringVar(&c.system, "system", envStr("DROVER_SYSTEM", defaultSystem), "system prompt")
	fs.StringVar(&c.leashURL, "leash-url", envStr("DROVER_LEASH_URL", ""), "route model calls through a leash proxy for governance (empty talks to the provider directly)")
	fs.StringVar(&c.db, "db", envStr("DROVER_DB", "drover.db"), "durable store path (SQLite)")
	fs.StringVar(&c.apiKey, "api-key", "", "provider API key (or set OPENAI_API_KEY / ANTHROPIC_API_KEY)")
	fs.StringVar(&c.leashToken, "leash-token", envStr("LEASH_TOKEN", ""), "X-Leash-Token, if the proxy requires auth")
	fs.IntVar(&c.maxSteps, "max-steps", 0, "max plan/act iterations (0 = default)")
	return c
}

// runStore is a closable rerun.Store; both the SQLite and Postgres backends
// satisfy it.
type runStore interface {
	rerun.Store
	io.Closer
}

// build assembles the store, model client, and runner for a run id.
func (c *jobFlags) build(runID string) (runStore, *runner.Runner, error) {
	client, err := buildClient(c.provider, c.leashURL, provider.Config{
		APIKey:     pickKey(c.apiKey, c.provider),
		LeashToken: c.leashToken,
		RunID:      runID,
	})
	if err != nil {
		return nil, nil, err
	}
	store := sqlite.New(c.db)
	ag := agent.Agent{Model: c.model, System: c.system, Tools: tools.Default(), MaxSteps: c.maxSteps}
	r := runner.New(store, &agent.Loop{Agent: ag, Client: client, Tools: agent.NewToolset(ag.Tools...)},
		runner.WithStepLogger(func(_ string, l rerun.Log) {
			fmt.Fprintf(os.Stderr, "  · %s\n", l.Tag)
		}))
	return store, r, nil
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("drover run", flag.ContinueOnError)
	c := registerJobFlags(fs)
	goal := fs.String("goal", envStr("DROVER_GOAL", ""), "the task for the agent (required)")
	runID := fs.String("run", envStr("DROVER_RUN", ""), "run id (generated if empty); also the leash X-Loop-Id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *goal == "" {
		return fmt.Errorf("--goal is required")
	}
	if err := c.requireModel(); err != nil {
		return err
	}

	id := *runID
	if id == "" {
		id = genID()
	}
	store, r, err := c.build(id)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx := context.Background()
	dest := c.leashURL
	if dest == "" {
		dest = "direct"
	}
	fmt.Fprintf(os.Stderr, "drover: run %s  (%s/%s → %s)\n", id, c.provider, c.model, dest)
	if err := r.Start(ctx, id, *goal); err != nil {
		return err
	}
	st, err := r.Wait(ctx, id)
	if err != nil {
		return err
	}
	return report(ctx, st, r, id)
}

func resumeCmd(args []string) error {
	fs := flag.NewFlagSet("drover resume", flag.ContinueOnError)
	c := registerJobFlags(fs)
	runID := fs.String("run", envStr("DROVER_RUN", ""), "run id to resume (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return fmt.Errorf("--run is required")
	}
	if err := c.requireModel(); err != nil {
		return err
	}

	store, r, err := c.build(*runID)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx := context.Background()
	// Only wait if the run is still in flight; an already-finished run just has its
	// result read back (Recover would not relaunch it, so Wait would block).
	incomplete, err := isIncomplete(ctx, store, *runID)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "drover: resume %s\n", *runID)
	if err := r.Recover(ctx); err != nil {
		return err
	}
	st := rerun.Done
	if incomplete {
		if st, err = r.Wait(ctx, *runID); err != nil {
			return err
		}
	}
	return report(ctx, st, r, *runID)
}

// requireModel enforces that a real provider was given a model id; fake needs none.
func (c *jobFlags) requireModel() error {
	if c.provider != "fake" && c.model == "" {
		return fmt.Errorf("--model is required for provider %q", c.provider)
	}
	return nil
}

// buildClient selects a model.Client. When leashURL is set, calls route through the
// leash proxy (openai, gemini, and ollama all speak the OpenAI wire to it; anthropic
// speaks its own), and leash forwards to the real upstream. When leashURL is empty,
// drover talks to the provider's own endpoint directly, ungoverned.
func buildClient(prov, leashURL string, cfg provider.Config) (model.Client, error) {
	if prov == "fake" {
		return fakeClient(), nil
	}
	if leashURL != "" {
		cfg.BaseURL = leashURL
		if prov == "anthropic" {
			return provider.NewAnthropic(cfg), nil
		}
		return provider.NewOpenAI(cfg), nil
	}
	switch prov {
	case "openai":
		return provider.NewOpenAI(cfg), nil
	case "anthropic":
		return provider.NewAnthropic(cfg), nil
	case "gemini":
		return provider.NewGemini(cfg), nil
	case "ollama":
		return provider.NewOllama(cfg), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (want openai, anthropic, gemini, ollama, or fake)", prov)
	}
}

// fakeClient answers offline by echoing the latest user message, so `drover run
// --provider fake` exercises the durable loop with no key and no network.
func fakeClient() model.Client {
	return &provider.Fake{Reply: func(req model.Request) model.Response {
		last := ""
		for _, m := range req.Messages {
			if m.Role == model.User {
				last = m.Content
			}
		}
		return model.Response{Content: "drover(fake) received: " + last}
	}}
}

// pickKey prefers the flag, else the provider's conventional environment variable.
func pickKey(flagKey, prov string) string {
	if flagKey != "" {
		return flagKey
	}
	switch prov {
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "gemini":
		if k := os.Getenv("GEMINI_API_KEY"); k != "" {
			return k
		}
		return os.Getenv("GOOGLE_API_KEY")
	default: // ollama and fake need no key
		return ""
	}
}

// report prints a finished run's outcome.
func report(ctx context.Context, st rerun.Status, r *runner.Runner, id string) error {
	res, err := r.Result(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case res.Stopped != "":
		fmt.Printf("\nrun %s stopped after %d step(s): %s\n", id, res.Steps, res.Stopped)
	case res.Output != "":
		fmt.Printf("\nrun %s done in %d step(s):\n\n%s\n", id, res.Steps, res.Output)
	default:
		fmt.Printf("\nrun %s finished (%s) with no output\n", id, st)
	}
	return nil
}

// isIncomplete reports whether runID is still pending or running in store.
func isIncomplete(ctx context.Context, store rerun.Store, runID string) (bool, error) {
	runs, err := store.Incomplete(ctx)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.ID == runID {
			return true, nil
		}
	}
	return false, nil
}

// envStr returns the environment variable value, or def when unset.
func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// genID mints a short random run id.
func genID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "run-unknown"
	}
	return "run-" + hex.EncodeToString(b[:])
}

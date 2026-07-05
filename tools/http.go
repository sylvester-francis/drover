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

// Package tools provides drover's built-in agent tools. Each is idempotent, as
// rerun's at-least-once execution requires: a tool may run again on recovery, so
// re-running it must be harmless.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/model"
)

// maxBody bounds how much of a fetched page is returned to the model.
const maxBody = 8 << 10

// HTTPGet returns a tool that fetches a URL with an HTTP GET. A GET has no side
// effect, so re-running it after a crash is harmless. That idempotence is what
// rerun asks of every tool.
func HTTPGet() agent.Tool {
	return agent.FuncTool{
		Def: model.ToolSchema{
			Name:        "http_get",
			Description: "Fetch the text contents of a URL via HTTP GET. Returns the status and up to 8 KB of the body.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"the URL to fetch"}},"required":["url"]}`),
		},
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			if a.URL == "" {
				return "error: missing required argument \"url\"", nil
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
			if err != nil {
				return "", err
			}
			resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
			if err != nil {
				return fmt.Sprintf("error: %v", err), nil
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
			return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(b)), nil
		},
	}
}

// Default is drover's built-in toolset for the CLI's default agent.
func Default() []agent.Tool {
	return []agent.Tool{HTTPGet()}
}

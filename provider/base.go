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

// Package provider implements model.Client against real chat APIs, each pointed
// at a leash proxy URL so every call is governed. The shared base handles the
// transport and the one piece of logic that is provider-independent: reading
// leash's answer. A 429 with Retry-After is a rate-limit (wait and retry); a 429
// without one is a terminal boundary stop; a 5xx is transient; a 2xx body is the
// model's reply for the provider to decode.
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sylvester-francis/drover/model"
)

// Config configures a leash-fronted HTTP model client.
type Config struct {
	// BaseURL overrides the endpoint. Set it to a leash proxy URL to route calls
	// through the governor; leave it empty to talk to the provider's own endpoint
	// directly (ungoverned). Each provider constructor supplies a default.
	BaseURL string
	// APIKey is the provider credential (a Bearer token for OpenAI and Gemini, an
	// x-api-key for Anthropic). Empty for a local Ollama, or when a leash proxy
	// holds the key.
	APIKey string
	// LeashToken is the optional X-Leash-Token when the proxy requires auth.
	LeashToken string
	// RunID is sent as X-Loop-Id, tying every call to one leash run and budget.
	RunID string
	// MaxTokens caps a completion's output length. Anthropic requires it (drover
	// defaults to 4096 when unset); OpenAI leaves it to the model when zero.
	MaxTokens int
	// HTTP is the client to use; nil builds one with no overall timeout (a long
	// completion must not be cut off; cancellation rides the request context).
	HTTP *http.Client
}

// base carries the transport and leash wiring shared by every provider.
type base struct {
	baseURL    string
	leashToken string
	runID      string
	client     *http.Client
}

func newBase(cfg Config) base {
	c := cfg.HTTP
	if c == nil {
		c = &http.Client{}
	}
	return base{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		leashToken: cfg.LeashToken,
		runID:      cfg.RunID,
		client:     c,
	}
}

// sendResult is the outcome of one governed request: either a 2xx body for the
// provider to decode, or a governor verdict (a terminal Stopped, or a RetryAfter
// to wait out) already shaped as a model.Response.
type sendResult struct {
	body     []byte
	governor *model.Response
}

// send applies the leash headers, performs req, and classifies leash's response.
// A network failure or a 5xx is returned as an error (the loop retries on its
// presence); a 429 becomes a governor verdict; a 2xx returns its body.
func (b base) send(req *http.Request) (sendResult, error) {
	req.Header.Set("X-Loop-Id", b.runID)
	if b.leashToken != "" {
		req.Header.Set("X-Leash-Token", b.leashToken)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return sendResult{}, fmt.Errorf("provider: request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sendResult{}, fmt.Errorf("provider: read response: %w", err)
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return sendResult{body: body}, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		if d := retryAfter(resp.Header.Get("Retry-After")); d > 0 {
			// leash rate-limit backpressure: the run is still alive, wait and retry.
			return sendResult{governor: &model.Response{RetryAfter: d}}, nil
		}
		// A terminal boundary: the run is stopped for good.
		return sendResult{governor: &model.Response{Stopped: boundaryReason(body)}}, nil
	case resp.StatusCode >= 500:
		return sendResult{}, fmt.Errorf("provider: upstream %d: %s", resp.StatusCode, snippet(body))
	default:
		return sendResult{}, fmt.Errorf("provider: request rejected %d: %s", resp.StatusCode, snippet(body))
	}
}

// retryAfter parses leash's Retry-After header (integer seconds) into a duration;
// 0 when absent or unparseable.
func retryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// boundaryReason pulls leash's machine-readable stop reason from a 429 body,
// defaulting to "stopped" when the body is not a leash boundary envelope.
func boundaryReason(body []byte) string {
	var lb struct {
		Error struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &lb); err == nil && lb.Error.Reason != "" {
		return lb.Error.Reason
	}
	return "stopped"
}

// snippet bounds an error body so a failure message stays readable.
func snippet(body []byte) string {
	const max = 200
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

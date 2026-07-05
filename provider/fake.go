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

package provider

import (
	"context"
	"sync"

	"github.com/sylvester-francis/drover/model"
)

// Fake is an offline model.Client that returns canned responses, so drover can
// run without an API key or a live model: for examples, the quickstart, and CI.
// It never touches the network, so it is not governed by leash; point a real
// provider at a leash URL to exercise governance.
type Fake struct {
	// Reply, if set, computes a response from each request (full control).
	Reply func(req model.Request) model.Response
	// Script, used when Reply is nil, is returned one entry per call; the last
	// entry repeats once exhausted.
	Script []model.Response

	mu    sync.Mutex
	calls int
}

// Complete returns the next canned response.
func (f *Fake) Complete(_ context.Context, req model.Request) (model.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Reply != nil {
		return f.Reply(req), nil
	}
	if len(f.Script) == 0 {
		return model.Response{Content: "ok"}, nil
	}
	i := f.calls
	f.calls++
	if i >= len(f.Script) {
		return f.Script[len(f.Script)-1], nil
	}
	return f.Script[i], nil
}

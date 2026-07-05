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

// Command fakeupstream is a minimal fake OpenAI Chat Completions endpoint for the
// end-to-end demo. It ignores the request and returns a tool call plus a usage
// block on every turn. That keeps a drover agent looping: the tool ("noop") is
// one the demo agent does not have, so drover folds it back as an observation and
// asks again, giving leash a real multi-call loop to meter and eventually stop on
// its budget. It exists only for the demo and is not a general-purpose fake.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// reply is a fixed OpenAI chat.completion: one tool call for "noop" (a tool the
// demo agent does not register) plus a usage block the demo price table prices at
// about two cents per call.
const reply = `{"id":"chatcmpl-demo","object":"chat.completion","model":"demo-model",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":null,` +
	`"tool_calls":[{"id":"call_noop","type":"function","function":{"name":"noop","arguments":"{}"}}]},` +
	`"finish_reason":"tool_calls"}],` +
	`"usage":{"prompt_tokens":1000,"completion_tokens":1000,"total_tokens":2000}}`

func main() {
	addr := os.Getenv("FAKE_UPSTREAM_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18080"
	}
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, reply)
	})
	log.Printf("fake upstream listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

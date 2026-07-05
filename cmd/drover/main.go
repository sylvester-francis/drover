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

// Command drover runs durable, multi-step agent jobs on rerun, with every model
// call governed by the leash proxy. This is a scaffold; the durable runner is
// being built on rerun's execution engine (see DESIGN.md).
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Printf("drover %s\n", version)
		return
	}
	fmt.Fprintln(os.Stderr, "drover: durable agent runner (scaffold). See DESIGN.md for the plan.")
	os.Exit(2)
}

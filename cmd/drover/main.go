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
// call governed by the leash proxy. A job is a rerun workflow: a crash resumes at
// the step that was in flight rather than restarting the whole job.
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		fail(runCmd(os.Args[2:]))
	case "resume":
		fail(resumeCmd(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Printf("drover %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "drover: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

// fail prints err and exits non-zero; flag.ErrHelp (already printed) exits clean.
func fail(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "drover:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprint(os.Stderr, `drover: durable, budgeted agent runner

usage:
  drover run     [flags]   start a new agent job and run it to completion
  drover resume  [flags]   resume jobs a restart left in flight
  drover version           print the version

Run "drover run -h" or "drover resume -h" for flags.
`)
}

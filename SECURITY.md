# Security Policy

## Supported versions

drover is `v0.x` and unstable. Security fixes land on the latest minor release.

## Reporting a vulnerability

Please report security issues privately through GitHub's "Report a vulnerability"
button under the repository's Security tab, not a public issue. You will get an
acknowledgement within a few days.

## Scope

drover is an orchestrator. It holds no credentials of its own and stores only what a
run journals (the conversation and step results) in its rerun `Store`. It routes
model calls through a leash proxy and forwards whatever credential the caller
provides. Credential handling and spend governance at the proxy are leash's domain;
durability and the journal are rerun's.

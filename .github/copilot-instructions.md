<!-- Copyright (c) Microsoft Corporation. -->
<!-- Licensed under the MIT License. -->

# Copilot Instructions for Mantle

## Building

Mantle builds inside a Docker container. Do **not** run `go build` directly on the host.

```bash
cd /workspace/mantle
docker build -t mantle .
```

This builds all binaries (`cork`, `gangue`, `kola`, `ore`, `plume`) for both amd64 and arm64.

## Running kola tests

Run tests from the `azure-container-linux` repo root using `run_local_tests.sh`. For ACL:

```bash
PACKAGE_SOURCE_MODE=RPM ./run_local_tests.sh amd64 2 <test-pattern>
```

For example:

```bash
PACKAGE_SOURCE_MODE=RPM ./run_local_tests.sh amd64 2 acl.disk.raid0.data
```

## Project structure

- `kola/` — kola test harness and test registration
- `kola/tests/` — test implementations organized by category (e.g., `misc/`, `flannel/`, `etcd/`)
- `platform/` — platform abstractions (qemu, cloud providers)
- `cmd/` — CLI entry points for mantle tools

## Test naming conventions

- Tests use a distro prefix: `cl.` for Container Linux, `acl.` for Azure Container Linux
- Tests specify allowed distros via `Distros: []string{"acl", "cl"}` in registration
- When a test needs different configs per distro, register separate `cl.` and `acl.` variants

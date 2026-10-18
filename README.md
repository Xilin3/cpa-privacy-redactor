# CPA Privacy Redactor

**English** | [简体中文](README_CN.md)

`privacy-redactor` is a local request normalization plugin for CLIProxyAPI (CPA), currently at version `0.1.0`. It runs inside the CPA process and redacts selected fields in request bodies sent to Claude. It also works with native Claude Code requests and does not depend on Cloak.

The plugin uses only the Go standard library. It does not read credentials, access files or the network, or log request content. It has been validated with CPA `8.0.32` (`911f49cb`), using C ABI `1` and JSON schema `6`.

## Redaction Scope

| Data | Handling |
| --- | --- |
| Regional OS editions in generated environment context | For example, simplify `Windows 11 Home China` to `Windows 11` |
| Working directory, home directory, username, and hostname in generated environment context | Replace with generic values such as `.`, `~`, `user`, and `localhost` |
| Timezone, locale, country, region, and IP in generated environment context | Redact the values of recognized fields |
| metadata, JSON-encoded user_id, and diagnostics | Recursively remove recognized location, host, and path fields |
| user_location in native WebSearch declarations | Remove the optional parameter while preserving the search query and domain filters |

The plugin preserves the platform, shell, date, user text, code, ordinary tool output, tool arguments, and thinking signatures. The protocol structure of device, account, and session IDs remains intact.

This plugin does not provide complete anonymity. User content and tool results may still contain personal information, and account and session identifiers can still link requests. The plugin only processes the request body fields described above; it does not modify HTTP headers or change the network egress IP. Requests are not protected by these rules when the plugin is disabled, fails to load, or is bypassed.

## Installation

Download `privacy-redactor.so` and `SHA256SUMS` from the repository's `v0.1.0` release. The prebuilt library currently supports **Linux ARM64** only. For other platforms, build from source and confirm that the CPA host supports native plugins and the required ABI.

```sh
sha256sum --ignore-missing -c SHA256SUMS
mkdir -p /path/to/cpa/plugins
cp privacy-redactor.so /path/to/cpa/plugins/privacy-redactor.so
```

Back up your CPA configuration, then merge the settings below into it while preserving existing plugin settings. Set `dir` to your actual plugin directory, and keep only one `plugins` section in the configuration file.

```yaml
plugins:
  enabled: true
  dir: /path/to/cpa/plugins
  configs:
    privacy-redactor:
      enabled: true
      priority: -10000
```

Load the plugin using the host's plugin loading procedure. If the first load does not take effect, restart CPA during a maintenance window. The management interface should display `Claude privacy redactor`.

The status endpoint is `GET /v0/management/privacy-redactor/status`. It requires CPA management credentials and returns processing counters only, without request content. To disable the plugin, turn it off in the management interface, or set `plugins.configs.privacy-redactor.enabled` to `false` and have the host apply the configuration.

## Building

You need Go `1.23` or newer and a C compiler for the target platform. To build for the current architecture on Linux:

```sh
mkdir -p dist
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -ldflags='-s -w' -o dist/privacy-redactor.so .
```

To cross-compile for ARM64 on Linux AMD64, install `aarch64-linux-gnu-gcc`:

```sh
mkdir -p dist/linux-arm64
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc \
  go build -trimpath -buildmode=c-shared -ldflags='-s -w' \
  -o dist/linux-arm64/privacy-redactor.so .
```

`build_linux.sh` preserves the isolated build procedure used for the initial deployment. Pass a Linux AMD64 Go toolchain archive and the project directory, with `gcc` and the ARM64 cross-compiler already installed. The script runs race checks and vet, then builds the ARM64 shared library:

```sh
sh ./build_linux.sh /path/to/go.linux-amd64.tar.gz "$(pwd)"
```

## Validation

```sh
go test ./...
CGO_ENABLED=1 go test -race ./...
go vet ./...
python3 abi_smoke.py ./dist/privacy-redactor.so
```

Run the C ABI check on a machine that can load the shared library for its target architecture. The initial deployment passed unit tests, race checks, vet, concurrent ARM64 C ABI calls, isolated CPA calls, and real Claude Code file reading and WebSearch checks. This repository does not include production credentials, real requests, server details, or deployment logs.

The ARM64 binary in the release is identical to the initially deployed file. Its SHA-256 is:

```text
d556a0081801af3a219ac85a4ec8dd6257e66373eb65f958a86cc58a838d5abb  privacy-redactor.so
```

Revalidate after upgrading the host or plugin ABI. Different toolchains may produce different binary hashes.

## Source Files

- `normalizer.go`: Request body redaction and status counters.
- `bridge.go`: Language-neutral C ABI.
- `normalizer_test.go`: Protocol preservation, redaction scope, and concurrency tests.
- `abi_smoke.py`: Synthetic request checks against the shared library using ctypes.
- `config.example.yaml`: Minimal plugin configuration.

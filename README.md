# Unikraft CLI

[![](https://pkg.go.dev/badge/unikraft.com/cli.svg)](https://pkg.go.dev/unikraft.com/cli)
![](https://img.shields.io/static/v1?label=license&message=BSD-3&color=%23385177)
[![](https://img.shields.io/discord/762976922531528725.svg?label=discord&logo=discord&logoColor=ffffff&color=7389D8&labelColor=6A7EC2)](https://unikraft.com/discord)
[![Go Report Card](https://goreportcard.com/badge/unikraft.com/cli)](https://goreportcard.com/report/unikraft.com/cli)
![Latest release](https://img.shields.io/github/v/release/unikraft/cli)


The `unikraft` CLI tool is a is designed for deploying highly performant microVMs.

> [!WARNING]
>
> This project is currently in **alpha**.
> The CLI may including breaking changes between releases.

> [!TIP]
>
> Your voice helps shape this tool!
> We are keen to hear your feedback.
> If you are interested and/or have tried any of Unikraft's tools or services, **please consider taking our [developer survey](https://unikraft.link/devsurvey)**!

## Overview

The Unikraft CLI is designed from to provide a focused, lightweight, and principled open-source developer experience for deploying ultra-lightweight and highly performance applications based on the [Unikraft Library Operating System](https://github.com/unikraft/unikraft) and other unikernel systems.

### Use cases

* Agentic sandboxes
* Build systems
* Web and production servers
* ETL and data scraping
* Webhooks
* Game servers
* 


## Installation

### Quick Install (Recommended)

```bash
curl -sSfL https://unikraft.sh/cli.sh | bash
```

The installer automatically detects your platform (Linux/macOS, amd64/arm64) and installs the latest stable version to `~/.local/bin`.

#### Installation Options

```bash
# Install a specific version
curl -sSfL https://unikraft.sh/cli.sh | bash -s -- --version v0.2.0

# Install to a custom directory
curl -sSfL https://unikraft.sh/cli.sh | bash -s -- --bin-dir /usr/local/bin

# Install from staging channel
curl -sSfL https://unikraft.sh/cli.sh | bash -s -- --channel staging
```

### Debian/Ubuntu

```bash
# Download and add the GPG key
sudo install -d -m 0755 /etc/apt/keyrings

sudo curl -fsSL \
  -o /etc/apt/keyrings/unikraft-cli.gpg \
  https://pkg.unikraft.com/debian/cli-apt/keys/cli-apt.gpg

echo "deb [signed-by=/etc/apt/keyrings/unikraft-cli.gpg] \
  https://pkg.unikraft.com/debian/cli-apt/ $(. /etc/os-release && echo "${VERSION_CODENAME}") stable staging" \
  | sudo tee /etc/apt/sources.list.d/unikraft-cli.list > /dev/null

# Update and install
sudo apt-get update
sudo apt-get install unikraft-cli
```

### From Source

Requires Go 1.25+:

```bash
go install unikraft.com/cli/cmd/unikraft@latest
```

Or clone and build:

```bash
git clone https://github.com/unikraft/cli.git && cd cli && make cli
```
Binary available at `./dist/unikraft`.
See [`HACKING.md`](./HACKING.md) for details.

## Quick Start

1. Login to [Unikraft Cloud](https://console.unikraft.com/auth/signin):

   ```bash
   unikraft login
   ```

   _(This will open your browser or provide you with a auth link.)_

2. Deploy your first instance in Frankfurt:

   ```bash
   unikraft run --metro=fra nginx:latest
   ```

3. List your instances:

   ```bash
   unikraft instances list
   ```

4. View instance Logs:

   ```bash
   unikraft instances logs my-instance
   ```

## Usage

```
unikraft <command> [flags]
```

### Core Commands

| Command        | Description                          |
| -------------- | ------------------------------------ |
| `run`          | Run an image as an instance          |
| `instances`    | Manage Unikraft Cloud instances      |
| `images`       | Manage Unikraft Cloud images         |
| `volumes`      | Manage Unikraft Cloud volumes        |
| `services`     | Manage Unikraft Cloud services       |
| `certificates` | Manage Unikraft Cloud certificates   |
| `metros`       | List available Unikraft Cloud metros |

### Account Commands

| Command   | Description                    |
| --------- | ------------------------------ |
| `login`   | Login to Unikraft Cloud        |
| `logout`  | Logout from Unikraft Cloud     |
| `profile` | Manage Unikraft Cloud profiles |

### Global Flags

```
--config file       Set the configuration file
--emojis            Enable or disable emojis in output (default: true)
--log-level level   Set the logging level (default: info)
--log-type type     Set the log type (default: text)
--profile name      Set the current profile (default: default)
--version           Print version information
```

## Examples

### Deploy with HTTPS

```bash
unikraft run --metro=sfo -p 443:8080/http+tls nginx:latest
```

### Deploy with HTTP to HTTPS Redirect

```bash
unikraft run --metro=sfo \
  -p 443:8080/http+tls \
  -p 80:443/http+redirect \
  nginx:latest
```

### Deploy with Environment Variables

```bash
unikraft run --metro=was \
  -e DATABASE_URL=postgres://... \
  -e API_KEY=secret \
  my-app:latest
```

### Deploy with Attached Volume

```bash
unikraft run --metro=sin \
  -v my-volume:/data \
  my-app:latest
```

### Deploy with Scale-to-Zero

```bash
unikraft run --metro=fra \
  -p 443:8080/http+tls \
  --scale-to-zero policy=on \
  nginx:latest
```

### Preview Deployment (Dry Run)

```bash
unikraft run --metro=dal --dry-run nginx:latest
```

### Deploy and Follow Logs

```bash
unikraft run --metro=fra --follow nginx:latest
```

## Configuration

The Unikraft CLI stores configuration in `~/.config/unikraft/config.yaml`.
You can manage multiple profiles for different environments or accounts:

```bash
# List profiles
unikraft profile list

# Create a new profile
unikraft profile create staging

# Switch profiles
unikraft profile use staging

# Use a profile for a single command
unikraft --profile=staging instances list
```

## Shell Completions

Enable tab completions for your shell:

```bash
# Bash
unikraft completion bash > /etc/bash_completion.d/unikraft

# Zsh
unikraft completion zsh > "${fpath[1]}/_unikraft"

# Fish
unikraft completion fish > ~/.config/fish/completions/unikraft.fish
```


## Project goals and roadmap

This new CLI has been developed from the ground up and accommodates nearly 6 years of feedback we've had from the community whilst building CLIs for Unikraft ([`pykraft`](https://github.com/unikraft/pykraft) and [`kraft`](https://github.com/unikraft/kraftkit)).
In this latest iteration, many of those ideas and suggested workflows have been built into to the CLI from day one.
In the end, it made more sense to build a new CLI instead of changing `kraft` in order to 1). preserve the many existing workflows which rely on its features, syntax and command structure whilst 2). being able to completely re-think how we approach building, running and deploying microVMs.

The ultimate vision of the `unikraft` CLI is to supersede `kraft` as the primary CLI tool for the Unikraft Unikernel Development Kit, library Operating System and the Unikraft Cloud Platform offering.
We aim to support fully local builds and runs of Unikraft in a way that does not feel intrusive to our cloud offering, and vice-versa and accommodate users with varying use cases.
To begin with, however, the CLI only supports the Cloud offering of Unikraft until we have finalized reworking our VMM packages and build flows.

## Development

```bash
# Build the CLI
make cli

# Run tests
make test

# Run linter
make lint
```

For detailed development instructions, architecture overview, and contribution workflows, see [`HACKING.md`](./HACKING.md).


## Contributing

Contributions are welcome!
Please see the [`CONTRIBUTING.md`](./CONTRIBUTING.md) file for development practices and architecture overview.


## License

BSD-3-Clause. See [`LICENSE.md`](./LICENSE.md) for details.


## Links

- **Documentation**: https://unikraft.com/docs
- **Unikraft Cloud**: https://unikraft.cloud
- **Issues**: https://github.com/unikraft/cli/issues
- **KraftKit** (build tool): https://github.com/unikraft/kraftkit

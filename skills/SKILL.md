---
name: unikraft-cli
description: Manage Unikraft Cloud resources including instances, images, volumes, and services. Use when the user needs to deploy applications, manage cloud infrastructure, or interact with Unikraft Cloud.
allowed-tools: Bash(unikraft*)
---

# Unikraft Cloud CLI

The `unikraft` command provides a unified interface for managing resources on Unikraft Cloud. It enables deploying instances, managing persistent storage, and configuring services.

## Installation

```bash
curl -sL https://get.unikraft.io | sh
```

## Quick Start

```bash
# Start by logging into Unikraft Cloud
unikraft login --no-browser

# Run a simple NGINX instance
unikraft run --metro=fra --scale-to-zero policy=on,stateful=true,cooldown-time=10 -p 8080:80 nginx:latest

# List running instances
unikraft instances list
```

## Core Resources

- **Instances**: MicroVMs on Unikraft Cloud run from `Dockerfile`'s.
- **Images**: Unikernel images stored in the registry.
- **Volumes**: Persistent storage that can be attached to instances.
- **Services**: Networking abstractions for exposing instances.
- **Certificates**: TLS certificates for secure connections.
- **Metros**: Geographic locations where resources are deployed.

## Commands

### Deployment (`unikraft run`)

The `run` command is the primary entry point for deploying applications.

```bash
unikraft run [flags] <image> [<args>...]
```

**Common Flags:**

- `--metro <code`>: Metro to deploy in (e.g., `fra`, `dal`, `sin`).
- `-p, --publish <port>`: Publish a port (e.g., `443:8080/http+tls`).
- `-e, --env <key=val>`: Set environment variables.
- `-v, --volume <vol>`: Attach a volume.
- `-m, --memory <size>`: Set memory size (e.g., `512MiB`).
- `--scale-to-zero`: Enable scale-to-zero policies.
- `--dry-run`: Preview creation without deploying.

### Instance Management

```bash
unikraft instances list                 # List all instances
unikraft instances get <name|uuid>      # Inspect instance details
unikraft instances logs <name|uuid>     # View instance logs
unikraft instances stop <name|uuid>     # Stop an instance
unikraft instances start <name|uuid>    # Start a stopped instance
unikraft instances rm <name|uuid>       # Remove an instance
```

### Volume Management

```bash
unikraft volumes list                   # List volumes
unikraft volumes create <name> --size 1G # Create a volume
unikraft volumes rm <name|uuid>         # Delete a volume
```

### Service Management

```bash
unikraft services list                  # List services
unikraft services create <name>         # Create a service (usually done via run)
unikraft services rm <name|uuid>        # Delete a service
```

### Authentication

```bash
unikraft login                          # interactive login
unikraft logout                         # logout
```

## Global Options

| Option | Description |
|--------|-------------|
| `--metro <code`> | Target metro for the command. |
| `--config <file>` | Path to configuration file. |
| `--profile <name>` | Use a specific profile. |
| `--log-level <level>` | Set logging verbosity (info, debug, trace). |
| `--json` | Output (log-type) as JSON. |

## Examples

### Deploy with HTTPS and Redirect

```bash
unikraft run \
  --metro=fra \
  -p 443:8080/http+tls \
  -p 80:443/http+redirect \
  nginx:latest
```

### Deploy with Persistent Volume

```bash
# Attach existing volume
unikraft run \
  --metro=sin \
  -v my-data:/data \
  my-app:latest
```

### Auto-scaling deployment

```bash
unikraft run \
  --metro=fra \
  --scale-to-zero policy=on,cooldown-time=300 \
  my-server:latest
```

### Debugging

```bash
# Follow logs immediately after run
unikraft run --metro=fra --follow my-app:latest

# Get detailed info
unikraft instances inspect my-instance-name
```

## Troubleshooting

**Authentication Issues?**
Run `unikraft login` to refresh credentials.

**Deployment Failures?**
Use `--dry-run` to validate configuration.
Check `unikraft instances logs <id>` for application startup errors.

**Resource Not Found?**
Ensure you are targeting the correct metro with `--metro`. Resources are often metro-specific.

## Tasks (Developer)

For developers working on this CLI repository:

- `task cli`: Build the binary.
- `task run`: Run specific dev tasks (see Taskfile.yml).
- `task lint`: Run linters.
- `task test`: Run unit tests.

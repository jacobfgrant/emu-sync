# emu-sync

Sync ROMs and BIOS files from an S3-compatible bucket to one or more devices.

## What it does

- **Upload** from your main machine to any S3-compatible bucket (Backblaze B2, AWS S3, DigitalOcean Spaces)
- **Sync** to multiple devices — only downloads new or changed files (manifest-based delta sync)
- **Delete propagation** — optionally removes local files that were deleted from the bucket
- **Parallel downloads** — configurable worker count for faster syncs
- **Setup tokens** — generate a single token that configures a recipient's device in one command
- **Interactive game selection** — choose which systems and individual games to sync
- **Automatic scheduling** — systemd timer (Linux/SteamOS) or launchd agent (macOS) that syncs every 6 hours
- **One-liner install** — download, configure, and schedule with a single command
- **Integrity verification** — re-hash local files to detect corruption or accidental deletion

Designed for syncing emulation libraries to Steam Decks, but works anywhere you need one-way S3-to-local sync.

## Quick start

### Admin (upload machine)

```sh
# Configure credentials and bucket
emu-sync init

# Upload your ROMs and BIOS files
emu-sync upload --source ~/Emulation --verbose

# Generate a token for recipients
emu-sync generate-token
```

### Recipient (Steam Deck or other device)

**One-liner** — install, configure, and schedule automatic syncing:

```sh
curl -sSL https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh | bash -s -- <token>
```

**Step by step:**

```sh
# Install the binary
curl -sSL https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh | bash

# Configure from the token the admin sent you
emu-sync setup

# Choose which systems/games to sync (optional — syncs everything by default)
emu-sync choose

# Sync files
emu-sync sync --verbose

# Set up automatic syncing every 6 hours
emu-sync install
```

## Commands

| Command | Description |
|---------|-------------|
| `init` | Interactive configuration wizard |
| `setup [token]` | Configure from a setup token (prompts if no token given) |
| `upload` | Upload ROMs/BIOS to the bucket |
| `sync` | Download new/changed files from the bucket |
| `choose` | Interactively select which systems and games to sync |
| `status` | Show what would change on next sync |
| `verify` | Check local files against the manifest |
| `generate-token` | Interactively create a setup token for recipients |
| `install` | Install automatic sync schedule (Linux systemd / macOS launchd) |
| `uninstall` | Remove automatic sync schedule |

### Common flags

| Flag | Commands | Description |
|------|----------|-------------|
| `--config` | all | Config file path (default `~/.config/emu-sync/config.toml`) |
| `--verbose` | all | Enable debug logging |
| `--source` | `upload` | Source directory (defaults to config `emulation_path`) |
| `--dry-run` | `upload`, `sync` | Show what would happen without making changes |
| `--no-delete` | `sync` | Skip deleting files removed from bucket |
| `--workers N` | `sync` | Parallel download workers (default 1) |
| `--manifest-only` | `upload` | Regenerate manifest without uploading files |
| `--progress-json` | `sync` | Emit JSON progress events to stdout |

## Storage provider setup

emu-sync works with any S3-compatible storage. Always scope credentials to a single bucket with the minimum permissions needed.

### Backblaze B2

Create an application key scoped to your bucket with these capabilities:

| Capability | Required for |
|------------|-------------|
| `listFiles` | `sync`, `status`, credential verification |
| `readFiles` | `sync`, `status` |
| `writeFiles` | `upload` |
| `deleteFiles` | `upload` (deleting removed files from bucket) |

**Sync-only key** (recipients): `listFiles`, `readFiles`

**Full access key** (admin): `listFiles`, `readFiles`, `writeFiles`, `deleteFiles`

The `init` wizard auto-detects the region from B2 endpoint URLs and auto-prefixes `https://` if omitted.

### AWS S3

Create an IAM user or role with a policy scoped to your bucket:

**Sync-only** (recipients):
```
s3:ListBucket, s3:GetObject
```

**Full access** (admin):
```
s3:ListBucket, s3:GetObject, s3:PutObject, s3:DeleteObject
```

Set the endpoint URL to your region's S3 endpoint (e.g., `https://s3.us-east-1.amazonaws.com`), or leave it blank to use the AWS SDK default.

### Other S3-compatible providers

emu-sync uses the standard S3 API (`ListObjectsV2`, `GetObject`, `PutObject`, `DeleteObject`). Any provider that supports these operations will work — configure the endpoint URL, region, and credentials as your provider specifies.

## Sample config

Config file location: `~/.config/emu-sync/config.toml`

```toml
[storage]
endpoint_url = "https://s3.us-west-002.backblazeb2.com"
bucket = "my-roms-bucket"
key_id = "your-key-id"
secret_key = "your-secret-key"
region = "us-west-002"
# prefix = "Emulation"  # optional: store under a path prefix in the bucket

[sync]
emulation_path = "/run/media/mmcblk0p1/Emulation"
sync_dirs = ["roms", "bios"]
# sync_exclude = ["roms/ps2/Some Huge Game.iso"]  # optional: exclude specific files
delete = true
workers = 4
```

Relative paths in `emulation_path` resolve against the user's home directory (e.g., `Emulation` becomes `~/Emulation`). Absolute paths and `~/` paths work as expected.

## How it works

emu-sync uses a **manifest-based delta sync** approach:

1. **Upload** walks your source directories, hashes every file (MD5), and compares against the remote manifest stored in the bucket. Only new or changed files are uploaded. The updated manifest is written to the bucket.

2. **Sync** downloads the remote manifest and compares it against the local manifest on the device. Files that are new or have a different hash are downloaded. Files present locally but absent from the remote manifest are optionally deleted. Files that exist in the manifest but are missing from disk are automatically re-downloaded.

This means syncs are fast even for large libraries — only actual changes transfer over the network.

## Building from source

```sh
git clone https://github.com/jacobfgrant/emu-sync.git
cd emu-sync
go build .
```

Requires Go 1.25+.

## License

Apache 2.0

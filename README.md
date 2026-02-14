# emu-sync

Sync ROMs and BIOS files from an S3-compatible bucket to one or more devices.

## What it does

- **Upload** from your main machine to any S3-compatible bucket (Backblaze B2, AWS S3, DigitalOcean Spaces)
- **Sync** to multiple devices — only downloads new or changed files (manifest-based delta sync)
- **Delete propagation** — optionally removes local files that were deleted from the bucket
- **Parallel downloads** — configurable worker count for faster syncs
- **Setup tokens** — generate a single token that configures a recipient's device in one command
- **Systemd integration** — auto-install a timer that syncs every 6 hours (Linux/SteamOS)
- **Integrity verification** — re-hash local files to detect corruption

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

```sh
# Install the binary
curl -sSL https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh | bash

# Configure from the token the admin sent you
emu-sync setup <token>

# Sync files
emu-sync sync --verbose

# Set up automatic syncing (Linux only — installs systemd timer + desktop shortcut)
emu-sync install
```

## Commands

| Command | Description |
|---------|-------------|
| `init` | Interactive configuration wizard |
| `setup <token>` | Configure from a setup token |
| `upload` | Upload ROMs/BIOS to the bucket |
| `sync` | Download new/changed files from the bucket |
| `status` | Show what would change on next sync |
| `verify` | Check local files against the manifest |
| `generate-token` | Create a setup token for recipients |
| `install` | Install systemd timer and desktop shortcut (Linux) |

### Common flags

| Flag | Commands | Description |
|------|----------|-------------|
| `--config` | all | Config file path (default `~/.config/emu-sync/config.toml`) |
| `--verbose` | all | Enable debug logging |
| `--source` | `upload` | Source directory (defaults to config `emulation_path`) |
| `--dry-run` | `upload`, `sync` | Show what would happen without making changes |
| `--no-delete` | `sync` | Skip deleting files removed from bucket |
| `--workers N` | `sync` | Parallel download workers (default 1) |
| `--progress-json` | `sync` | Emit JSON progress events to stdout |

## Sample config

Config file location: `~/.config/emu-sync/config.toml`

```toml
[storage]
endpoint_url = "https://s3.us-west-004.backblazeb2.com"
bucket = "my-roms-bucket"
key_id = "your-key-id"
secret_key = "your-secret-key"
region = "us-west-004"

[sync]
emulation_path = "/run/media/mmcblk0p1/Emulation"
sync_dirs = ["roms", "bios"]
delete = true
workers = 4
```

## How it works

emu-sync uses a **manifest-based delta sync** approach:

1. **Upload** walks your source directories, hashes every file (SHA-256), and compares against the remote manifest stored in the bucket. Only new or changed files are uploaded. The updated manifest is written to the bucket.

2. **Sync** downloads the remote manifest and compares it against the local manifest on the device. Files that are new or have a different hash are downloaded. Files present locally but absent from the remote manifest are optionally deleted.

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

# Installing memoryweb on Linux

memoryweb ships pre-built Linux binaries for x86-64 and ARM64. The recommended installation method is Homebrew.

---

## Option 1 — Homebrew (recommended)

[Homebrew on Linux](https://docs.brew.sh/Homebrew-on-Linux) works on x86-64 and ARM64 and handles architecture detection, PATH configuration, and updates automatically. If Homebrew is not yet installed, see the [Homebrew on Linux installation guide](https://docs.brew.sh/Homebrew-on-Linux) to install it first.

Once Homebrew is installed, tap the memoryweb formula and install:

```bash
brew tap corbym/memoryweb
brew install memoryweb
```

The binary is added to your PATH and the hook scripts are installed to `$(brew --prefix)/share/memoryweb/hooks/`.

Once the install finishes, skip to [Step 4 — Install Ollama](#step-4--install-ollama-for-semantic-search).

---

## Option 2 — Manual installation

If you are not using Homebrew, follow the steps below.

---

## Step 1 — Find out which architecture you have

Run the following command in a terminal:

```bash
uname -m
```

| Output | Binary to download |
|--------|--------------------|
| `x86_64` | `linux_amd64` |
| `aarch64` | `linux_arm64` |

---

## Step 2 — Download the binary

Go to the [latest releases page](https://github.com/corbym/memoryweb/releases/latest) and copy the download URL for your architecture:

| Architecture | File to download |
|--------------|-----------------|
| x86-64 | `memoryweb_vX.Y.Z_linux_amd64.tar.gz` |
| ARM64 | `memoryweb_vX.Y.Z_linux_arm64.tar.gz` |

Replace `X.Y.Z` with the version number shown on the releases page (e.g. `1.4.0`).

You can download directly from the terminal:

```bash
# x86-64
curl -L -o /tmp/memoryweb.tar.gz \
  https://github.com/corbym/memoryweb/releases/download/v1.4.0/memoryweb_v1.4.0_linux_amd64.tar.gz

# ARM64
curl -L -o /tmp/memoryweb.tar.gz \
  https://github.com/corbym/memoryweb/releases/download/v1.4.0/memoryweb_v1.4.0_linux_arm64.tar.gz
```

---

## Step 3 — Extract and install the binary

```bash
# Extract the archive
tar -xzf /tmp/memoryweb.tar.gz -C /tmp

# Move the binary to a directory on your PATH
sudo mv /tmp/memoryweb_linux_amd64/memoryweb /usr/local/bin/memoryweb
# (ARM64 users: replace amd64 with arm64 above)

# Mark it as executable
sudo chmod +x /usr/local/bin/memoryweb
```

Verify the installation:

```bash
memoryweb --help
```

You should see usage output. If you see "command not found", check that `/usr/local/bin` is on your PATH:

```bash
echo $PATH
```

If it is missing, add it to your shell profile:

```bash
echo 'export PATH="/usr/local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

---

## Step 4 — Install Ollama (for semantic search)

Semantic search requires [Ollama](https://ollama.com) running locally with the `snowflake-arctic-embed` model. This step is optional — memoryweb falls back to keyword search if Ollama is unavailable — but highly recommended.

Install Ollama using the official install script:

```bash
curl -fsSL https://ollama.com/install.sh | sh
```

This installs the `ollama` binary and sets up a systemd service that starts Ollama automatically.

Pull the embedding model:

```bash
ollama pull snowflake-arctic-embed
```

This downloads about 130 MB. Wait for it to complete.

Verify Ollama is running:

```bash
ollama list
```

You should see `snowflake-arctic-embed` in the output.

If Ollama is not running as a service, start it manually in a separate terminal:

```bash
ollama serve
```

---

## Step 5 — Run setup

The `setup` subcommand installs the Claude Code hooks and, if Ollama is not yet installed, offers to install it automatically using the official Linux install script (`https://ollama.com/install.sh`). On Linux, `memoryweb setup` can only auto-configure **Claude Desktop** (ChatGPT Desktop is not available on Linux).

> **Note:** If you already installed Ollama in Step 4 and the server is running, `setup` will skip the install prompt, pull `snowflake-arctic-embed` if it is missing, and proceed straight to installing the hooks.

```bash
memoryweb setup
```

The setup program will:
- If `ollama` is not in PATH: prompt you to install it via `https://ollama.com/install.sh`.
- If `ollama` is installed but the server is not running: start it in the background.
- Pull the `snowflake-arctic-embed` model if it has not been pulled yet.
- Install the `Stop` and `PreCompact` hooks into `~/.claude/settings.local.json`.
- Detect **Claude Desktop** (if `~/.config/Claude/` exists) and ask whether to configure it:
  ```
  Detected Claude Desktop. Configure it? [y/N]
  ```
  Answering `y` writes the MCP server entry to `~/.config/Claude/claude_desktop_config.json`.
- Print a summary of what was configured.

To preview what `setup` would do without writing any files:

```bash
memoryweb setup --dry-run
```

If you want the hooks in a specific directory:

```bash
memoryweb setup --hooks-dir /path/to/hooks
```

After setup, run `memoryweb doctor` to verify every component is wired correctly:

```bash
memoryweb doctor
```

Each line will show `[✓]` (pass), `[✗]` (fail), `[!]` (warning), or `[i]` (info). Fix any `[✗]` items before proceeding.

---

## Step 6 — Configure your AI client

`memoryweb setup` (Step 5) configures Claude Desktop automatically when it detects the `~/.config/Claude/` directory. The manual steps below are for cases where setup was not run, or you want to verify or edit the config files yourself.

> **Note:** ChatGPT Desktop is not available on Linux. If you are using a different desktop MCP client, consult that client's documentation for its config file location and use the same `mcpServers` JSON format shown below.

### Claude Desktop

Claude Desktop on Linux reads its config from:

```
~/.config/Claude/claude_desktop_config.json
```

Create or edit this file:

```bash
mkdir -p ~/.config/Claude
nano ~/.config/Claude/claude_desktop_config.json
```

Add the following content:

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/usr/local/bin/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/home/YOUR_USERNAME/.memoryweb.db"
      }
    }
  }
}
```

Replace `YOUR_USERNAME` with your Linux username (run `whoami` if you are unsure). Save the file and **restart Claude Desktop**.

> **Note:** Claude Desktop does not support hooks. To prompt the agent to file knowledge, add filing instructions to your system prompt manually.

### Claude Code

Claude Code picks up the hooks installed by `memoryweb setup` automatically. If you skipped the setup step, locate the hook scripts:

- **Homebrew install:** the scripts are already at `$(brew --prefix)/share/memoryweb/hooks/` — use those paths directly in the config below.
- **Manual install:** extract the scripts from the release archive:

```bash
tar -xzf /tmp/memoryweb.tar.gz -C /tmp
mkdir -p ~/.config/memoryweb/hooks
cp /tmp/memoryweb_linux_amd64/hooks/*.sh ~/.config/memoryweb/hooks/
chmod +x ~/.config/memoryweb/hooks/*.sh
```

Then add the following to `~/.claude/settings.local.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/home/YOUR_USERNAME/.config/memoryweb/hooks/memoryweb_save_hook.sh",
            "env": {
              "MEMORYWEB_DB": "/home/YOUR_USERNAME/.memoryweb.db",
              "MEMORYWEB_BIN": "/usr/local/bin/memoryweb"
            }
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/home/YOUR_USERNAME/.config/memoryweb/hooks/memoryweb_precompact_hook.sh",
            "env": {
              "MEMORYWEB_DB": "/home/YOUR_USERNAME/.memoryweb.db"
            }
          }
        ]
      }
    ]
  }
}
```

Also add memoryweb to your MCP config (`~/.claude.json` or a project-level `.mcp.json`):

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/usr/local/bin/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/home/YOUR_USERNAME/.memoryweb.db"
      }
    }
  }
}
```

Restart Claude Code to activate. After the next AI response you should see the hooks fire at the bottom of the terminal.

---

## Step 7 — Verify everything works

Start a new conversation in Claude Desktop or Claude Code and ask the agent:

> "Call `list_domains` and tell me what domains exist."

If memoryweb is connected, the agent will call the tool and return a result (an empty list is fine — you haven't filed anything yet).

---

## Updating

To check whether a newer version is available:

```bash
memoryweb doctor
```

The `[i] Update:` line will tell you if a newer release is available. You can also ask the agent — the `check_for_updates` tool checks GitHub and returns the current and latest versions.

### Homebrew update

```bash
brew update && brew upgrade memoryweb
```

Homebrew selects the correct architecture automatically. Restart your MCP client afterwards.

### Manual update

1. Download the latest archive for your architecture from the [releases page](https://github.com/corbym/memoryweb/releases/latest).
2. Extract and replace the binary atomically:

   ```bash
   curl -L -o /tmp/memoryweb.tar.gz \
     https://github.com/corbym/memoryweb/releases/download/vX.Y.Z/memoryweb_vX.Y.Z_linux_amd64.tar.gz
   tar -xzf /tmp/memoryweb.tar.gz -C /tmp
   sudo cp /tmp/memoryweb_linux_amd64/memoryweb /usr/local/bin/memoryweb.tmp
   sudo mv /usr/local/bin/memoryweb.tmp /usr/local/bin/memoryweb
   sudo chmod +x /usr/local/bin/memoryweb
   ```

   > **ARM64 users:** replace `amd64` with `arm64` in the URLs and paths above.

3. Restart your MCP client (Claude Desktop or Claude Code) so it picks up the new binary.

Your database is forward-compatible — the binary runs any pending schema migrations automatically on startup.

---

## Troubleshooting

**`memoryweb: command not found`**
Add `/usr/local/bin` to your PATH (see Step 3). Alternatively, install to `~/.local/bin` (no `sudo` required):

```bash
mkdir -p ~/.local/bin
mv /tmp/memoryweb_linux_amd64/memoryweb ~/.local/bin/memoryweb
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

**`memoryweb doctor` shows `[✗] Ollama binary: not found in PATH`**
Ollama installs to `/usr/local/bin/ollama` by default. Verify with `which ollama`. If it is not in PATH, add the directory where Ollama is installed.

**Ollama service not starting automatically**
Enable and start the service:

```bash
sudo systemctl enable ollama
sudo systemctl start ollama
```

**Permission denied when running memoryweb**
The binary may not be executable. Run:

```bash
chmod +x /usr/local/bin/memoryweb
```

**Claude Desktop shows no memoryweb tools**
Verify the config file is valid JSON. You can test it with:

```bash
python3 -m json.tool ~/.config/Claude/claude_desktop_config.json
```

Fix any errors reported, then restart Claude Desktop.

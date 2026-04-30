# Installing memoryweb on Linux

memoryweb ships two Linux binaries — one for **x86-64** (standard desktop/server CPUs) and one for **ARM64** (Raspberry Pi 4/5, AWS Graviton, Ampere, and similar). Follow the steps below for your machine.

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

The `setup` subcommand installs the Claude Code hooks and confirms Ollama is configured correctly:

```bash
memoryweb setup
```

The setup program will:
- Check that Ollama is running and the `snowflake-arctic-embed` model is available.
- Install the `Stop` and `PreCompact` hooks into `~/.claude/settings.local.json`.
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

Claude Code picks up the hooks installed by `memoryweb setup` automatically. If you skipped the setup step, locate the hook scripts in the release archive under the `hooks/` directory:

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

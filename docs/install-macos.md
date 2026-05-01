# Installing memoryweb on macOS

memoryweb ships pre-built macOS binaries for Apple Silicon (M1, M2, M3, M4) and Intel. The recommended way to install is via Homebrew.

---

## Option 1 — Homebrew (recommended)

If you have [Homebrew](https://brew.sh) installed, tap the memoryweb formula and install:

```bash
brew tap corbym/memoryweb
brew install memoryweb
```

Homebrew automatically selects the correct binary for your chip, installs it to your PATH, and places the hook scripts in `$(brew --prefix)/share/memoryweb/hooks/` (e.g. `/opt/homebrew/share/memoryweb/hooks/` on Apple Silicon, `/usr/local/share/memoryweb/hooks/` on Intel). Gatekeeper quarantine is handled automatically — Step 4 below is not required.

Once the install finishes, skip to [Step 5 — Install Ollama](#step-5--install-ollama-for-semantic-search).

---

## Option 2 — Manual installation

If you prefer not to use Homebrew, or Homebrew is not installed, follow the steps below.

---

## Step 1 — Find out which Mac you have

1. Click the **Apple menu** (🍎) in the top-left corner of the screen.
2. Choose **About This Mac**.
3. Look at the **Chip** or **Processor** line:
   - If it says **Apple M1**, **M2**, **M3**, **M4** (or any M-series number) → you have **Apple Silicon**. Download the `arm64` binary.
   - If it says **Intel Core i5**, **i7**, **i9**, **Xeon**, or anything with "Intel" → you have an **Intel Mac**. Download the `amd64` binary.

---

## Step 2 — Download the binary

Go to the [latest releases page](https://github.com/corbym/memoryweb/releases/latest) and download the file that matches your chip:

| Chip | File to download |
|------|-----------------|
| Apple Silicon (M1/M2/M3/M4) | `memoryweb_vX.Y.Z_darwin_arm64.tar.gz` |
| Intel | `memoryweb_vX.Y.Z_darwin_amd64.tar.gz` |

Replace `X.Y.Z` with the version number shown on the releases page (e.g. `1.4.0`).

---

## Step 3 — Extract and install the binary

Open **Terminal** (find it in Applications → Utilities → Terminal, or press `⌘ Space` and type "Terminal").

Run the following commands, substituting the filename you downloaded and your chip suffix (`arm64` or `amd64`):

```bash
# Extract the archive (replace the filename with what you downloaded)
tar -xzf ~/Downloads/memoryweb_v1.4.0_darwin_arm64.tar.gz -C ~/Downloads

# Move the binary to a permanent location on your PATH
sudo mv ~/Downloads/memoryweb_darwin_arm64/memoryweb /usr/local/bin/memoryweb

# Mark it as executable
chmod +x /usr/local/bin/memoryweb
```

> **Intel users:** replace `arm64` with `amd64` in the commands above.

Verify the installation worked:

```bash
memoryweb --help
```

You should see usage output. If you see "command not found", check that `/usr/local/bin` is on your `PATH` by running `echo $PATH`.

---

## Step 4 — Allow the binary through Gatekeeper (manual install only)

> **Homebrew users:** skip this step. Homebrew handles Gatekeeper quarantine automatically.

macOS may block the binary the first time you run it because it was downloaded from the internet. If you see a dialog saying *"memoryweb cannot be opened because Apple cannot check it for malicious software"*:

1. Open **System Settings** → **Privacy & Security**.
2. Scroll down to the **Security** section.
3. You will see a message about `memoryweb` being blocked. Click **Allow Anyway**.
4. Run `memoryweb --help` in Terminal again and click **Open** in the confirmation dialog.

Alternatively, you can remove the quarantine attribute from Terminal:

```bash
xattr -d com.apple.quarantine /usr/local/bin/memoryweb
```

---

## Step 5 — Install Ollama (for semantic search)

Semantic search requires [Ollama](https://ollama.com) running locally with the `snowflake-arctic-embed` model. This step is optional — memoryweb falls back to keyword search if Ollama is unavailable — but highly recommended.

1. Download Ollama from [https://ollama.com/download](https://ollama.com/download) and run the installer.
2. After installation, Ollama runs automatically in the background (you will see its icon in the menu bar).
3. Pull the embedding model:

   ```bash
   ollama pull snowflake-arctic-embed
   ```

   This downloads about 130 MB. Wait for it to complete.

4. Verify Ollama is running:

   ```bash
   ollama list
   ```

   You should see `snowflake-arctic-embed` in the output.

---

## Step 6 — Run setup

The `setup` subcommand installs the Claude Code hooks, detects desktop MCP clients (Claude Desktop and ChatGPT Desktop), and, once Ollama is in your PATH, starts the server and pulls the model automatically.

```bash
memoryweb setup
```

The setup program will:
- Detect that Ollama is installed and start the server if it is not already running.
- Pull the `snowflake-arctic-embed` model if it has not been pulled yet.
- Install the `Stop` and `PreCompact` hooks into `~/.claude/settings.local.json`.
- Detect **Claude Desktop** and **ChatGPT Desktop** (if installed) and ask whether to configure each one:
  ```
  Detected Claude Desktop. Configure it? [y/N]
  Detected ChatGPT Desktop. Configure it? [y/N]
  ```
  Answering `y` writes the MCP server entry to the appropriate config file. You can also configure these manually (see Step 7).
- Print a summary of what was configured.

> **Note:** If you skipped Step 5 and Ollama is not yet installed, `setup` will offer to install it automatically. On macOS the automatic install path uses a Linux shell script that may not work correctly — complete Step 5 first if prompted.

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

## Step 7 — Configure your AI client

`memoryweb setup` (Step 6) configures Claude Desktop and ChatGPT Desktop automatically when it detects them. The manual steps below are for cases where setup was not run, or you want to verify or edit the config files yourself.

### Claude Desktop

Open (or create) the Claude Desktop config file:

```
~/Library/Application Support/Claude/claude_desktop_config.json
```

You can open it with TextEdit or any editor. If it does not exist yet, create it with the following content:

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/usr/local/bin/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/Users/YOUR_USERNAME/.memoryweb.db"
      }
    }
  }
}
```

Replace `YOUR_USERNAME` with your macOS username (run `whoami` in Terminal if you are unsure).

Save the file, then **quit and relaunch Claude Desktop**. memoryweb will appear as an available tool in new conversations.

> **Note:** Claude Desktop does not support hooks. To prompt the agent to file knowledge, add filing instructions to your system prompt manually.

### ChatGPT Desktop

Open (or create) the ChatGPT Desktop MCP config file:

```
~/Library/Application Support/ChatGPT/mcp.json
```

Add the following content (create the file if it does not exist):

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/usr/local/bin/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/Users/YOUR_USERNAME/.memoryweb.db"
      }
    }
  }
}
```

Replace `YOUR_USERNAME` with your macOS username.

Save the file, then **quit and relaunch ChatGPT Desktop**. memoryweb will appear as an available tool in new conversations.

> **Note:** ChatGPT Desktop does not support hooks. Add filing instructions to your Custom Instructions (Settings → Personalization → Custom Instructions) to prompt the model to use memoryweb automatically.

### Claude Code

Claude Code picks up the hooks installed by `memoryweb setup` automatically. If you skipped the setup step, add the following to `~/.claude/settings.local.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/local/bin/memoryweb_save_hook.sh",
            "env": {
              "MEMORYWEB_DB": "/Users/YOUR_USERNAME/.memoryweb.db",
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
            "command": "/usr/local/bin/memoryweb_precompact_hook.sh",
            "env": {
              "MEMORYWEB_DB": "/Users/YOUR_USERNAME/.memoryweb.db"
            }
          }
        ]
      }
    ]
  }
}
```

If you installed via **Homebrew**, the hook scripts are already at `$(brew --prefix)/share/memoryweb/hooks/` — use `$(brew --prefix)/share/memoryweb/hooks/memoryweb_save_hook.sh` and `$(brew --prefix)/share/memoryweb/hooks/memoryweb_precompact_hook.sh` as the paths above. If you installed **manually**, the hook scripts ship inside the release archive under the `hooks/` directory. Copy them somewhere permanent (e.g. `~/.config/memoryweb/hooks/`) and update the paths above.

Restart Claude Code to activate. After the next AI response you should see the hooks fire at the bottom of the terminal.

Also add memoryweb to your MCP config. Claude Code reads from `~/.claude.json` or a project-level `.mcp.json`:

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/usr/local/bin/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/Users/YOUR_USERNAME/.memoryweb.db"
      }
    }
  }
}
```

---

## Step 8 — Verify everything works

Start a new conversation in Claude Desktop, ChatGPT Desktop, or Claude Code and ask the agent:

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

Homebrew selects the correct binary for your chip and handles Gatekeeper quarantine automatically. Restart your MCP client afterwards.

### Manual update

1. Download the latest archive for your chip from the [releases page](https://github.com/corbym/memoryweb/releases/latest).
2. Extract and replace the binary atomically:

   ```bash
   tar -xzf ~/Downloads/memoryweb_vX.Y.Z_darwin_arm64.tar.gz -C ~/Downloads
   sudo cp ~/Downloads/memoryweb_darwin_arm64/memoryweb /usr/local/bin/memoryweb.tmp
   sudo mv /usr/local/bin/memoryweb.tmp /usr/local/bin/memoryweb
   ```

   > **Intel users:** replace `arm64` with `amd64` above.

3. If macOS blocks the new binary, clear the quarantine flag:

   ```bash
   xattr -d com.apple.quarantine /usr/local/bin/memoryweb
   ```

4. Restart your MCP client (Claude Desktop, ChatGPT Desktop, or Claude Code) so it picks up the new binary.

Your database is forward-compatible — the binary runs any pending schema migrations automatically on startup.

---

## Troubleshooting

**`memoryweb: command not found`**
`/usr/local/bin` may not be in your PATH. Run `echo $PATH` to check. You can add it to your shell profile with:
```bash
echo 'export PATH="/usr/local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

**Gatekeeper blocks the binary**
Follow Step 4 above to allow it through System Settings, or run:
```bash
xattr -d com.apple.quarantine /usr/local/bin/memoryweb
```

**Ollama model not found**
Make sure Ollama is running (`ollama list` should return results). If it is not running, launch it from Applications or run `ollama serve` in Terminal.

**Claude Desktop or ChatGPT Desktop shows no memoryweb tools**
Double-check the config path — note the spaces in "Application Support" and the app name. Make sure the JSON is valid (no trailing commas). Quit and relaunch the application fully.

**`memoryweb doctor` shows `[✗] Ollama binary: not found in PATH`**
Ollama installs to `/usr/local/bin/ollama` on macOS. If it is missing from PATH, add `/usr/local/bin` to your `PATH` as shown above.

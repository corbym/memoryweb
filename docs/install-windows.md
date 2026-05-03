# Installing memoryweb on Windows

memoryweb ships a Windows binary for **x86-64** (64-bit Intel/AMD CPUs). This covers virtually all modern Windows PCs. There is no separate ARM binary for Windows at this time.

---

## Step 1 — Download the binary

Go to the [latest releases page](https://github.com/corbym/memoryweb/releases/latest) and download:

```
memoryweb_vX.Y.Z_windows_amd64.zip
```

Replace `X.Y.Z` with the version number shown on the releases page (e.g. `1.4.0`).

---

## Step 2 — Extract and install the binary

1. Open **File Explorer** and navigate to your **Downloads** folder.
2. Right-click the downloaded `.zip` file and choose **Extract All…**
3. Choose a destination (for example `C:\memoryweb`) and click **Extract**.

You should now have a file at `C:\memoryweb\memoryweb_windows_amd64\memoryweb.exe`.

### Add the binary to your PATH

Adding the binary to your PATH means you can run `memoryweb` from any terminal without typing the full path every time.

1. Press `Win + S` and search for **"Environment Variables"**, then click **"Edit the system environment variables"**.
2. In the System Properties window, click **Environment Variables…**
3. Under **User variables**, find the `Path` entry and double-click it.
4. Click **New** and add the full path to the folder containing `memoryweb.exe`, e.g.:
   ```
   C:\memoryweb\memoryweb_windows_amd64
   ```
5. Click **OK** on all dialogs to save.
6. Close and reopen any open Command Prompt or PowerShell windows.

Verify the installation in a new terminal:

```powershell
memoryweb --help
```

You should see usage output.

---

## Step 3 — Install Ollama (for semantic search)

Semantic search requires [Ollama](https://ollama.com) running locally with the `snowflake-arctic-embed` model. This step is optional — memoryweb falls back to keyword search if Ollama is unavailable — but highly recommended.

> **You must install Ollama before running `memoryweb setup`.** On Windows, `memoryweb setup` cannot install Ollama automatically — it relies on a Linux shell script that does not run on Windows. Install Ollama first, then run `setup`.

1. Download the Windows installer from [https://ollama.com/download](https://ollama.com/download).
2. Run the installer and follow the prompts. Ollama installs as a background service and adds an icon to the system tray.
3. Open a Command Prompt or PowerShell and pull the embedding model:

   ```powershell
   ollama pull snowflake-arctic-embed
   ```

   This downloads about 130 MB. Wait for it to complete.

4. Verify Ollama is running:

   ```powershell
   ollama list
   ```

   You should see `snowflake-arctic-embed` in the output.

---

## Step 4 — Run setup

The `setup` subcommand installs the Claude Code hooks, detects Claude Desktop, and, if Ollama is already installed and in PATH (Step 3), pulls the model and starts the server automatically.

> **Important:** `memoryweb setup` **cannot install Ollama itself on Windows**. Its automatic install path uses `sh -c "curl ... | sh"`, which requires a Unix shell that is not available by default on Windows. You must install Ollama first (Step 3). Once the `ollama` binary is in your PATH, `setup` will handle the model pull and server start.

Open a Command Prompt or PowerShell and run:

```powershell
memoryweb setup
```

The setup program will:
- Detect that Ollama is already installed and start the server if it is not running.
- Pull the `snowflake-arctic-embed` model if it has not been pulled yet.
- Install the `Stop` and `PreCompact` hooks into `%USERPROFILE%\.claude\settings.local.json`.
- Detect **Claude Desktop** (if installed) and ask whether to configure it:
  ```
  Detected Claude Desktop. Configure it? [y/N]
  ```
  Answering `y` writes the MCP server entry to the appropriate config file. You can also configure this manually (see Step 5).
- Print a summary of what was configured.

To preview what `setup` would do without writing any files:

```powershell
memoryweb setup --dry-run
```

After setup, run `memoryweb doctor` to verify every component is wired correctly:

```powershell
memoryweb doctor
```

> **Advisory:** `setup` stores the database path inside your MCP client configs. If you passed `--db C:\custom\path.db`, also set `MEMORYWEB_DB` as a user environment variable (follow the same steps as in Step 2) so that CLI commands (`memoryweb doctor`, etc.) use the same database. If you used the default path (`%USERPROFILE%\.memoryweb.db`), no action is needed — the binary falls back to that path automatically.

Each line will show `[✓]` (pass), `[✗]` (fail), `[!]` (warning), or `[i]` (info). Fix any `[✗]` items before proceeding.

## Step 5 — Configure your AI client

`memoryweb setup` (Step 4) configures Claude Desktop automatically when it detects it. The manual steps below are for cases where setup was not run, or you want to verify or edit the config files yourself.

### Claude Desktop

Claude Desktop on Windows reads its config from:

```
%APPDATA%\Claude\claude_desktop_config.json
```

To open this folder quickly, press `Win + R`, type `%APPDATA%\Claude`, and press Enter. If the folder does not exist, create it.

Create or edit `claude_desktop_config.json` with a text editor (Notepad works fine):

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "C:\\memoryweb\\memoryweb_windows_amd64\\memoryweb.exe",
      "env": {
        "MEMORYWEB_DB": "C:\\Users\\YOUR_USERNAME\\.memoryweb.db"
      }
    }
  }
}
```

Replace `YOUR_USERNAME` with your Windows username. In JSON, backslashes must be doubled (`\\`).

Save the file and **quit and relaunch Claude Desktop** from the Start menu or system tray.

> **Note:** Claude Desktop does not support hooks. To prompt the agent to file knowledge, add filing instructions to your system prompt manually.

### Claude Code

> **Note:** Claude Code on Windows requires WSL (Windows Subsystem for Linux) or Git Bash for the shell hook scripts, which are Bash scripts. If you are running Claude Code in WSL, follow the Linux install guide instead.

If you are running Claude Code natively on Windows with PowerShell, the hook scripts are not directly usable. You have two options:

**Option A — Use WSL**
Install WSL (`wsl --install` in an administrator PowerShell), then follow the [Linux install guide](./install-linux.md) inside WSL.

**Option B — Configure without hooks**
Add memoryweb to your MCP config (`%USERPROFILE%\.claude.json` or a project-level `.mcp.json`):

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "C:\\memoryweb\\memoryweb_windows_amd64\\memoryweb.exe",
      "env": {
        "MEMORYWEB_DB": "C:\\Users\\YOUR_USERNAME\\.memoryweb.db"
      }
    }
  }
}
```

Then add filing instructions to your system prompt to prompt the agent to file knowledge manually.

---

## Step 6 — Verify everything works

Start a new conversation in Claude Desktop or Claude Code and ask the agent:

> "Call `list_domains` and tell me what domains exist."

If memoryweb is connected, the agent will call the tool and return a result (an empty list is fine — you haven't filed anything yet).

---

## Updating

To check whether a newer version is available:

```powershell
memoryweb doctor
```

The `[i] Update:` line will tell you if a newer release is available. You can also ask the agent — the `check_for_updates` tool checks GitHub and returns the current and latest versions.

To update:

1. Download the latest `.zip` from the [releases page](https://github.com/corbym/memoryweb/releases/latest).
2. Extract it to a temporary folder (e.g. `C:\memoryweb-new`).
3. Replace the binary. If `memoryweb.exe` is in `C:\memoryweb\memoryweb_windows_amd64\`:

   ```powershell
   Copy-Item C:\memoryweb-new\memoryweb_windows_amd64\memoryweb.exe `
     C:\memoryweb\memoryweb_windows_amd64\memoryweb.exe -Force
   ```

4. Restart your MCP client (Claude Desktop or Claude Code) so it picks up the new binary.

Your database is forward-compatible — the binary runs any pending schema migrations automatically on startup.

---

## Troubleshooting

**`memoryweb: command not found` / `'memoryweb' is not recognized`**
The folder containing `memoryweb.exe` is not in your PATH. Follow Step 2 to add it. After updating PATH, you must open a new terminal window for the change to take effect.

**`memoryweb doctor` shows `[✗] Ollama binary: not found in PATH`**
Ollama installs to `%LOCALAPPDATA%\Programs\Ollama` on Windows. Verify it is in PATH by running `where ollama`. If not found, add the Ollama installation directory to your PATH as in Step 2.

**Ollama is installed but `ollama list` fails**
The Ollama service may not be running. Look for the Ollama icon in the system tray and click it to start, or run `ollama serve` in a terminal.

**Claude Desktop shows no memoryweb tools**
Verify the config file is valid JSON — Windows Notepad can silently add a BOM that breaks JSON parsers. Use VS Code or Notepad++ to edit the file, or validate it with:

```powershell
# Claude Desktop
Get-Content "$env:APPDATA\Claude\claude_desktop_config.json" | ConvertFrom-Json
```

If PowerShell reports an error, fix the JSON and restart the application.

**Security warning: Windows Defender SmartScreen blocks the binary**
When you first run `memoryweb.exe`, Windows may show a blue "Windows protected your PC" dialog. Click **More info**, then **Run anyway**. This happens because the binary is not signed with a paid code-signing certificate.

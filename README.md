<p align="left"><strong>English</strong> · <a href="README_ZH.md">简体中文</a></p>

<p align="center">
  <img src="resources/icon.png" alt="Opswright logo" width="128" height="128">
</p>

<h1 align="center">Opswright</h1>

Opswright is a native desktop SSH / SFTP client for Windows, bringing terminals, file management and AI assistance into one app.

## Screenshots

<table>
  <tr>
    <td width="50%" align="center">
      <img src="docs/terminal-ai-assistant.webp" alt="Terminal and AI assistant" width="100%"><br>
      Terminal and AI assistant
    </td>
    <td width="50%" align="center">
      <img src="docs/sftp-file-manager.webp" alt="SFTP file manager" width="100%"><br>
      SFTP file manager
    </td>
  </tr>
  <tr>
    <td width="50%" align="center">
      <img src="docs/system-monitor.webp" alt="System monitor" width="100%"><br>
      System monitor
    </td>
    <td width="50%" align="center">
      <img src="docs/ai-assistant-settings.webp" alt="AI assistant settings" width="100%"><br>
      AI assistant settings
    </td>
  </tr>
</table>

## Features

- **Terminal**
  - Tabs, split panes and terminal search, with tabs you can reorder and rename
  - True colour, wide characters, input methods and mouse support for tmux, vim and htop
  - Ten built-in colour schemes plus `.itermcolors` import, keyword highlighting, line numbers and timestamps, and adjustable cursor, line height and character width
  - Many output encodings, set per host; Ctrl-click to open links in the terminal, Alt-drag for block selection
  - Command suggestions from the host's shell history as you type, accepted with → or End, and a list of the folder's files to complete paths from
  - Telnet, serial ports and local terminals besides SSH (PowerShell, CMD, Git Bash and WSL on Windows)
  - Shared connections across panes on the same host, with automatic reconnection that acts at once on a network change or wake from sleep and returns the shell to its folder

- **Workspace**
  - System monitor, process usage, host details, Docker, snippets, command history, broadcast commands and session logs are draggable panes that dock beside any pane or in another tab
  - Docker container and image details with resource usage, plus starting and stopping containers
  - Broadcast commands with synchronised typing, and session logs recorded as text or raw output
  - Command palette and customisable shortcuts, with optional memory of window and split layouts

- **File management**
  - SFTP alongside the terminal with directory following, falling back to SCP when the server has no SFTP
  - Tabbed built-in editor with a folder tree
  - File access through sudo when you need it
  - Drag-and-drop support and folder transfers; uploads keep permissions and modification times
  - Copy files and folders straight from one server to another by drag-and-drop or copy and paste, streamed through the app without touching the local disk
  - Transfer queue with pause, resume and retry, keeping the computer awake while files transfer

- **AI assistant**
  - Run commands and edit files through a dedicated assistant for each host, over SSH, Telnet, serial or a local terminal, with conversations saved per host
  - Connect AI providers, or use ACP agents detected on this computer (Claude Code, Codex, Grok, Gemini CLI and more), several at once, each with the models and thinking levels it offers; their work stays on the remote host
  - Paste images and files as attachments
  - Choose Ask, Review or Full access mode, with a configurable review model, tool-call limit per turn and request timeout

- **Agent integrations**
  - Connect local AI agents through the open API, MCP and Skill
  - Browse hosts, establish connections and run commands without exposing saved credentials
  - One-click Skill setup for Claude Code and Codex; agent access is off by default

- **Hosts and credentials**
  - Add and group hosts by hand, or connect quickly with `user@host:port`
  - Passwords, OpenSSH / PEM / PuTTY keys, OpenSSH certificates, SSH agent and password-less login support
  - Nested groups you can rearrange by dragging, with multi-select and latency tests
  - Local encrypted credential storage, key generation and public-key installation; a host's key is trusted on first use and checked by fingerprint only when it changes, and known hosts can be managed

- **Backup and restore**
  - Password-encrypted backups of hosts, credentials, snippets and settings, with restore on another device
  - Save locally or to WebDAV, S3-compatible storage and private GitHub Gists
  - Manual or scheduled backups, with configurable retention

- **Networking**
  - Multi-hop jump hosts and HTTP / SOCKS5 proxies, which AI providers and update checks can use too
  - Local, remote and dynamic port forwarding

- **Everyday use**
  - Server metrics at a glance
  - Light and dark themes, with background images and interface scaling
  - Close to the tray and connect to hosts from the tray popover
  - English and Simplified Chinese

## Secrets and the master password

Run Opswright as your normal desktop user, without root or `sudo`. Saving and unlocking credentials needs no setup of its own: a single **master password** seals a random master key in `master.enc`, and that key encrypts everything Opswright saves in `secrets.enc`.

The first time you use Opswright it asks you to set the master password. That computer then remembers the key in the Windows Credential Manager, through the operating system itself, so later launches open without asking. A lock you ask for by hand — the shortcut, the tray, the settings — needs the password again for the rest of that run; the next launch is remembered again. Idle time, screen lock and sleep only hide the windows and never ask.

Copying your data folder to another computer, or to another OS user, carries `master.enc`, so that computer asks for the password once and then remembers it too. Opswright never stores the password: if you forget it, saved secrets can only be recovered from a backup, and the lock screen's reset deletes them. **Settings → About → Secret storage** changes the password, and forgets the key on this computer if you want to be asked again.

## Development

Built with [mygo](https://github.com/egoist/mygo). Requires Go 1.27.3 (`GOTOOLCHAIN` can download it). No cgo is needed.

```sh
# run
go run .

# run the development app
scripts/run-dev-app.sh

# keep data in a separate directory
OPSWRIGHT_DATA_DIR=/tmp/demo go run .

# a local SSH server for testing
go run ./cmd/devsshd -addr 127.0.0.1:2222 -user demo -password demo -root /tmp/remote

# a local HTTP / SOCKS proxy for testing
go run ./cmd/devproxy -addr 127.0.0.1:3128

# tests and checks
go test -race -count=1 ./...
go vet ./...
gofmt -l .

# regenerate the locale catalog after editing locales/*.json
GOFLAGS=-mod=mod go run github.com/egoist/mygo/cmd/mygo generate

# package into dist/
go tool mygo build -platform windows/amd64 -o dist
```

To try it against devsshd, add a host `127.0.0.1`, port `2222`, user `demo`, password `demo`.

Contributor rules are in [AGENTS.md](AGENTS.md).

## Community Support

- [LinuxDO](https://linux.do)

## License

Opswright is licensed under the [GNU Affero General Public License v3.0](LICENSE).

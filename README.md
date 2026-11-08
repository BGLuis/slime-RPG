<p align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/header/graph.svg?title=Slime+RPG&subtitle=Game+and+dataset+localization+toolkit+in+pure+Go&align=left&logo=go&mode=dark" /><img alt="Slime RPG" src="https://shieldcn.dev/header/graph.svg?title=Slime+RPG&subtitle=Game+and+dataset+localization+toolkit+in+pure+Go&align=left&logo=go&mode=light" /></picture>
</p>

<p align="center"><b>English</b> · <a href="README.pt-BR.md">Português (BR)</a></p>

<p align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/github/stars/BGLuis/slime-RPG.svg?variant=secondary&size=sm&mode=dark" /><img alt="GitHub Stars" src="https://shieldcn.dev/github/stars/BGLuis/slime-RPG.svg?variant=secondary&size=sm&mode=light" /></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/github/forks/BGLuis/slime-RPG.svg?variant=secondary&size=sm&mode=dark" /><img alt="GitHub Forks" src="https://shieldcn.dev/github/forks/BGLuis/slime-RPG.svg?variant=secondary&size=sm&mode=light" /></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/github/watchers/BGLuis/slime-RPG.svg?variant=secondary&size=sm&mode=dark" /><img alt="Watchers" src="https://shieldcn.dev/github/watchers/BGLuis/slime-RPG.svg?variant=secondary&size=sm&mode=light" /></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/github/contributors/BGLuis/slime-RPG.svg?theme=emerald&size=sm&mode=dark" /><img alt="Contributors" src="https://shieldcn.dev/github/contributors/BGLuis/slime-RPG.svg?theme=emerald&size=sm&mode=light" /></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/github/license/BGLuis/slime-RPG.svg?variant=ghost&size=sm&mode=dark" /><img alt="License" src="https://shieldcn.dev/github/license/BGLuis/slime-RPG.svg?variant=ghost&size=sm&mode=light" /></picture>
</p>

<p align="center">
  <img alt="Go" src="https://shieldcn.dev/badge/Language-Go-00ADD8.svg?logo=go&variant=branded&size=sm" />
  <img alt="SQLite" src="https://shieldcn.dev/badge/Database-SQLite-003B57.svg?logo=sqlite&variant=branded&size=sm" />
  <img alt="Docker" src="https://shieldcn.dev/badge/Tool-Docker-2496ED.svg?logo=docker&variant=branded&size=sm" />
  <img alt="Ollama" src="https://shieldcn.dev/badge/Tool-Ollama-000000.svg?logo=ollama&variant=branded&size=sm" />
  <img alt="DeepL" src="https://shieldcn.dev/badge/Tool-DeepL-0F2B46.svg?logo=deepl&variant=branded&size=sm" />
  <img alt="Google Translate" src="https://shieldcn.dev/badge/Tool-Google_Translate-4285F4.svg?logo=googletranslate&variant=branded&size=sm" />
</p>

# 📖 About

**Slime RPG** is a pure-Go (zero CGO) toolkit for extracting translatable text, masking control codes and variables (`__XTOK_<n>__`), automating AI/API translation, and safely reinjecting localized strings back into games and structured datasets. The retired Python implementation remains available on the `legacy/python` branch.

Key capabilities:

- **Single self-contained binary** with three built-in interfaces: interactive **Terminal UI** (`charmbracelet/bubbletea` + `lipgloss`), native **Desktop UI** (`egoist/mygo` system webview), and **headless CLI** (with `--dry-run` estimation and `--json` JSON Lines output).
- **Supported engines and formats**:
  - **RPG Maker MV / MZ** (`www/data/*.json`, `data/*.json`, and `js/plugins.js`)
  - **RPG Maker VX Ace / VX / XP** (`Data/*.rvdata2`, `*.rvdata`, `*.rxdata` via a pure-Go Ruby Marshal 4.8 parser)
  - **WOLF RPG Editor** (`Data/BasicData`, `.mps` maps, and `.wolf` archives)
  - **CSV spreadsheets** (delimiter sniffing, BOM, and CRLF preservation) and **generic JSON** (RFC 6901 JSON Pointers)
- **Translation providers**: **Google Translate** (with automatic proxy rotation), **DeepL API** (Free & Pro auto-detection), and **Ollama** (offline local LLMs with game synopsis context).
- **SQLite Translation Memory**: local pure-Go persistence (`modernc.org/sqlite` with WAL mode and FTS5), TMX import/export, glossary term locking, and status precedence (`locked` > `reviewed` > `machine`).
- **Lifecycle & integrity tracking (`.slime/meta.json`)**: atomic workspace writes, automatic backups, incremental updates (`--incremental`), offline cache reapplication (`--memory-only`), provider switching (`--switch-engine`), and pre-translated game adoption (`slime adopt`).

# 📋 Motivation

I initially created this repository to translate RPG Maker games and later evolve the concept to also translate website JSON i18n files.

# 💻 Getting Started

### Requirements

- [Go](https://go.dev/dl/) 1.27.3+ (local execution)
- [Task](https://taskfile.dev/installation/) v3 (local execution)
- [Docker](https://docs.docker.com/get-docker/) & Docker Compose (Docker option)
- [Ollama](https://ollama.com/download) (optional, for local LLM translation)

### Installation

1. Clone the repository:

   ```sh
   git clone https://github.com/BGLuis/slime-RPG.git
   ```

2. Enter the project directory:

   ```sh
   cd slime-RPG
   ```

#### Option 1: Docker

3. Build the image and run the CLI container against your game directory:

   ```sh
   docker compose build
   SLIME_GAMES_DIR=/path/to/games docker compose run --rm slime translate -i /games/MyGame -s ja -d pt
   ```

#### Option 2: Local execution

3. Build and start the application (or install the binary and shortcuts globally):

   ```sh
   # Build and open the interactive Terminal UI (TUI)
   task run

   # Build and open the native Desktop UI (GUI)
   task run:gui

   # Optional: install slime, slm, sl, and slime-gui into ~/.local/bin
   task install-cli
   ```

   On **Arch Linux (AUR)**, you can also install the prebuilt package with desktop shortcuts and Nemo/Dolphin context menus ("Translate Game Folder"):

   ```sh
   yay -S slime-rpg-bin
   ```

# 🚀 Usage

### CLI & Lifecycle Commands

```sh
# Launch the interactive TUI in the current folder
slime

# Launch the native Desktop UI
slime --gui

# Headless translation (incremental by default)
slime translate -i ./MyGame --service google --from ja --to pt

# Pre-flight estimation without modifying files, or structured JSON Lines output
slime translate -i ./MyGame --dry-run
slime translate -i ./MyGame --json

# Reapply cached translations offline or switch providers keeping reviewed/locked entries
slime translate -i ./MyGame --memory-only -s ja -d pt
slime translate -i ./MyGame --switch-engine --service deepl -s ja -d pt

# Verify translated game integrity or adopt an existing external translation
slime verify ./MyGame --original ./MyGameOriginal
slime adopt ./MyGame --original ./MyGameOriginal -e rpgmaker -s ja -d pt

# Translate asset filenames or manage the SQLite Translation Memory (TMX & glossary)
slime filenames -i ./MyGame/www/img/pictures -s ja -d pt
slime tm
slime tm export --src ja --tgt pt --out backup.tmx
slime tm import --file backup.tmx --status reviewed
slime tm glossary --src ja --tgt pt --source "勇者" --target "Hero"
```

### Taskfile Commands

```sh
task              # Build bin/slime for the current OS
task build:all    # Cross-compile Linux (amd64/arm64) and Windows (.exe) with CGO_ENABLED=0
task test         # Run the unit test suite with the race detector (-race)
task fmt          # Format Go source files
task tidy         # Tidy go.mod and go.sum
task clean        # Remove compiled artifacts
```

### Technical Documentation

- [Go Architecture](docs/ARCHITECTURE_GO.md) & [System Architecture Overview](ARCHITECTURE.md)
- [Python-to-Go Feature Parity](docs/PYTHON_PARITY.md)
- [Product Scope & Compatibility](PRODUCT.md)
- [RPG Maker Event Codes Reference](docs/engines/rpg_maker.md)

# ⚙️ Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `OLLAMA_HOST` | HTTP endpoint of the local or remote Ollama server | `http://localhost:11434` |
| `OLLAMA_MODEL` | Ollama model used for translation | `qwen2.5` |
| `OLLAMA_MAX_REQUESTS` | Maximum concurrent requests sent to Ollama | `10` |
| `OLLAMA_CONTEXT_FUNCTION` | Custom primary system prompt sent to Ollama | `-` |
| `OLLAMA_CONTEXT_ADDITIONAL` | Custom style and tag-preservation instructions for Ollama | `-` |
| `DEEPL_API_KEY` | Official DeepL API key (Free `:fx` or Pro) | `-` |
| `USE_PROXY_POOL` | Enables automatic rotating proxy pool for Google Translate | `true` |
| `DISABLE_PROXY_POOL` | Forces disabling the rotating proxy pool when `true` | `false` |
| `HTTP_PROXY` / `HTTPS_PROXY` | Optional custom HTTP/HTTPS proxy read by the Go runtime | `-` |
| `SLIME_LANG` | UI and CLI language override (`pt-BR` or `en`) | System locale |
| `SLIME_DATA_DIR` | Custom base directory for persistent application data | XDG / AppData `slime-rpg` |
| `SLIME_CONFIG_DIR` | Custom directory for user preferences (`settings.db`) | XDG / AppData `slime-rpg` |
| `SLIME_CACHE_DIR` | Custom directory for cache files | XDG / AppData `slime-rpg` |
| `SLIME_LOG_DIR` | Custom directory for log files | XDG State / `logs` |
| `SLIME_MEMORY_DB` | Explicit file path override for SQLite `memory.db` | `<SLIME_DATA_DIR>/memory.db` |
| `SLIME_SETTINGS_DB` | Explicit file path override for SQLite `settings.db` | `<SLIME_CONFIG_DIR>/settings.db` |
| `SLIME_LOG_FILE` | Explicit file path override for `processamento.log` | `<SLIME_LOG_DIR>/processamento.log` |

# 🤝 Contributors

<a href="https://github.com/BGLuis/slime-RPG/graphs/contributors">
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/contributors/BGLuis/slime-RPG.svg?title=false&preset=transparent&border=false&mode=dark" /><img alt="Contributors" src="https://shieldcn.dev/contributors/BGLuis/slime-RPG.svg?title=false&preset=transparent&border=false&mode=light" /></picture>
</a>

# 📄 License

Distributed under the [MIT](LICENSE) license.

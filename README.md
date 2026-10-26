<div align="center">

<!-- Badges de Status do GitHub -->
![GitHub Stars](https://www.shieldcn.dev/github/stars/bgluis/extractor-and-translation.svg?variant=secondary&size=sm)
![GitHub Forks](https://www.shieldcn.dev/github/forks/bgluis/extractor-and-translation.svg?variant=secondary&size=sm)
![Watchers](https://www.shieldcn.dev/github/watchers/bgluis/extractor-and-translation.svg?variant=secondary&size=sm)
![Contributors](https://www.shieldcn.dev/github/contributors/bgluis/extractor-and-translation.svg?theme=emerald&size=sm)
![License](https://www.shieldcn.dev/github/license/bgluis/extractor-and-translation.svg?variant=ghost&size=sm)

<br/>

<!-- Badges das Tecnologias Utilizadas -->
![Python](https://www.shieldcn.dev/badge/Python-3776AB.svg?logo=python&variant=branded&size=sm)
![Docker](https://www.shieldcn.dev/badge/Docker-2496ED.svg?logo=docker&variant=branded&size=sm)
![Ollama](https://www.shieldcn.dev/badge/Ollama-FFFFFF.svg?logo=ollama&variant=branded&size=sm)

  <h3>Extractor and Translation</h3>
  Text extraction and translation tool for RPG Maker games and JSON i18n files.
</div>

*Read this in other languages: [Português](README.pt-br.md)*

# 📖 About
Extractor and Translation is a Python-based tool designed to extract translatable text from specific formats and automate its translation using AI models via Ollama. It leverages modern libraries like PyQt5 for a GUI and deep_translator/Ollama for translating game assets and localization JSON files efficiently. It simplifies the localization process by managing the context and concurrency of requests to the AI engine.

# 📋 Reason
The project was initially created to translate RPG Maker games, and later the concept evolved to also translate website JSON i18n files.

# 💻 Getting Started

### Requirements
- [Python 3.10+](https://www.python.org/downloads/)
- [Docker](https://docs.docker.com/get-docker/) (Optional, for containerized execution)
- [Ollama](https://ollama.com/) (If using local LLMs for translation)

### Installation

1. Clone the repository:
  ```sh
  git clone https://github.com/bgluis/extractor-and-translation.git
  ```

2. Navigate to the project directory:
  ```sh
  cd extractor-and-translation
  ```

#### Method 1: Native Execution (Python)

3. Create and activate a virtual environment (optional but recommended):
  ```sh
  python -m venv .venv
  source .venv/bin/activate  # On Windows use: .venv\Scripts\activate
  ```

4. Install the dependencies:
  ```sh
  pip install -r requirements.txt
  ```

5. Run the application:
  ```sh
  python main.py
  ```

#### Method 2: Global CLI Installation with Shortcuts (Slime)

You can install the executable globally with pre-configured terminal shortcuts/abbreviations (`slime`, `slm`, `sl`):

```sh
# Installs 'slime' and shortcuts (slm, sl, extractor-translation) into ~/.local/bin
make install-cli
```

After installation, you can run the tool from any folder:

```sh
# Start interactive mode:
slime
# or with shortcuts:
slm
sl --interactive

# Direct translation:
slm -i /path/to/game -e RPGMaker -t Google -s ja -d pt

# Launch GUI:
slime --gui

# Add custom aliases/abbreviations:
make add-alias ALIAS=slx
# or directly:
slime --add-alias slx
```

3. Build and start the container using Docker Compose:
  ```sh
  docker-compose up -d --build
  ```

# 🎮 Game Metadata (.slime/meta.json)

The tool manages translation lifecycle tracking through metadata stored in `.slime/meta.json` inside the game directory. This enables integrity checks, tracking game patches/updates, adopting legacy translations, and incremental translations.

### Adopting a Pre-translated Game (`--adopt`)

If you have a game that was already translated (or translated with an earlier version or external tool), you can adopt it so Slime tracks file integrity and enables incremental updates:

```sh
# Adopt a pre-translated RPG Maker game
slime --adopt /path/to/translated_game --original /path/to/original_game -e RPGMaker

# With language specifications
slime --adopt /path/to/translated_game --original /path/to/original_game -e RPGMaker -s ja -d en
```

This scans both original and translated directories, calculates SHA-256 checksums, and creates `.slime/meta.json`.

### Verifying Translation Integrity (`--verify-game`)

Check whether translated files or original game files have been modified, updated, or corrupted:

```sh
# Verify translated files against recorded metadata
slime --verify-game /path/to/translated_game

# Verify against original game folder to detect game updates/patches
slime --verify-game /path/to/translated_game --original /path/to/original_game
```

### Verification Status Codes

When verifying game files, each file is classified into one of the following states:

| Status Code | Description |
| --- | --- |
| `intact` | File matches recorded hashes and previous translation succeeded. |
| `original_changed` | The original file has changed (e.g., game patch/update). Needs re-translation. |
| `new` | A new file exists in the original game folder that was not recorded in metadata. |
| `partial` | The file was previously only partially translated. |
| `edited` | The translated file was modified locally after translation. |
| `unchanged` | The file content is identical to the original (no translatable text changed). |
| `original_missing` | The original file could not be found in the specified original folder. |
| `translation_missing` | The translated file is missing in the translated game folder. |

### Incremental Re-translation (`--incremental`)

Combine metadata verification with incremental translation to only process files that changed or are new:

```sh
slime -i /path/to/game --incremental -e RPGMaker -t Google -s ja -d en
```

### Offline Reapplication (`--memory-only` / `--offline`)

Reapply existing translations directly from local translation memory without making network calls. Missed segments remain in original source text without modifying the cache:

```sh
slime -i /path/to/game --memory-only -e RPGMaker -t Google -s ja -d en
```

### Engine Switching (`--switch-engine`)

Switch translation engines while preserving reviewed and locked segments, ignoring machine translations from other engines:

```sh
slime -i /path/to/game --switch-engine -e RPGMaker -t DeepL -s ja -d en
```

# ⚙️ Environment Variables
The application uses environment variables for configuration. You can copy `.env.example` to `.env` and adjust the values.

| Variable | Description | Default Value |
| --- | --- | --- |
| `OLLAMA_MODEL` | Ollama model to be used for translation (e.g., llama3.1, mistral) | `llama3.1` |
| `OLLAMA_MAX_REQUESTS` | Maximum number of concurrent requests to the model | `5` |
| `OLLAMA_CHAR_LIMIT` | Character limit per request (optional) | `10000` |
| `OLLAMA_CONTEXT_FUNCTION` | Custom function context for the prompt (optional) | - |
| `OLLAMA_CONTEXT_ADDITIONAL`| Additional context for the prompt (optional) | - |

# 📚 Documentation
- [System Architecture](ARCHITECTURE.md) - Design patterns, pipeline flow, and technical contracts.
- [Product Vision & Roadmap](PRODUCT.md) - Scope, supported game engines, and features.
- [RPG Maker Event Codes Reference](docs/engines/rpg_maker.md) - Complete opcode and format codes specification.

# 🤝 Contributors
 <a href="https://github.com/bgluis/extractor-and-translation/graphs/contributors">
   <img src="https://contrib.rocks/image?repo=bgluis/extractor-and-translation"/>
 </a>

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

# ⚙️ Environment Variables
The application uses environment variables for configuration. You can copy `.env.example` to `.env` and adjust the values.

| Variable | Description | Default Value |
| --- | --- | --- |
| `OLLAMA_MODEL` | Ollama model to be used for translation (e.g., llama3.1, mistral) | `llama3.1` |
| `OLLAMA_MAX_REQUESTS` | Maximum number of concurrent requests to the model | `5` |
| `OLLAMA_CHAR_LIMIT` | Character limit per request (optional) | `10000` |
| `OLLAMA_CONTEXT_FUNCTION` | Custom function context for the prompt (optional) | - |
| `OLLAMA_CONTEXT_ADDITIONAL`| Additional context for the prompt (optional) | - |

# 🤝 Contributors
 <a href="https://github.com/bgluis/extractor-and-translation/graphs/contributors">
   <img src="https://contrib.rocks/image?repo=bgluis/extractor-and-translation"/>
 </a>

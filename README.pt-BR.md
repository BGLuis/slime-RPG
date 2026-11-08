<p align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/header/graph.svg?title=Slime+RPG&subtitle=Extra%C3%A7%C3%A3o+e+tradu%C3%A7%C3%A3o+de+jogos+e+dados+em+Go+puro&align=left&logo=go&mode=dark" /><img alt="Slime RPG" src="https://shieldcn.dev/header/graph.svg?title=Slime+RPG&subtitle=Extra%C3%A7%C3%A3o+e+tradu%C3%A7%C3%A3o+de+jogos+e+dados+em+Go+puro&align=left&logo=go&mode=light" /></picture>
</p>

<p align="center"><b>Português (BR)</b> · <a href="README.md">English</a></p>

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

# 📖 Sobre

O **Slime RPG** é uma ferramenta desenvolvida em Go puro (zero CGO) para extrair textos traduzíveis, mascarar variáveis e códigos de controle (`__XTOK_<n>__`), automatizar a tradução via IA/APIs e reinjetar os textos traduzidos preservando a integridade estrutural e binária de jogos e datasets. A antiga implementação em Python segue preservada na branch `legacy/python`.

Principais recursos:

- **Binário único e autocontido** com três interfaces integradas: **Terminal UI** interativa (`charmbracelet/bubbletea` + `lipgloss`), **Desktop UI** nativa (`egoist/mygo` com webview do sistema) e **CLI headless** (com estimativa `--dry-run` e saída estruturada em JSON Lines `--json`).
- **Motores e formatos suportados**:
  - **RPG Maker MV / MZ** (`www/data/*.json`, `data/*.json` e `js/plugins.js`)
  - **RPG Maker VX Ace / VX / XP** (`Data/*.rvdata2`, `*.rvdata`, `*.rxdata` via parser Ruby Marshal 4.8 em Go puro)
  - **WOLF RPG Editor** (`Data/BasicData`, mapas `.mps` e arquivos `.wolf`)
  - **Planilhas CSV** (detecção de delimitador, BOM e preservação de CRLF) e **JSON genérico** (ponteiros RFC 6901)
- **Provedores de tradução**: **Google Translate** (com rotação automática de proxies), **DeepL API** (detecção automática Free e Pro) e **Ollama** (LLMs locais offline com suporte a sinopse do jogo).
- **Memória de Tradução SQLite**: persistência local em Go puro (`modernc.org/sqlite` com WAL mode e FTS5), importação/exportação TMX, glossário de termos travados e precedência de status (`locked` > `reviewed` > `machine`).
- **Ciclo de vida e integridade (`.slime/meta.json`)**: escrita atômica em workspace, backups automáticos, retradução incremental (`--incremental`), reaplicação offline (`--memory-only`), troca de provedor (`--switch-engine`) e adoção de jogos já traduzidos (`slime adopt`).

# 📋 Motivo

Criei esse repositório inicialmente para poder traduzir jogos de RPG Maker e posteriormente evoluir o conceito para utilizar para traduzir JSON i18n de sites.

# 💻 Como iniciar

### Requisitos

- [Go](https://go.dev/dl/) 1.27.3+ (execução local)
- [Task](https://taskfile.dev/installation/) v3 (execução local)
- [Docker](https://docs.docker.com/get-docker/) & Docker Compose (opção Docker)
- [Ollama](https://ollama.com/download) (opcional, para tradução com LLM local)

### Instalação

1. Clone o repositório:

   ```sh
   git clone https://github.com/BGLuis/slime-RPG.git
   ```

2. Entre no diretório do projeto:

   ```sh
   cd slime-RPG
   ```

#### Opção 1: Docker

3. Construa a imagem e execute o container CLI apontando para a pasta dos seus jogos:

   ```sh
   docker compose build
   SLIME_GAMES_DIR=/caminho/para/jogos docker compose run --rm slime translate -i /games/MeuJogo -s ja -d pt
   ```

#### Opção 2: Execução local

3. Compile e inicie a aplicação (ou instale o binário e os atalhos globalmente):

   ```sh
   # Compilar e abrir a interface interativa de terminal (TUI)
   task run

   # Compilar e abrir a interface desktop nativa (GUI)
   task run:gui

   # Opcional: instalar slime, slm, sl e slime-gui em ~/.local/bin
   task install-cli
   ```

   No **Arch Linux (AUR)**, você também pode instalar o pacote pré-compilado com atalho no menu de aplicativos e menus de contexto para Nemo e KDE Dolphin ("Translate Game Folder"):

   ```sh
   yay -S slime-rpg-bin
   ```

# 🚀 Uso

### Comandos de CLI e Ciclo de Vida

```sh
# Iniciar a TUI interativa na pasta atual
slime

# Iniciar a janela Desktop nativa
slime --gui

# Tradução headless (incremental por padrão)
slime translate -i ./MeuJogo --service google --from ja --to pt

# Estimativa pré-voo sem alterar arquivos ou saída em JSON Lines
slime translate -i ./MeuJogo --dry-run
slime translate -i ./MeuJogo --json

# Reaplicar traduções em cache offline ou trocar de provedor preservando revisões/glossário
slime translate -i ./MeuJogo --memory-only -s ja -d pt
slime translate -i ./MeuJogo --switch-engine --service deepl -s ja -d pt

# Verificar integridade dos arquivos traduzidos ou adotar uma tradução externa existente
slime verify ./MeuJogo --original ./MeuJogoOriginal
slime adopt ./MeuJogo --original ./MeuJogoOriginal -e rpgmaker -s ja -d pt

# Traduzir nomes de arquivos de mídia ou gerenciar a Memória de Tradução SQLite (TMX e glossário)
slime filenames -i ./MeuJogo/www/img/pictures -s ja -d pt
slime tm
slime tm export --src ja --tgt pt --out backup.tmx
slime tm import --file backup.tmx --status reviewed
slime tm glossary --src ja --tgt pt --source "勇者" --target "Herói"
```

### Comandos do Taskfile

```sh
task              # Compila bin/slime para o sistema operacional atual
task build:all    # Compilação cruzada Linux (amd64/arm64) e Windows (.exe) com CGO_ENABLED=0
task test         # Executa a suíte de testes unitários com detector de corrida (-race)
task fmt          # Formata o código-fonte Go
task tidy         # Atualiza e limpa dependências do go.mod e go.sum
task clean        # Remove artefatos compilados
```

### Documentação Técnica

- [Arquitetura Go](docs/ARCHITECTURE_GO.md) e [Visão Geral da Arquitetura](ARCHITECTURE.md)
- [Paridade de Funcionalidades Python → Go](docs/PYTHON_PARITY.md)
- [Escopo do Produto e Compatibilidade](PRODUCT.md)
- [Guia de Códigos de Eventos do RPG Maker](docs/engines/rpg_maker.md)

# ⚙️ Variáveis de Ambiente

| Variável | Descrição | Padrão |
| :--- | :--- | :--- |
| `OLLAMA_HOST` | Endpoint HTTP do servidor Ollama local ou remoto | `http://localhost:11434` |
| `OLLAMA_MODEL` | Modelo do Ollama utilizado na tradução | `qwen2.5` |
| `OLLAMA_MAX_REQUESTS` | Número máximo de requisições concorrentes ao Ollama | `10` |
| `OLLAMA_CONTEXT_FUNCTION` | Instrução principal customizada enviada ao Ollama | `-` |
| `OLLAMA_CONTEXT_ADDITIONAL` | Instruções adicionais de estilo e preservação de tags no Ollama | `-` |
| `DEEPL_API_KEY` | Chave oficial da API DeepL (Free `:fx` ou Pro) | `-` |
| `USE_PROXY_POOL` | Ativa a rotação automática de proxies no Google Translate | `true` |
| `DISABLE_PROXY_POOL` | Força a desativação do pool de proxies quando `true` | `false` |
| `HTTP_PROXY` / `HTTPS_PROXY` | Proxy HTTP/HTTPS customizado lido pelo runtime Go | `-` |
| `SLIME_LANG` | Sobrescreve o idioma da CLI/TUI/GUI (`pt-BR` ou `en`) | Idioma do sistema |
| `SLIME_DATA_DIR` | Diretório base customizado para dados persistentes | XDG / AppData `slime-rpg` |
| `SLIME_CONFIG_DIR` | Diretório customizado para preferências (`settings.db`) | XDG / AppData `slime-rpg` |
| `SLIME_CACHE_DIR` | Diretório customizado para arquivos de cache | XDG / AppData `slime-rpg` |
| `SLIME_LOG_DIR` | Diretório customizado para arquivos de log | XDG State / `logs` |
| `SLIME_MEMORY_DB` | Caminho explícito para o arquivo SQLite `memory.db` | `<SLIME_DATA_DIR>/memory.db` |
| `SLIME_SETTINGS_DB` | Caminho explícito para o arquivo SQLite `settings.db` | `<SLIME_CONFIG_DIR>/settings.db` |
| `SLIME_LOG_FILE` | Caminho explícito para o log `processamento.log` | `<SLIME_LOG_DIR>/processamento.log` |

# 🤝 Contribuidores

<a href="https://github.com/BGLuis/slime-RPG/graphs/contributors">
  <picture><source media="(prefers-color-scheme: dark)" srcset="https://shieldcn.dev/contributors/BGLuis/slime-RPG.svg?title=false&preset=transparent&border=false&mode=dark" /><img alt="Contribuidores" src="https://shieldcn.dev/contributors/BGLuis/slime-RPG.svg?title=false&preset=transparent&border=false&mode=light" /></picture>
</a>

# 📄 Licença

Distribuído sob a licença [MIT](LICENSE).

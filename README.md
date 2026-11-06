# Slime RPG (Go Edition)

[![Go Version](https://img.shields.io/badge/go-1.27+-00ADD8.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

O **Slime RPG** é uma ferramenta de alta performance para extração, mascaramento de variáveis/tags, tradução assistida por IA/APIs e reinjeção de textos em jogos e datasets estruturados (RPG Maker, Wolf RPG, CSV, JSON).

Esta branch (`feat/go-port`) implementa a reescrita completa do projeto em **Go puro (Zero CGO)**:
- **Executáveis leves e autocontidos** (~15MB a 25MB), sem necessidade de runtime Python ou dependências externas instaladas.
- **Zero CGO**: compilação cruzada nativa instantânea para Linux e Windows.
- **Interface Desktop moderna com MyGo** ([`egoist/mygo`](https://github.com/egoist/mygo)): webview integrada com IPC fortemente tipado, design system Figma (WCAG 2.2 AA) e stepper de 4 passos.
- **Terminal User Interface (TUI) rica com Bubble Tea** ([`charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea) + Lip Gloss).
- **Motores & Formatos Suportados**:
  - RPG Maker MV / MZ (`www/data/*.json`)
  - RPG Maker VX Ace / VX / XP (`Data/*.rvdata2`, `*.rvdata`, `*.rxdata` via parser Ruby Marshal 4.8 em Go puro)
  - WOLF RPG (`Data/BasicData`, `.mps` com LZ4 e criptografia LCG)
  - Planilhas CSV tabulares com sniffing de delimitadores e CRLF/BOM
  - JSON genérico com navegação via ponteiros RFC 6901
- **Provedores de Tradução**:
  - Google Translate (com controle de requisições e rotação inteligente de proxies)
  - DeepL API (REST oficial, auto-detecção Free vs Pro)
  - Ollama (LLM local privada e offline)
- **Memória de Tradução SQLite**: persistência ultra-rápida via `modernc.org/sqlite` com WAL mode, FTS5 e precedência de estados (`locked` > `reviewed` > `machine`).

---

## Pré-requisitos

- [Go](https://go.dev/dl/) 1.27 ou superior
- [Task](https://taskfile.dev) (gerenciador de tarefas em Go)
  ```bash
  go install github.com/go-task/task/v3/cmd/task@latest
  ```

---

## Comandos Principais (`Taskfile`)

Utilizamos o **Taskfile** (alternativa ao `Make` e `Justfile` baseada em Go):

```bash
# Listar todas as tarefas disponíveis
task

# Compilar o binário para o seu sistema operacional atual (bin/slime)
task build

# Compilar binários para Linux e Windows (.exe) sem CGO
task build:all

# Executar a aplicação (abre a TUI interativa automaticamente no terminal)
task run

# Executar a interface de terminal interativa (TUI)
task run:tui

# Executar a interface desktop nativa com webview (MyGo)
task run:gui

# Executar a suíte de testes unitários com detecção de corrida de dados
task test

# Formatar o código-fonte
task fmt

# Atualizar e limpar dependências do go.mod
task tidy

# Limpar artefatos de build
task clean
```

---

## Uso via Linha de Comando (CLI)

```bash
# Iniciar a TUI interativa na pasta atual
slime

# Iniciar a janela desktop nativa
slime --gui

# Tradução headless automática
slime translate -i ./Meujogo --service google --from ja --to pt --incremental

# Simulação pré-voo (Dry-Run) sem alterar arquivos
slime translate -i ./Meujogo --dry-run

# Saída estruturada em JSON Lines (para automação e pipelines)
slime translate -i ./Meujogo --json

# Verificar integridade dos arquivos traduzidos
slime verify -i ./Meujogo

# Consultar estatísticas da Memória de Tradução SQLite
slime tm
```

---

## Estrutura do Projeto

```text
├── cmd/
│   └── slime/              # Ponto de entrada da aplicação (CLI, TUI, Desktop MyGo)
├── pkg/
│   ├── detector/           # Detecção automática de engine e idioma
│   ├── event/              # Barramento de eventos desacoplado com envelopes
│   ├── extractor/          # Extratores (JSON MV/MZ, Ruby Marshal, Wolf RPG, CSV)
│   ├── job/                # Orquestrador central e snapshot thread-safe
│   ├── memory/             # TranslationMemory (SQLite pure-go, FTS5)
│   ├── model/              # Modelos imutáveis de domínio (Segment)
│   ├── paths/              # Caminhos XDG/AppData nativos e portáteis
│   ├── pipeline/           # Pipeline de mascaramento (__XTOK_<n>__) e integridade
│   ├── provider/           # Provedores (Google, DeepL, Ollama, Proxies)
│   ├── ui/                 # Interfaces de Usuário
│   │   ├── desktop/        # Desktop UI nativa com MyGo (HTML/CSS/JS embutido)
│   │   ├── theme/          # Tokens e paleta WCAG 2.2 AA (Lip Gloss)
│   │   └── tui/            # Terminal UI interativa (Bubble Tea)
│   └── workspace/          # Workspace atômico (os.Rename, os.Link)
├── Taskfile.yml            # Automação de tarefas
├── go.mod / go.sum         # Módulos Go gerenciados
└── GEMINI.md               # Diretrizes e arquitetura para agentes de IA
```

---

## Licença

Distribuído sob a licença [MIT](LICENSE).

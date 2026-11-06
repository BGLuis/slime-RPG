# Slime RPG (Go Edition)

[![Go Version](https://img.shields.io/badge/go-1.27+-00ADD8.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

O **Slime RPG** é uma ferramenta de alta performance para extração, mascaramento de variáveis/tags, tradução assistida por IA/APIs e reinjeção de textos em jogos e datasets estruturados (RPG Maker, Wolf RPG, CSV, JSON).

Esta branch (`feat/go-port`) implementa a reescrita do projeto em **Go**, visando:
- **Executáveis leves e autocontidos** (~15MB a 25MB), sem necessidade de runtime Python instalado.
- **Zero CGO**: compilação cruzada nativa instantânea para Linux e Windows.
- **Interface Desktop moderna com MyGo** ([`egoist/mygo`](https://github.com/egoist/mygo)): webview integrada com IPC tipado.
- **Terminal User Interface (TUI) rica com Bubble Tea** ([`charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea)).
- **Concorrência massiva**: Goroutines para I/O não-bloqueante e streaming de arquivos.
- **Memória de Tradução SQLite**: persistência ultra-rápida via `modernc.org/sqlite` com WAL mode.

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

# Executar a aplicação
task run

# Executar a interface de terminal interativa (TUI)
task run:tui

# Executar os testes unitários
task test

# Formatar o código-fonte
task fmt

# Atualizar e limpar dependências do go.mod
task tidy

# Limpar artefatos de build
task clean
```

---

## Estrutura do Projeto

```text
├── cmd/
│   └── slime/              # Ponto de entrada da aplicação (CLI, TUI, GUI)
├── pkg/                    # Pacotes internos do motor (em desenvolvimento)
│   ├── pipeline/           # Passos ordenados de mascaramento e tradução
│   ├── memory/             # TranslationMemory (SQLite pure-go)
│   ├── extractor/          # Extratores (JSON, CSV, RPG Maker, Wolf RPG)
│   └── provider/           # Provedores de tradução (Google, DeepL, Ollama)
├── Taskfile.yml            # Definição das tarefas de build e automação
├── go.mod / go.sum         # Módulos Go gerenciados
├── GEMINI.md               # Diretrizes e arquitetura para agentes de IA
└── .ai-memory.toml         # Configuração de continuidade cross-session
```

---

## Licença

Distribuído sob a licença [MIT](LICENSE).

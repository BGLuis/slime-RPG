# Diretrizes do Projeto: Slime RPG (Go Edition)

Contexto específico da branch de portabilidade para Go (`feat/go-port`). As regras gerais de trabalho ficam no `GEMINI.md` global; aqui entra o que vale para este projeto.

## Visão Geral da Arquitetura (Go)
Este projeto é uma ferramenta modular desenvolvida em Go para extrair, mascarar variáveis (códigos protegidos), traduzir e reinjetar textos em jogos e formatos estruturados (RPG Maker, Wolf RPG, CSV, JSON).

A arquitetura em Go adota:
1. **Desktop UI (`egoist/mygo`)**: Framework em pure-Go (zero CGO) com suporte a webview nativa do SO e IPC fortemente tipado com TypeScript.
2. **Terminal UI (`charmbracelet/bubbletea` + `bubbles` + `lipgloss`)**: TUI reativa baseada em arquitetura Elm e orientada a mensagens concorrentes assíncronas (`tea.Msg`).
3. **Task Runner (`Taskfile.yml`)**: Automação padrão via `task` (compilação, execução, testes, formatação e cross-compile sem CGO).
4. **Pipeline / Chain of Responsibility (`pkg/pipeline`)**: Orquestra os passos: Cache Lookup -> Get Patterns -> Mask -> Translate -> Unmask -> Fix -> Cache Store.
5. **Translation Memory (`pkg/memory`)**: Persistência SQLite local em Go puro via `modernc.org/sqlite` com WAL mode e FTS5.
6. **Concorrência com Goroutines e Channels**: Substituição dos pools de threads manuais do Python por canais e goroutines com controle de contexto (`context.Context`).

## Convenções do Projeto
- **Padrão Go**: Nomenclatura idiomática (`camelCase` / `PascalCase`), tratamento explícito de erros com `if err != nil`, pacotes focados em `pkg/` e executáveis em `cmd/`.
- **Git & Commits**: Conventional Commits em inglês (ex: `feat:`, `fix:`, `refactor:`, `docs:`, `chore:`).
- **Sem CGO**: Manter o projeto compatível com compilação estática/cross-compilation pura sem dependência de toolchains C/C++.
- **ai-memory**: Escopo do projeto configurado no `.ai-memory.toml` (`workspace = "rpg-tools"`, `project = "slime-rpg"`).

## Orçamento de Tokens
Respeite os limites de contexto. Se a sessão acumular ruídos, resuma o contexto e seja objetivo.

# Paridade com a versão Python

Inventário do que a versão Python (`main`, commit 8d32a04) faz e do estado de cada item no port Go (`feat/go-port`). Serve de checklist para aposentar a versão Python.

Legenda: ✅ paridade · ⚠️ parcial ou diferente · ❌ falta no Go · ➖ não será portado (com motivo)

## 1. Extratores

| Item (Python) | Onde no Python | Go |
|---|---|---|
| RPG Maker MV/MZ: `data/*.json` | `src/extractor/rpgmaker/RPGMakerExtractor.py` | ✅ verificado no jogo *Holy Astel*: os mesmos 7254 valores alterados |
| RPG Maker: 19 códigos de evento (101, 102, 105, 122, 205, 320, 324, 325, 355, 356, 357, 401, 402, 405, 505, 655…) | `RPGEventStrategy.py`, `RPGEventCodes.py` | ✅ |
| RPG Maker: `js/plugins.js` (incluindo JSON serializado em parâmetros) | `FormatAdapter.py` | ✅ |
| RPG Maker VX Ace `.rvdata2` | `RVDataAdapter.py`, `utils/RubyMarshal.py` | ✅ (o Go também lê `.rvdata` e `.rxdata`) |
| Wolf RPG: arquivos soltos | `src/extractor/wolfrpg/` | ✅ |
| Wolf RPG: desempacotar `.wolf` (DXArchive, Huffman) | `WolfArchive.py`, `huffman.py` | ✅ `pkg/extractor/wolfrpg/dxa`. Saída byte a byte igual à do Python num `BasicData.wolf` real. Leitura sob demanda; a pasta do jogo não é alterada (o Python renomeava o `.wolf` para `.bak`). |
| Wolf RPG: reempacotar `Data.wolf` | `WolfRPGExtractor.py` | ⚠️ `--opt wolf.repack_mode=true`. O Go grava com Huffman e LZ legíveis pelo leitor do Python; o Python gravava com um parâmetro que o próprio leitor dele não abre acima de 32 KB. Não foi testado se o motor do jogo aceita o arquivo gerado. |
| Wolf RPG: manter título original | `WolfRPGExtractor.py` | ✅ `--opt wolf.preserve_title` (padrão true) |
| Wolf RPG: fonte alvo | `WolfRPGExtractor.py` | ✅ `--opt wolf.target_font`. O Go aplica a fonte mesmo quando o título é preservado, o que o Python não fazia. |
| Wolf RPG: criptografia 0x13A (ex.: `銀眼のライトナ/Data.wolf`) | — | ➖ nenhuma das duas versões abre esse arquivo |
| CSV: auto-detecção, `columns`, `delimiter` | `CSVExtractor.py` | ✅ `--opt csv.columns`, `--opt csv.delimiter`. No Go o delimitador informado é usado de fato; no Python costumava ser sobrescrito pela detecção automática. |
| JSON genérico: `keys` | `JsonExtractor.py` | ✅ `--opt json.keys` |
| Escolher o extrator (`-e`) | `main.py` / `src/cli.py` | ✅ `-e/--extractor`, com os aliases do Python |
| Correção pós-tradução (`fix_text_translate`) | `RPGMakerExtractor.py:1045-1113` | ✅ o Go evita o dano mascarando os códigos antes de traduzir. Coberto por um corpus com saídas reais do Python (`pkg/job/testdata/python_fix_reference.json`). |
| Bug do Python: rebaixa `\F`, `\FH`, `\FF` para minúsculo | `RPGMakerExtractor.py:1059` | ➖ o Go não tem o bug; o estrago já gravado na memória sai com `slime tm repair` |
| Bug do Python: cria `plugins.js` solto na raiz do jogo | `RPGMakerExtractor.py:331-345` | ➖ o Go não tem o bug |

## 2. Tradutores

| Item | Onde no Python | Go |
|---|---|---|
| Google gtx, depois o endpoint web, depois MyMemory | `GoogleTranslate.py:265` | ✅ mesma ordem e mesmas regras de bloqueio por limite de requisições. Diferença deliberada: se tudo falha, o Go devolve erro (o engine tenta de novo) em vez de gravar o texto original como tradução. O HTML do endpoint web não foi conferido com um IP liberado. |
| Pool de proxies | `services/ProxyManager.py` | ✅ |
| DeepL | `DeeplTranslate.py` | ✅ chave pela interface desktop ou por `DEEPL_API_KEY` |
| Ollama | `OllamaTranslate.py` | ✅ |
| Lotes, deduplicação, retry, circuit breaker | `BaseTranslate.py`, `TranslationEngine.py` | ✅ |

## 3. Memória de tradução (SQLite)

| Item | Python | Go |
|---|---|---|
| Esquema, hash, nomes de motor, arquivo padrão `~/.local/share/slime-rpg/memory.db` | `TranslationMemory.py` | ✅ idênticos; as duas versões compartilham o banco |
| Glossário, TMX | `scripts/tm_*.py` | ✅ `slime tm glossary/export/import` |
| Migrar caches JSON antigos | `scripts/migrate_cache_to_db.py` | ✅ `slime tm migrate-legacy --dir <cache>` (idempotente) |
| Limpar placeholders `__XTOK_` gravados | `scripts/clean_corrupted_cache.py` | ✅ `slime tm repair` |
| Normalizar `\nome`/`\name` → `\NAME[...]` na memória | `scripts/sweep_fix_name_tags.py` | ✅ `slime tm repair` |
| Restaurar a caixa dos códigos rebaixados pelo bug do Python | — | ✅ `slime tm repair` (2466 entradas numa cópia do banco real) |
| Remover entradas de motores de teste | — | ✅ `slime tm repair` |

`slime tm repair` só simula por padrão. Com `--apply`, cria antes um backup `<db>.bak-<data>`.

## 4. Ciclo de vida, CLI e interfaces

| Item | Python | Go |
|---|---|---|
| `.slime/meta.json`, adopt, verify, incremental, backup | `services/GameMetadata.py`, `job.py` | ✅ |
| `--memory-only`, `--switch-engine`, `--synopsis` | `cli.py` | ✅ |
| Tradução de nomes de arquivo | `services/FileNameTranslator.py` | ✅ `slime filenames` |
| Pausa para verificação antes de gravar | `job.py` (padrão; `--no-verify` desliga) | ✅ `--review`, opcional, para não travar automação e `--json` |
| Modo interativo no terminal | `cli.py` (Textual) | ⚠️ a TUI roda o job de verdade (provedor, idiomas, incremental, proxies, opções do extrator, chave DeepL, modo só-memória; progresso, pausa/cancelamento, revisão e resultado com erros). Só abre o jogo da pasta atual (sem digitar/navegar caminho), os idiomas não são editáveis e o painel de estimativa da tela Configurar ainda é fixo. Testada com provedor falso, não numa sessão real de terminal. |
| GUI com formulário de opções por extrator | `gui.py` | ✅ desktop (MyGo). Não foi aberto numa janela real. |
| Idioma da interface PT/EN | `i18n.py` | ❌ só PT |
| Preferências persistidas | `services/SettingsStore.py` | ❌ |

## 5. Instalação e distribuição

| Item | Python | Go |
|---|---|---|
| Atalhos `slime`, `slm`, `sl` e `add-alias`/`remove-alias`/`list-aliases` | `Makefile` | ✅ `task install-cli`, `task add-alias NAME=x` |
| Atalho de desktop e "Traduzir jogo" no menu de pastas (Nemo, Nautilus, Dolphin) | `scripts/install_linux_integration.sh` | ✅ `task install-desktop`. Os menus de contexto não foram testados nos gerenciadores de arquivos. |
| Dockerfile e docker-compose | raiz do repositório | ✅ imagem de 40 MB, só CLI |

## 6. Não portado

| Script | Motivo |
|---|---|
| `analyze_code.py`, `compare_json.py`, `verify_inputs.py` | Ferramentas de depuração do desenvolvimento em Python. |
| `translate_ska.py` | Script avulso para um jogo específico. |
| `repair_corrupted_variables.py`, `sweep_fix_name_tags.py` (parte dos arquivos do jogo) | Consertam jogos já estragados por versões antigas do Python. **Decisão pendente:** portar como `slime repair` ou retraduzir esses jogos com o Go. |

## Decisões em aberto

- Aspas tipográficas: o Go troca “ ” por " também no texto de diálogo dentro de scripts 355 e de comandos de plugin; o Python só troca dentro de código. O resultado é JS válido; muda só a tipografia.
- Os códigos `\<` e `\>` (exibição instantânea do RPG Maker) não são protegidos nas duas versões.

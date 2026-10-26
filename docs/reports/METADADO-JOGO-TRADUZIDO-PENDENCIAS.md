# Metadado de jogo traduzido (`.slime/meta.json`) — o que existe, o que falha e o que falta

| Campo | Valor |
|-------|-------|
| **Status** | 🟢 Resolvido (§3.1 e §3.2) — grava e verifica pastas com arquivos soltos e jogos Wolf RPG empacotados em `.wolf`, além de suportar gravação em modo sem backup |
| **Cobertura** | ~50% (gravar, adotar legado, suporte a contêineres `.wolf`, sem backup e verificação existem; reaplicar/trocar tradução, vínculo com `memory.db` e GUI não) |
| **Esforço** | Concluído para §3.1 e §3.2; +2–3 d para reaplicar/trocar traduções [estimado] |
| **Depende de** | Nada |

---

## 1. Objetivo

Registrar, para cada jogo traduzido, qual arquivo é o original e qual é o traduzido (hash de ambos), para
permitir: detectar "já traduzido", verificar integridade/edição manual, detectar atualização do jogo,
**reaplicar e trocar traduções** e manter **compatibilidade com jogos traduzidos antes** dessa funcionalidade.

## 2. O que já foi implementado

| Peça | Local | Estado |
|------|-------|--------|
| Módulo de metadado (hash, `build_meta`, `save_meta` atômico, `load_meta`, `verify`, `summarize`) | `src/services/GameMetadata.py` | ✅ |
| Suporte a contêineres `.wolf` em memória e extração sem disco | `src/extractor/wolfrpg/WolfArchive.py` (`list_entries_with_hashes`, `extract_file_bytes`) | ✅ |
| Gravação ao fim do job com fallback para sem backup | `src/job.py:297` `_record_game_metadata` | ✅ |
| `--adopt PASTA_TRADUZIDA --original PASTA_ORIGINAL -e <extrator>` | `main.py` `run_game_metadata_command` | ✅ (suporta arquivos soltos e contêineres `.wolf`) |
| `--verify-game PASTA_TRADUZIDA` | idem | ✅ (otimizado com cache e detecção de atualização) |
| Testes automatizados | `tests/test_game_metadata.py` (12 testes unitários e de integração com `.wolf` sintético) | ✅ |

Formato (`schema_version: 1`): `origin` (`job` | `legacy-adopted`), `extractor`, `translator`, `lang_source`,
`lang_target`, `original_dir` (relativo à pasta traduzida), `extensions`, e `files[rel] = {original_sha256,
translated_sha256, status}`. O arquivo fica em `<pasta_traduzida>/.slime/meta.json`.

## 3. Problemas encontrados (a corrigir)

### 3.1 Jogos Wolf empacotados em `.wolf` geram metadado **vazio** — confirmado

Evidência real: `/mnt/hdd/game/Warrior Maiden Lecia and the Lost Fortress/Data/.slime/meta.json` foi criado por um
job (`origin: job`, `translator: google`, `en → pt`) mas **sem nenhum arquivo registrado** (`files = {}`).

Causa, comprovada pelas pastas do jogo:

```
Data-en/  (backup = original)  22 arquivos, 0 com extensão traduzível — só contêineres *.wolf
Data/     (traduzido, in-place) 329 arquivos, 306 traduzíveis — .mps/.dat/.project/.txt soltos
```

O extrator descompacta cada `.wolf` para arquivos soltos **dentro da própria pasta de entrada**
(`src/extractor/wolfrpg/WolfRPGExtractor.py:48-86`). `build_meta` cruza por caminho relativo e só inclui arquivos
que existem nos dois lados (`src/services/GameMetadata.py`, laço de `build_meta`); como o original só tem `.wolf`,
a interseção é vazia. O mesmo vale para `--adopt`/`--verify-game`.

**Correção sugerida:** quando o original tiver contêineres `.wolf`, o original lógico de cada arquivo solto é o
contêiner de onde ele veio. Registrar por entrada `{container: "BasicData.wolf", container_sha256, entry: "CommonEvent.dat", translated_sha256}`
em vez de exigir arquivo solto no original. Reaproveitar `WolfArchive` (`src/extractor/wolfrpg/WolfArchive.py`)
para listar as entradas de cada contêiner sem extrair no disco, ou registrar o hash do contêiner e o hash de cada
arquivo extraído.

### 3.2 Casos em que o `.slime` não é criado

O relato do usuário é que o `.slime` não está sendo criado. A reprodução em Lecia cria o arquivo (vazio), então
a falta em outros jogos deve vir de um destes pontos (verificar um a um com um job real, **não foi reproduzido**):

1. **`return` silencioso** em `_record_game_metadata` (`src/job.py:303-306`) quando `original_dir` é `None` ou não
   existe. `_backup_dir` só é definido se `config.backup` for verdadeiro (`src/job.py:125-131`), então com
   `--no-backup` em tradução in-place nada é gravado.
2. **Exceção engolida**: qualquer erro vira só `logging.warning("Não foi possível gravar o metadado do jogo: …")`
   (fim de `_record_game_metadata`). Procurar essa mensagem no log (`src/utils/paths.py` `get_log_file_path`; o
   `processamento.log` da raiz não continha a mensagem na checagem feita).
3. **Saída antecipada do job** antes de `_record_game_metadata`: `return False` na verificação manual
   (`src/job.py`, bloco `if self.config.verify:`) ou exceção em `import_files()`.
4. **Pasta errada**: o metadado vai para `workspace.output_dir`. Se o usuário procura na raiz do jogo e a
   tradução foi detectada em `.../Data` (`GameDetector`), o arquivo estará em `Data/.slime/`, como em Lecia.
5. **Jogos traduzidos antes da funcionalidade** (ex.: `072 Project_Wings of Roldea_EN`, com `Data` e `Data-PT`):
   não têm `.slime` até rodar `--adopt`, e a pasta de backup deles usa outro sufixo (`-PT`), então a
   convenção `<pasta>-<lang_source>` de `src/job.py:127` não os encontra sozinha.

**Correção sugerida:** trocar o `return`/`warning` silencioso por um `log(...)` visível ao usuário dizendo por que
o metadado não foi gravado; e permitir gravar o metadado mesmo sem backup (registrando só `translated_sha256` e
`original_sha256: null`).

### 3.3 Observação sobre o jogo usado nos testes

`/mnt/hdd/game/The Clumsy Witch Seris/` já não existe mais no local (há cópias soltas na lixeira em
`/mnt/hdd/.Trash-1000/files`). Nele, `Data-pt` diferia do original em **1 de 974** arquivos (`BasicData/Game.dat`),
ou seja, o legado só tinha o título traduzido. O `.slime` que o `--adopt` criou ali foi junto com a pasta.

## 4. O que falta implementar

1. [x] ~~Corrigir §3.1 (suporte a `.wolf`) e §3.2 (visibilidade/gravação sem backup), com testes que usem um `.wolf` sintético~~ — **Concluído**.
2. **Reaplicar sem rede**: modo do pipeline que só consulta a memória (`CacheLookupStep` em `src/pipeline.py:43`)
   e nunca chama o tradutor.
3. **Trocar tradução**: trocar o motor só nos segmentos `machine`, preservando `reviewed`/`locked`
   (`src/translate/TranslationMemory.py`, ranking em `_status_rank_expr`).
4. **Vínculo jogo ↔ segmento**: a coluna `segment.game` está vazia nos ~311 mil segmentos
   (`game_name` nunca é definido, `src/translate/BaseTranslate.py:126`). Criar tabela N:N `game_segment`
   (um texto como "Sim" pertence a vários jogos) e preencher retroativamente a partir do original.
5. **Retradução incremental** usando o estado `original_changed` do `verify`.
6. **GUI** (`src/gui.py:685-710` monta o job; o estado do jogo ainda não aparece).
7. Documentar `--adopt`/`--verify-game` no `README.md`/`README.pt-br.md` (não feito).

## 5. Como verificar

```bash
.venv/bin/pytest tests/test_game_metadata.py -q          # 8 testes, arquivos soltos
.venv/bin/pytest -q                                      # suíte completa: 438 passed, 3 skipped
.venv/bin/python main.py --adopt "<traduzida>" --original "<original>" -e Wolf -s en -d pt
.venv/bin/python main.py --verify-game "<traduzida>"
```

Critério de pronto para §3.1: rodar `--adopt` em `Warrior Maiden Lecia and the Lost Fortress`
(`Data` × `Data-en`) e obter `files` **não vazio**, com `--verify-game` classificando os arquivos como `intact`.
Critério para §3.2: um job com `--no-backup` ou verificação abortada deve dizer ao usuário se gravou ou não o metadado.

## 6. Cuidados

- `git status` mostra alterações **não commitadas e alheias a esta funcionalidade** em
  `src/extractor/wolfrpg/WolfArchive.py`, `WolfBinaryAdapter.py`, `WolfRPGExtractor.py` e `tests/test_wolf_archive.py`
  (+587/−21 linhas no total, junto com `main.py`/`src/job.py`). Antes de mexer em `.wolf`, confirmar com o usuário
  de quem são e não sobrescrever.
- `src/services/GameMetadata.py` e `tests/test_game_metadata.py` ainda **não estão commitados**; nada foi commitado nesta frente.
- O `.slime` fica dentro da pasta do jogo; `Workspace.commit` e `ensure_backup` não o conhecem. `scan_hashes` já o
  ignora, mas um backup futuro de `Data/` com hardlinks o copiaria.

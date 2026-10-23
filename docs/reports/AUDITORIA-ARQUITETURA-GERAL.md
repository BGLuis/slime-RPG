# Análise Técnica — Arquitetura, performance e qualidade do Slime

**Data:** 2026-09-27 · **Branch:** `main` · **Commit base:** `4973016` + working tree (`ProxyManager.py` e `GoogleTranslate.py` modificados, não commitados)
**Escopo:** `main.py`, `src/**` (extratores, tradutores, pipeline, serviços, CLI/GUI) e `tests/`
**Relatório irmão:** [`INSTALACAO-CLI-TERMINAL.md`](./INSTALACAO-CLI-TERMINAL.md)

---

## 1. Sumário executivo

O Slime extrai texto de jogos (RPG Maker MV/MZ/VX Ace, WOLF RPG, JSON e CSV genéricos), traduz por
Google (endpoint `gtx`), DeepL ou Ollama, com memória de tradução em SQLite, e regrava os arquivos.
São ~13.200 linhas em `src/` + `main.py` e 256 testes. A leitura foi feita com uma pergunta fixa:
**quanto custa adicionar um jogo, um provedor ou uma forma nova de traduzir?** Hoje, custa editar o
núcleo em três a cinco lugares — e a unidade de trabalho (o arquivo, não o segmento de texto) impede
as formas de tradução mais valiosas: LLM com contexto, revisão humana e troca de provedor.

| # | Problema | Alcance | Severidade |
|---|---|---|---|
| Q-01 | `ProxyManager.get_proxy_dict` entra em **deadlock** (working tree); a suíte trava | Google + pool de proxies | Crítica |
| Q-02 | `fix_text_translate` do RPG Maker troca "falso/verdadeira/nula" por `false/true/null` em diálogo | Todo RPG Maker → PT/ES/FR | Crítica |
| A-07 | Não existe modelo de segmento: extrator devolve árvore ad-hoc casada por posição | Toda extensão futura | Alta |
| A-04 | Provedor de tradução carrega cache, batching, retry e classificação de erro | Todo provedor novo | Alta |
| A-05 | Memória de tradução ignora provedor e jogo: trocar de provedor não retraduz | Todas | Alta |
| Q-03 | Jogos WOLF distribuídos como `Data.wolf` **nunca** são desempacotados | WOLF RPG | Alta |
| P-01 | Um lote por arquivo, sem deduplicação global: 20 arquivos → 20 requisições | Todos os provedores | Alta |

**Veredicto:** o pipeline de mascaramento, a memória SQLite e a fast lane são trabalho sólido, mas
estão presos a um esqueleto que conhece cada formato pelo nome e trata tradução como
`list[str] → list[str]`. Antes de adicionar o próximo motor ou provedor, o núcleo precisa de três
contratos explícitos — formato, segmento e provedor — senão cada extensão aumenta o acoplamento.

---

## 2. Metodologia e limites

### 2.1 O que foi feito

- Leitura integral de `main.py`, `factory.py`, `pipeline.py`, `BaseExtractor.py`, `BaseTranslate.py`,
  `TranslationMemory.py`, dos três provedores, de `ProxyManager.py`, `GameDetector.py`,
  `FileNameTranslator.py`, `TextsUtils.py`, `JsonExtractor.py`, `CSVExtractor.py`,
  `RPGMakerExtractor.py` e `WolfRPGExtractor.py`; leitura estrutural (assinaturas) de `gui.py`,
  `cli.py`, strategies e adapters binários.
- Execução da suíte e de scripts de reprodução numa **cópia** do repositório (`git archive HEAD` +
  os dois arquivos modificados) no diretório temporário, com o interpretador de `.venv`.
- Instrumentação de `json.load`/`json.loads`/`json.dumps` e de `_translate_single_batch` com um
  provedor falso (maiúsculas), rodando o fluxo real `process_files → import_files`.

### 2.2 O que NÃO foi feito — limites desta análise

> Nenhum jogo real foi processado e nenhum provedor real (Google, DeepL, Ollama) foi chamado. A GUI
> não foi aberta. `WolfArchive.py`, `huffman.py`, `WolfBinaryAdapter.py` (≈ 3.750 linhas de
> formato binário) não foram auditados linha a linha. **Nenhum tempo de parede foi medido** além do
> tempo total da suíte; todo custo de tempo neste relatório é `[modelado]`.

Protocolo das medições da seção 3: Linux 7.2.6 (Arch), Python 3.14.7, pytest 9.1.1; contagens de
chamadas são determinísticas (n = 2 execuções: cache frio e cache quente); o tempo da suíte é n = 1 e
só indicativo. Recurso limitante das contagens: nenhum — são propriedades do fluxo de controle.

---

## 3. Evidências medidas

### 3.1 A suíte trava no working tree atual

```bash
python -m pytest -v            # para em:
# tests/test_proxy_and_deepl.py::TestProxyManagerAndDeepl::test_proxy_manager_rotation  (timeout 90 s)
python -m pytest -q --deselect <o teste acima>
# 1 failed, 252 passed, 2 skipped, 1 deselected in 7.71s
```

A falha restante (`test_cli_aliases.py::test_slime_script_list_extractors`) é artefato da cópia:
`scripts/slime:71-76` cai no `python3` do sistema quando `.venv` não existe, e o sistema não tem
`rubymarshal`. Não é defeito do código, mas mostra que o teste depende do ambiente local (Q-10).

### 3.2 Cada arquivo JSON é lido seis vezes e serializado duas

| Fluxo (20 × `MapNNN.json`, 30 falas cada) | `json.load` | `json.loads` de validação | `json.dumps` | Requisições ao provedor |
|---|---|---|---|---|
| Cache frio | 80 (4 por arquivo) | 40 (2 por arquivo) | 40 (2 por arquivo) | 20 |
| Cache quente | 60 (3 por arquivo) | 40 (2 por arquivo) | 40 (2 por arquivo) | 0 |

Origem: instrumentação descrita em 2.1. As 20 requisições para ~20 mil caracteres mostram que o
lote nunca atravessa a fronteira do arquivo (P-01).

### 3.3 A correção pós-tradução corrompe diálogo comum

```text
entrada → 'Isso é falso, a história é verdadeira.' | 'A resposta foi nula.' | 'Ele disse "  " e saiu'
saída   → 'Isso é false, a história é true.'       | 'A resposta foi null.'  | 'Ele disse "" e saiu'
```

Chamada direta a `RPGMakerExtractor.fix_text_translate` com originais em inglês sem nenhum código.

### 3.4 O CSV traduzido muda de esquema

Entrada `id,text` sem BOM → saída `﻿id,text,text_old`. Fluxo real com provedor falso.

### 3.5 Verificação negativa: `Items.json` do RPG Maker NÃO recebe chaves `*_old`

`merge_dicts_texts` cria `name_old` (`BaseExtractor.py:189-192`), mas a saída de `Items.json` saiu
limpa (`{"id":1,"name":"POTION","description":"HEALS A BIT",…}`). A hipótese de vazamento no RPG
Maker está **refutada**; o vazamento existe só no CSV (Q-05).

---

## 4. Achados de arquitetura

### A-01 · `BaseExtractor` conhece cada formato concreto pelo nome — **Alta**

`src/extractor/BaseExtractor.py:55-69` + `:94-132` + `WolfRPGExtractor.py:130-158`

```python
if file_path.endswith('.rvdata2'):
    from src.extractor.rpgmaker.RVDataAdapter import RVDataAdapter
if file_path.endswith(('.mps', '.dat', '.project')):
    from src.extractor.wolfrpg.WolfBinaryAdapter import WolfBinaryAdapter
```

A classe abstrata decide leitura e escrita por extensão e importa os adapters de RPG Maker e WOLF.
O `WolfRPGExtractor` ainda sobrescreve `import_file` duplicando a mesma lógica com um caso a mais
(`has_dummy_types`). Um motor novo (Ren'Py, Unity `.assets`, Kirikiri, RPG Maker 2000 `.lmu`) exige
editar a base — que é compartilhada por todos — e herda o risco de quebrar os outros formatos.

**Reprodução:** leitura do código; `grep -n "rvdata2\|\.mps" src/extractor/BaseExtractor.py` devolve
as linhas 59, 63, 97 e 116.

**Correção:** formalizar o contrato que já existe de fato — `load_file(path)`/`save_file(path, data,
original_raw)` em `RVDataAdapter.py:35,46` e `WolfBinaryAdapter.py:1109,1157` — como um
`FormatAdapter` registrado por extensão; o extrator declara seus adapters e a base só despacha.
**Critério de aceite:** `grep -n "rvdata2\|mps\|wolf" src/extractor/BaseExtractor.py` → 0 resultados,
e um teste registra um adapter falso para `.txt` sem tocar em `BaseExtractor.py`.

---

### A-02 · Estado global e caminhos relativos ao diretório de trabalho — **Alta**

`BaseExtractor.py:14-16,71-74` + `BaseTranslate.py:20,106` + `ProxyManager.py:37` + `FileNameTranslator.py:8`

`folderInput/Process/Output` são atributos de **classe** com valor `'input'`, `'process'`,
`'output'`; `init_folder()` é `staticmethod` que limpa `BaseExtractor.folderProcess` sem olhar a
instância; o cache é `cache/memory.db` e o pool lê `proxies.txt` — tudo relativo ao `cwd`. Dois jobs
simultâneos (duas GUIs, GUI + CLI, um servidor) apagam as pastas um do outro, e o projeto não pode ser
usado como biblioteca. O defeito já foi registrado em `INSTALACAO-CLI-TERMINAL.md` e continua aberto.

**Reprodução:** rodar dois `slime` em pastas de jogo distintas a partir do mesmo diretório; o
segundo `init_folder()` apaga `process/` enquanto o primeiro grava nela (dedução da leitura; não executado).

**Correção:** um objeto `Workspace(root)` criado por job (diretório temporário por padrão) e
injetado no extrator; cache e proxies vindos de uma `Settings` com caminhos absolutos
(`platformdirs` ou o precedente `SettingsStore.py:5`, corrigido para caminho absoluto).
**Critério de aceite:** teste que roda dois jobs em paralelo com `Workspace` distintos e verifica que
as saídas dos dois estão completas; `grep -rn "= 'input'\|= 'process'\|= 'output'" src/` → 0.

---

### A-03 · Três orquestrações do mesmo fluxo; a GUI ignora a configuração dos plugins — **Alta**

`main.py:348-410` + `main.py:413-535` + `gui.py:436-478`

O fluxo "escolher extrator → tradutor → idiomas → configurar → rodar" está escrito três vezes. Os
plugins expõem suas opções como dados (`get_interactive_questions`), mas só o terminal as consome:

```bash
grep -n "get_interactive_questions" src/gui.py
# → 0 resultados
```

Na GUI não há como informar a chave do DeepL (só via `.env`), ligar o pool de proxies, escolher fonte
ou reempacotamento do WOLF, nem as colunas do CSV. Cada opção nova de plugin tem de ser lembrada em
até três lugares — e hoje já está faltando em um.

**Reprodução:** abrir `slime --gui`, escolher `deeplTranslator` sem `DEEPL_API_KEY` no ambiente e
iniciar; o erro só aparece no log do lote (`DeeplTranslate.py:63-64`) — dedução da leitura, GUI não executada.

**Correção:** uma camada de aplicação `TranslationJob(JobConfig)` única, chamada por CLI, TUI e GUI;
a GUI gera o formulário a partir do mesmo esquema de perguntas que `main.py:277-345` já interpreta.
**Critério de aceite:** `grep -n "ExtractorFactory.create\|TranslatorFactory.create" main.py src/gui.py`
devolve só chamadas dentro do módulo de aplicação; um teste da GUI (ou do gerador de formulário)
afirma que a pergunta `api_key` do DeepL vira um campo.

---

### A-04 · O contrato do provedor mistura transporte, batching, cache, retry e política — **Alta**

`BaseTranslate.py:29-50,243-374` + `pipeline.py:43-99,204-226`

```python
if cls.__name__ == 'GoogleTranslate':        # BaseTranslate.py:35
    from src.services.ProxyManager import ProxyManager
```

Para adicionar um provedor hoje é preciso herdar 380 linhas que fazem lookup e gravação de cache
(**duplicando** `CacheLookupStep`/`CacheStoreStep` do pipeline), montam lotes por delimitador,
fazem fallback item a item e classificam erros por texto. A base testa o **nome** de uma subclasse. O
método abstrato `translator()` foi copiado idêntico em três provedores e, no DeepL, chama um
`preprocess_text` que não existe (Q-07). E a interface — `list[str] → list[str]` — não tem onde
colocar o que um LLM precisa (falante, arquivo, glossário, limite de caracteres por linha) nem o que
um provedor declara (tamanho máximo de requisição, suporte a lote nativo, a tags XML, a glossário).

**Reprodução:** leitura do código; `grep -n "def translator" src/translate/*.py` → 3 cópias.

**Correção:** reduzir o provedor a `translate(segments, ctx) -> list[str]` mais um objeto
`Capabilities(max_request_chars, max_items, native_batch, tag_handling, glossary, context)`; mover
cache, lotes, retry e rate limit para um `TranslationEngine` que consulta as capacidades. O
precedente de lote nativo já está no DeepL (`DeeplTranslate.py:70-87`), que dispensa delimitador.
**Critério de aceite:** um provedor de teste com < 30 linhas passa na mesma suíte de contrato que os
três reais; `grep -n "__name__ ==" src/translate/` → 0.

---

### A-05 · A memória de tradução não sabe quem traduziu nem para qual jogo — **Alta**

`TranslationMemory.py:56-61,162-170,194-198` + `BaseTranslate.py:109`

```python
WHERE src_lang=? AND tgt_lang=? AND source_hash=?   # sem engine, sem game
```

A tabela grava `engine` e `game`, mas a busca ignora os dois; `game_name` é lido por
`BaseTranslate.py:109` e **nunca atribuído** (`grep -rn "game_name" src` → 1 resultado, a leitura).
Consequências: trocar do Google para DeepL ou Ollama devolve a tradução antiga do Google para todo
texto já visto — justamente o cenário "traduzir de outro jeito"; o mesmo "Right" de dois jogos
diferentes compartilha tradução; e `normalize_source` colapsa espaços e **quebras de linha**, então
uma fala com a quebra em outro lugar recebe o layout da primeira ocorrência gravada.

**Reprodução:** traduzir um `Map001.json` com `googleTraslator`, depois o mesmo arquivo com
`ollamaTranslator`; a segunda execução termina na fast lane com 0 requisições (dedução da leitura; não executado).

**Correção:** política de lookup explícita no job (`prefer_engine`, `scope=game|global`,
`reuse_other_engines: bool`), `game` preenchido pelo detector, e hash sobre o texto exato (a
normalização vira só uma chave auxiliar para sugestões, como já faz `fuzzy_candidates` em `:255-264`).
**Critério de aceite:** teste que grava a mesma origem com dois `engine` e afirma que o lookup com
`prefer_engine='ollama'` devolve a tradução do Ollama, e que `"A\nB"` e `"A B"` não colidem.

---

### A-06 · Detecção de jogo centralizada, com nomes de extrator como strings mágicas — **Média**

`GameDetector.py:28-195`

Uma cadeia de 170 linhas de `if` conhece a estrutura de pastas de cada motor e devolve
`'RPG Maker'`, `'Wolf RPG'`, `'Json'`, `'CSV'` — strings que precisam bater com os nomes passados a
`@register_extractor`. Um motor novo exige editar este arquivo, e a ordem dos `if` decide prioridade
sem que isso esteja escrito em lugar algum.

**Reprodução:** leitura; renomear `@register_extractor("Wolf RPG")` quebra a detecção sem erro.

**Correção:** cada extrator implementa `detect(path) -> DetectionResult | None` com uma pontuação;
o detector só itera o registro e escolhe a maior.
**Critério de aceite:** `grep -n "'RPG Maker'\|'Wolf RPG'" src/services/GameDetector.py` → 0.

---

### A-07 · Não existe modelo de segmento: tudo é árvore ad-hoc casada por posição — **Alta**

`TextsUtils.py:4-36` + `RPGMakerExtractor.py:137-154` + `WolfRPGExtractor.py:208-240`

Cada extrator devolve uma árvore própria (`{"id", "pages": [{"id", "list": [...]}]}`); o pipeline a
achata com `dictToList` e a remonta com `interactive_item`, **por ordem de aparição**. Não há
identificador estável por texto, nem contexto (arquivo, evento, falante, tipo: fala/menu/item), nem
restrição (largura da caixa de mensagem). As strategies de RPG Maker e WOLF têm o mesmo contrato
declarado duas vezes (`RPGEventStrategy.py:5-10` × `WolfEventStrategy.py:4-9`) e
`extract_text_map` é praticamente o mesmo código nos dois motores.

É a raiz de quase tudo o que o pedido quer habilitar: sem segmento com ID não há exportação para
XLIFF/PO/planilha e reimportação, revisão humana, prompt de LLM com contexto de cena, nem retomada
parcial confiável.

**Reprodução:** leitura; `grep -n "class .*Strategy:" src/extractor -r` → duas bases idênticas.

**Correção:** `Segment(id, source, context: dict, constraints: dict)`; o extrator passa a ter
`extract(data) -> list[Segment]` e `apply(data, {id: texto})`. As árvores atuais viram detalhe interno
de cada extrator; unificar `EventStrategy`/`WolfEventStrategy` num protocolo único.
**Critério de aceite:** teste de ida e volta por extrator: `apply(extract(x), identidade) == x` byte a
byte, e IDs estáveis entre duas extrações do mesmo arquivo.

---

### A-08 · Fábrica aceita nomes por substring e persiste um nome com erro de digitação — **Média**

`factory.py:56-61,88-93` + `GoogleTranslate.py:11`

`find_match` aceita qualquer substring: medido, `-t translator` escolhe `deeplTranslator` e `-t g`
escolhe `googleTraslator`, em silêncio e dependendo da ordem de import. O nome registrado tem erro
("Traslator") e vira a coluna `engine` da memória — corrigir o nome depois orfaniza o histórico.

**Reprodução:** `slime -t translator -e rpg …` usa DeepL sem avisar.

**Correção:** match exato ou por alias declarado; ambiguidade vira erro listando candidatos; um
`id` estável (`google`, `deepl`, `ollama`) separado do rótulo de exibição.
**Critério de aceite:** teste afirma que `find_match('translator')` levanta erro de ambiguidade.

---

### A-09 · Descoberta de plugins por import com lista fixa, sem pacote instalável — **Média**

`src/extractor/__init__.py:13-16` + `main.py:1-2`

```python
subpackages = ['rpgmaker', 'wolfrpg']
```

O registro acontece como efeito colateral de import, e subpacotes novos precisam entrar nessa lista.
Um terceiro não consegue distribuir um extrator ou provedor sem editar o repositório. O mecanismo
padrão do ecossistema para isso são *entry points* declarados em `[project.entry-points.'grupo']` e
lidos com `importlib.metadata.entry_points(group=…)` [F3][F4] — o que exige o `pyproject.toml` que o
projeto ainda não tem (`INSTALACAO-CLI-TERMINAL.md`).

**Reprodução:** criar `src/extractor/renpy/` com `@register_extractor`; ele não aparece em
`--list-extractors` até ser incluído na lista.

**Correção:** `pyproject.toml` com grupos `slime.extractors` e `slime.providers`; os plugins internos
se declaram pelo mesmo mecanismo.
**Critério de aceite:** um pacote de teste instalado em modo editável aparece em `--list-extractors`
sem alterar `src/`.

---

## 5. Achados de performance

### P-01 · Um lote por arquivo, sem deduplicação entre arquivos — **Alta**

`pipeline.py:150-154` + `BaseTranslate.py:285-290` + `BaseExtractor.py:545-589`

Cada arquivo chama `translate_batch` isoladamente; a deduplicação (`dict.fromkeys`) só enxerga o
próprio arquivo e o cache só ajuda depois que outro arquivo terminou. Medido (3.2): 20 arquivos com
30 falas curtas geraram **20 requisições**, quando o texto total cabe em ~4–5 lotes de 5.000
caracteres [modelado]. Para o Google, requisição é o recurso escasso — é a razão de existir todo o
`ProxyManager`. Falas repetidas em vários mapas ("...", "Sim", nomes) são traduzidas várias vezes se
os arquivos rodarem em paralelo.

| Alcance | Impacto |
|---|---|
| Google (sem chave) | Alto — mais requisições antecipam o HTTP 429. |
| DeepL / Ollama | Médio — custo por caractere igual, mas latência e cota por requisição somam. |

**Custo [modelado]:** ~4× mais requisições que o necessário no cenário de 3.2.

**Reprodução:** o script de 2.1 com `N=20`; contar chamadas a `_translate_single_batch`.

**Correção:** fila global de segmentos no `TranslationEngine` (A-04): extração de todos os arquivos →
dedup global → lotes pelas capacidades do provedor → aplicação por arquivo. A fast lane
(`BaseExtractor.py:556-594`) já prova o padrão de separar varredura e despacho.
**Critério de aceite:** o mesmo cenário de 3.2 faz ≤ 5 requisições e o número de strings únicas
enviadas é igual ao número de strings únicas do conjunto.

---

### P-02 · Retries aninhados em três níveis e pool escondido dentro do semáforo — **Alta**

`BaseExtractor.py:411-435` + `GoogleTranslate.py:217-239,246-296,265-269`

Um lote que falha é tentado 3 vezes por `_translate_single_batch`; cada tentativa chama
`_translate_raw`, que faz 5 tentativas `gtx` + `deep_translator` + MyMemory; e `process_file` repete o
arquivo inteiro até 5 vezes com *backoff* de até 60 s. Pior caso: 5 × 3 × 7 ≈ **105 chamadas por
lote** [modelado], exatamente quando o provedor está sinalizando sobrecarga. Além disso, quando o
delimitador volta alterado, o fallback cria um `ThreadPoolExecutor(max_workers=5)` **dentro** da
thread que já segura o semáforo de requisições; com `MAX_REQUESTS_SIMULTANEOUSLY = 4`, o teto efetivo
passa a 4 × 5 = 20 requisições simultâneas.

**Reprodução:** provedor falso que sempre levanta `ConnectionError`; contar chamadas até
`process_file` desistir (não executado; o 105 vem da leitura).

**Correção:** um único nível de retry, no engine, com orçamento por job e *circuit breaker* por
provedor; o fallback item a item passa pela mesma fila e pelo mesmo semáforo.
**Critério de aceite:** com o provedor falso acima, o número de chamadas por lote é ≤ 5 e nunca há
mais de `MAX_REQUESTS_SIMULTANEOUSLY` chamadas simultâneas (contador com pico registrado).

---

### P-03 · Seis parses e duas serializações por arquivo — **Média**

`BaseExtractor.py:136-139,393,491,632-640`

Medido em 3.2. As causas: a fast lane descarta o resultado da pré-varredura quando o arquivo vai para
o fluxo normal (`:491` → `:393` reextrai); `import_file` serializa e reparseia só para validar
(`:136-139`); `import_files` reextrai o arquivo de `process/` e **também** o original de `input/`,
cujo conteúdo só serve para um `logging.warning` (`:632-638`); e reescreve tudo em `output/`. Para os
binários WOLF, `WolfRPGExtractor.import_file` ainda relê o original (`WolfRPGExtractor.py:141-148`).

**Custo [modelado]:** irrelevante para jogos pequenos; linear no tamanho dos maiores `MapNNN.json` e
`.mps`, que passam de MB em jogos comerciais.

**Reprodução:** o script de 2.1; contar `json.load` por arquivo.

**Correção:** a pré-varredura entrega `data` e `raw_text` para as duas lanes; a validação usa o
objeto em memória; `import_files` só copia de `process/` para `output/` (ou o job grava direto no
destino final, P-05).
**Critério de aceite:** instrumentação de 3.2 mostra ≤ 2 `json.load` e 1 `json.dumps` por arquivo.

---

### P-04 · Atualização de status quadrática no número de arquivos — **Média**

`BaseExtractor.py:251-256` + `:541-543`

Cada `add_threads_status` refiltra a lista inteira chamando `normalize_status_file` (várias chamadas
`os.path`) para **cada** item, sob lock, e notifica os observadores com uma cópia completa. Todos os
arquivos são pré-inseridos, então a lista tem n itens desde o início e cada arquivo gera ~5 eventos.

**Custo [modelado]:** 1.000 arquivos × ~5 eventos × 1.000 itens ≈ 5 milhões de normalizações de
caminho, todas sob o mesmo lock que as threads de tradução disputam.

**Reprodução:** medir o tempo de `process_files` com 2.000 arquivos vazios e provedor falso (não executado).

**Correção:** `dict` indexado pelo caminho já normalizado na entrada; notificar só o delta.
**Critério de aceite:** tempo de 2.000 arquivos vazios cresce linearmente (razão ≤ 2,2 entre 2.000 e
1.000 arquivos, mesmo protocolo).

---

### P-05 · Quatro cópias completas dos arquivos a cada execução — **Média**

`main.py:559-569,621-623`

Fluxo: pasta do jogo → backup (`-<idioma>`, só na primeira vez) → `input/` → `process/` → `output/` →
pasta do jogo. Com as pastas no `cwd` (A-02), um jogo de 2 GB exige ~6 GB livres no diretório de onde o
comando foi chamado [modelado].

**Reprodução:** `du -sh input process output` após uma execução.

**Correção:** ler direto da pasta do jogo, gravar num `Workspace` temporário e aplicar com troca
atômica por arquivo; o backup continua como está.
**Critério de aceite:** após uma execução, só backup e destino contêm cópias dos arquivos.

---

### P-06 · Expressões regulares recompiladas a cada chamada — **Baixa**

`RPGMakerExtractor.py:47-59,348-373,384-391`

`get_mask_patterns` recompila 9 regex por arquivo e `fix_text_translate` recompila ~15 por lote e 4
por **texto** (`re.sub` com padrão literal dentro do laço). O cache interno do módulo `re` amortiza
parte disso, mas o código fica mais lento e mais difícil de testar do que constantes de módulo.

**Reprodução:** leitura.

**Correção:** constantes de módulo, como `RPGMakerExtractor._note_tag_pattern` (`:237`) já faz.
**Critério de aceite:** `grep -n "re.compile" src/extractor/rpgmaker/RPGMakerExtractor.py` só
devolve linhas no nível de módulo ou de classe.

---

## 6. Achados de qualidade e correção

### Q-01 · `get_proxy_dict` readquire um `Lock` que já segura — **Crítica** (working tree)

`src/services/ProxyManager.py:190-192` + `:154`

```python
with self._pool_lock:
    if len(self._working_proxies) < 3:
        self.ensure_proxies(min_count=5)   # ensure_proxies faz `with self._pool_lock:` de novo
```

`_pool_lock` é `threading.Lock` (`:29`), que não é reentrante: `acquire()` de um lock já travado
bloqueia até outra thread liberá-lo [F2]. Com pool ativo, IP marcado como bloqueado e menos de 3
proxies, **toda** thread de tradução para para sempre. Reproduzido: script isolado → `exit 124` por
timeout; a suíte trava em `test_proxy_manager_rotation`. O defeito só existe na alteração não
commitada; em `4973016` a chamada só ocorria com o pool vazio e retornava antes.

**Reprodução:** `pm._enabled = True; pm._direct_blocked = True; pm.get_proxy_dict()` com pool vazio.

**Correção:** decidir sob o lock e chamar `ensure_proxies` fora dele (como `report_proxy_failure`
já faz em `:216-221`), ou trocar por `threading.RLock` [F2].
**Critério de aceite:** `pytest tests/test_proxy_and_deepl.py` termina em < 10 s, e um teste novo com
pool vazio e `direct_blocked=True` retorna em < 2 s.

---

### Q-02 · A "correção" de código transforma palavras de diálogo em literais JavaScript — **Crítica**

`src/extractor/rpgmaker/RPGMakerExtractor.py:361-368,384-391`

```python
r'\b(?:falso|falsa|faux|fausse)\b': 'false',
r'\b(?:nulo|nula|nul)\b': 'null',
```

As substituições valem para **todo** texto traduzido — fala, descrição, nome — não só para scripts.
Medido (3.3): "falso", "verdadeira", "nula" viram `false`, `true`, `null`; `"  "` vira `""`; qualquer
`_` com espaços em volta é colado. Português é o idioma de destino principal do projeto.

**Reprodução:** o trecho de 3.3.

**Correção:** aplicar as correções de código só aos segmentos que vieram de `ScriptStrategy`,
`PluginStrategyMV/MZ` e `ControlVariablesStrategy` — o que exige saber a origem do texto (A-07). Até
lá, restringir às strings em que `RPGTextFilters.is_technical_or_code` é verdadeiro.
**Critério de aceite:** teste com as quatro frases de 3.3 mantendo a saída idêntica à entrada, e um
teste com `if(v[1] == verdadeiro)` ainda corrigido para `true`.

---

### Q-03 · `Data.wolf` é anunciado pelo detector e nunca desempacotado — **Alta**

`WolfRPGExtractor.py:113-121,160-166` + `BaseExtractor.py:386-389,468-470,531-538` + `GameDetector.py:101-124`

`is_translatable_file('Data.wolf')` devolve `False` (medido), e tanto a pré-varredura quanto
`process_file` testam isso **antes** de chamar `extract_files` — onde está o único
`WolfArchive.extract_archive` do projeto. O `.wolf` é copiado intacto com status "No text to
process". Mesmo que chegasse lá, a extração grava em `input/` depois que `process_files` já listou os
arquivos, então o conteúdo desempacotado nunca entraria na fila.

**Reprodução:** pasta com só `Data/Data.wolf`; o detector sugere "Wolf RPG" e o resultado é 0 textos.

**Correção:** desempacotar numa etapa de preparação antes da listagem (hook `prepare(workspace)` do
formato) e tratar o `.wolf` como contêiner, não como arquivo traduzível.
**Critério de aceite:** teste com um `.wolf` sintético (os testes de `WolfArchive` já criam um) que
termina com os `.mps` internos traduzidos no `output/`.

---

### Q-04 · Falha do provedor vira "Processed successfully" com texto original — **Alta**

`GoogleTranslate.py:173-174,181-184,206-209,235-239` + `OllamaTranslate.py:66-67` + `BaseTranslate.py:322,336,350`

A cadeia de fallback termina devolvendo o texto de entrada: MyMemory engole exceções, Ollama devolve a
entrada se o lote não tiver exatamente 1 item, `translate_batch` preenche itens que falharam com o
original. O arquivo sai com status `success` e parte do texto no idioma de origem, sem nenhuma contagem
disso. O filtro de identidade (`TranslationMemory.py:219-221`) impede o cache de ser envenenado, mas
não informa o usuário.

**Reprodução:** provedor falso que devolve a entrada; o status final é `success` (dedução de `BaseExtractor.py:442`; não executado).

**Correção:** o engine conta segmentos não traduzidos por arquivo e o status passa a `partial` com o
número; o relatório final lista arquivos com cobertura < 100 %.
**Critério de aceite:** teste com provedor que falha em 1 de 10 segmentos resulta em status `partial`
e mensagem com "9/10".

---

### Q-05 · CSV traduzido ganha coluna `*_old` e BOM; estado de classe colide entre subpastas — **Alta**

`CSVExtractor.py:46,133-135,209,288,325`

Medido (3.4): a saída ganha `text_old` e um BOM que a entrada não tinha — ambos mudam o que o
carregador do jogo lê. `_detected_delimiters` é atributo de **classe** e, junto com `_file_modes`, é
chaveado por nome base: `en/strings.csv` e `menu/strings.csv` sobrescrevem a configuração um do
outro quando processados em paralelo. Há ainda `print` dentro de `apply_configuration` (`:80,82,91`),
camada de domínio escrevendo na saída do terminal.

**Reprodução:** o fluxo de 3.4; para a colisão, duas subpastas com CSVs homônimos e delimitadores
diferentes.

**Correção:** não gravar `*_old` no arquivo de saída (se a comparação for útil, vai para um relatório
à parte); preservar a presença ou ausência de BOM da entrada; estado por caminho relativo e por
instância.
**Critério de aceite:** teste de ida e volta com provedor identidade produz CSV byte a byte igual à
entrada.

---

### Q-06 · O extrator JSON genérico traduz todo valor string — **Alta**

`JsonExtractor.py:32-36` + `GameDetector.py:177-184`

`extract_text` devolve o documento inteiro: caminhos de imagem, IDs, nomes de variável e chaves de
script vão para o provedor. É também o fallback do detector para qualquer pasta com `.json` — o
caminho mais provável para um jogo que o projeto ainda não conhece.

**Reprodução:** `{"face": "Actor1", "text": "Hello"}` → `face` é traduzido e o jogo não acha a imagem (dedução da leitura; não executado).

**Correção:** lista de chaves permitidas configurável (com padrão conservador, como `CSVExtractor`
já faz para colunas em `:188-190`) e reuso de `RPGTextFilters.is_technical_or_code`.
**Critério de aceite:** teste com o JSON acima mantém `face` intacto.

---

### Q-07 · DeepL: método inexistente no caminho de `translator()` e `auto` enviado como `AUTO` — **Média**

`DeeplTranslate.py:50-58,89-95`

`translator()` chama `self.preprocess_text`, que não existe no DeepL nem na base (medido com
`hasattr`); o caminho hoje só é alcançado quando o tradutor não tem cache, mas o método é o contrato
abstrato. Com idioma de origem `auto`, `_map_lang_code` envia `source_lang=AUTO`; a documentação
diz que a autodetecção acontece quando o parâmetro é **omitido** [F1].

**Reprodução:** `DeeplTranslate().translator({"a": "x"})` → `AttributeError`.

**Correção:** remover `translator()` do contrato (A-04) e omitir `source_lang` quando for `auto`.
**Critério de aceite:** teste com `requests.post` simulado afirma que o payload não tem `source_lang`
quando a origem é `auto`.

---

### Q-08 · Classificação de erro por substring da mensagem, espalhada em quatro lugares — **Média**

`BaseExtractor.py:327-358` + `BaseTranslate.py:216-224,316-321,330-335` + `GoogleTranslate.py:129-131,274-275`

Decidir se um erro é fatal, retentável ou de cota depende de achar `"429"`, `"bloqueado pelo google"`
ou `"desmascaramento incompleto"` na mensagem — texto em português escrito em outro módulo. Qualquer
exceção desconhecida é retentável por padrão (`BaseExtractor.py:358`), inclusive erro de chave de API.

**Reprodução:** DeepL com chave inválida (HTTP 403) é retentado 5 vezes com *backoff* (dedução de `BaseExtractor.py:358,411-431`; não executado).

**Correção:** hierarquia tipada (`ProviderRateLimited`, `ProviderAuthError`, `ProviderUnavailable`,
`MaskIntegrityError`) levantada pelos provedores; o engine decide pela classe.
**Critério de aceite:** `grep -rn "\"429\" in\|bloqueado pelo google" src/` → 0, e teste com HTTP 403
simulado não faz retry.

---

### Q-09 · Tradução de nomes de arquivo sem sanitização e com sobrescrita silenciosa — **Média**

`FileNameTranslator.py:3,8,46,66-73`

O nome traduzido vira caminho sem filtrar `/`, `:` ou nomes reservados, e dois arquivos que traduzem
para o mesmo nome se sobrescrevem sem aviso. O serviço importa `cli` diretamente (camada de serviço
dependendo da de apresentação) e grava em `output/` relativo ao `cwd`.

**Reprodução:** arquivo `a.png` cujo nome traduzido contenha `/` (dedução da leitura; não executado).

**Correção:** sanitizar, detectar colisão e sufixar; retornar um relatório em vez de imprimir.
**Critério de aceite:** teste com dois nomes que colidem produz dois arquivos distintos.

---

### Q-10 · Sem CI, sem pacote e sem versões travadas — **Média**

`requirements.txt:13` + `.github/`

```bash
ls .github/workflows
# → No such file or directory
find . -maxdepth 1 -name pyproject.toml -o -maxdepth 1 -name setup.py
# → 0 resultados
```

256 testes existem, mas nada os roda automaticamente — por isso o deadlock de Q-01 pode ser commitado.
`rubymarshal>=1.2.10` é a única dependência sem versão fixa e é justamente a que lê binários de jogo.
`test_cli_aliases` depende de existir `.venv` com as dependências (3.1).

**Reprodução:** 3.1.

**Correção:** workflow de CI com `pytest` e `flake8` (o `Makefile` já tem `lint`), `pyproject.toml`
com versões fixadas.
**Critério de aceite:** um PR com o deadlock de Q-01 falha no CI em < 5 min.

---

### Q-11 · API pública sem nenhum uso — **Baixa**

```bash
grep -rn "sanitize_output_files\|from_all\|from_default\|get_game_synopsis" src main.py scripts | grep -v "def "
# → 0 resultados
```

Somam-se `preprocess_text`/`postprocess_text` que não fazem nada (`GoogleTranslate.py:75-83`),
`report_proxy_success` vazio (`ProxyManager.py:223-225`), `cache_lock` mantido "por compatibilidade"
(`BaseTranslate.py:111-115`) e `save_cache` mexendo em atributos privados do `TranslationMemory`
(`:117-121`). Cada um é superfície que o próximo provedor vai achar que precisa implementar.

**Reprodução:** a busca acima.

**Correção:** remover; `save_cache` vira `TranslationMemory.flush()`.
**Critério de aceite:** a busca acima continua em 0 e `grep -n "_conn\|_lock" src/translate/BaseTranslate.py` → 0.

---

## 7. O que está bem feito

| Local | Decisão |
|---|---|
| `TranslationMemory.py:69-121` | Um escritor serializado + leitores *thread-local* em WAL: leitura sem lock global. Manter. |
| `TranslationMemory.py:46-53` | Upsert que não rebaixa `locked` > `reviewed` > `machine` — base pronta para revisão humana. |
| `TranslationMemory.py:172-210` | `lookup_many` em blocos de 900 parâmetros: um SELECT por arquivo, não por texto. |
| `BaseExtractor.py:556-594` | Pré-varredura em *streaming* separando arquivos 100 % em cache: o padrão certo para P-01. |
| `pipeline.py:165-167` | `UnmaskingStep` não engole exceção: placeholder nunca chega ao jogo. |
| `BaseExtractor.py:391-447` | Retry só na fase de rede; extração e gravação não são retentadas. |
| `main.py:277-345` | Perguntas de configuração como dados — é o que permite a GUI gerar formulário (A-03). |
| `RPGMakerExtractor.py:38-41` | Comentário explicando por que `COMMENT`/`LABEL` ficam de fora: decisão de domínio documentada. |

---

## 8. Observação estrutural: arquitetura-alvo

Os achados A-01 a A-09 têm uma causa comum: **faltam três contratos**. A proposta abaixo não
reescreve nada do que está bem feito — move peças para trás de interfaces.

```text
           ┌────────── UI (CLI · TUI · GUI) ──────────┐
           │ só coleta JobConfig a partir de esquemas │
           └──────────────────┬───────────────────────┘
                              ▼
                     TranslationJob (Workspace)
   GameFormat ──► list[Segment] ──► TranslationEngine ──► TranslationProvider
   detect/prepare     id, source,     dedup global, lotes     translate(segments, ctx)
   load/extract       context,        por Capabilities,       Capabilities
   apply/save         constraints     TM com política,
                                      retry + rate limit
                              │
                              └──► SegmentExporter (XLIFF · PO · CSV) ↔ revisão humana
```

- **`GameFormat`** (A-01, A-06, Q-03): `detect`, `prepare` (desempacotar `.wolf`), `load`,
  `extract → list[Segment]`, `apply`, `save`. RPG Maker e WOLF viram dois plugins; JSON/CSV genéricos,
  plugins configuráveis.
- **`Segment`** (A-07, Q-02): com `context.kind` (`dialogue`, `script`, `menu`, `db`), as correções de
  código só rodam em `script`, e um LLM recebe falante e cena.
- **`TranslationProvider` + `Capabilities`** (A-04, P-01, P-02): provedores novos (Azure, OpenAI,
  Claude, LibreTranslate, modelo local) cabem em dezenas de linhas.
- **`TranslationEngine`** (A-05, P-01, P-02, Q-04, Q-08): único dono de cache, lotes, retry e
  cobertura.
- **Exportação de segmentos**: "outras maneiras de traduzir" deixa de ser um provedor e vira um
  fluxo — exportar, traduzir fora (ferramenta de CAT, planilha, revisor), reimportar com status
  `reviewed`, que a memória já prioriza.

Migração em quatro passos, cada um entregável sozinho: (1) Q-01, Q-02 e CI; (2) `Segment` e
`FormatAdapter` por trás dos extratores atuais, sem mudar saída — os testes de ida e volta de A-07
são a rede de segurança; (3) `TranslationEngine` absorvendo cache e lotes de `BaseTranslate`;
(4) `TranslationJob` e *entry points*. Fazer o passo 3 antes do 2 obriga a reescrever o engine.

---

## 9. Backlog priorizado

> Ordenado por (impacto no usuário × alcance) ÷ esforço; correções de dado corrompido antes de
> arquitetura.

| Pri | Item | Esforço | Alcance | Tipo |
|---|---|---|---|---|
| 1 | **Q-01** — tirar `ensure_proxies` de dentro do lock | XS | Google + pool | Correção |
| 2 | **Q-02** — restringir correções de código a strings técnicas | S | RPG Maker | Correção |
| 3 | **Q-10** — CI com `pytest` + `flake8` | S | Todas | Qualidade |
| 4 | **Q-05** — CSV sem `*_old`, BOM preservado, estado por instância | S | CSV | Correção |
| 5 | **Q-03** — desempacotar `.wolf` antes da listagem | M | WOLF | Correção |
| 6 | **Q-04 + Q-08** — status `partial` e exceções tipadas (unidade: o engine precisa das duas) | M | Todas | Qualidade |
| 7 | **Q-06** — chaves permitidas no extrator JSON | S | JSON genérico | Correção |
| 8 | **A-07 + A-01** — `Segment` e `FormatAdapter` (unidade: o adapter devolve segmentos) | L | Extensão a jogos | Arquitetura |
| 9 | **A-04 + P-01 + P-02** — `TranslationEngine` e `Capabilities` | L | Extensão a provedores | Arquitetura/Perf |
| 10 | **A-05** — política de lookup da memória | M | Troca de provedor | Arquitetura |
| 11 | **A-02 + A-03** — `Workspace` e `TranslationJob` únicos para CLI e GUI | M | Todas | Arquitetura |
| 12 | **A-09 + A-08** — `pyproject.toml`, *entry points*, IDs estáveis | M | Plugins de terceiros | Arquitetura |
| 13 | **P-03 + P-04 + P-05** — um parse por arquivo, status em `dict`, sem cópias | M | Jogos grandes | Perf |
| 14 | **A-06, Q-07, Q-09, Q-11, P-06** | S cada | Pontual | Qualidade |

Os itens 1, 2 e 3 somam menos de um dia de trabalho e eliminam os dois defeitos críticos e a causa de
eles terem chegado ao código. Os itens de desempenho (9 e 13) devem entrar **um por vez**, repetindo
a instrumentação de 3.2 entre eles, para que cada ganho seja atribuível.

---

## 10. Riscos e o que falta verificar

1. **O provedor padrão depende de um endpoint não oficial.** `translate.googleapis.com/translate_a/single`
   com `client=gtx` (`GoogleTranslate.py:93-99`) não tem contrato público; uma mudança do lado do
   Google derruba o provedor padrão sem aviso. Antes de investir mais no `ProxyManager`, avaliar
   provedores com API oficial.
2. **O pool usa proxies públicos de listas no GitHub** (`ProxyManager.py:18-22`). O texto do jogo
   trafega por servidores desconhecidos (o TLS é mantido por túnel, mas disponibilidade e intenção não
   são controladas) e o contorno de *rate limit* pode violar os termos do serviço — decisão do
   mantenedor, registrada aqui como risco, não como achado.
3. **Nenhum tempo foi medido.** P-01 a P-05 têm contagens medidas, mas o impacto em minutos é
   `[modelado]`; medir com um jogo real (≥ 200 mapas) no mesmo protocolo antes e depois de cada item.
4. **Os adapters binários não foram auditados.** `WolfArchive.py`, `huffman.py`,
   `WolfBinaryAdapter.py` e `RubyMarshal.py` somam ≈ 4.100 linhas; o teste de ida e volta de A-07
   aplicado a jogos reais é a forma barata de achar corrupção neles.
5. **A GUI não foi executada.** A-03 vem da leitura e do grep; conferir abrindo `slime --gui`.
6. **Limite de itens por requisição do DeepL não confirmado.** A referência consultada só documenta
   128 KiB por requisição [F1]; com `char_limit = 10000` o lote fica abaixo disso, mas o número de
   textos por requisição não foi verificado.
7. **Hipóteses refutadas, registradas para não voltarem:**
   > **Não há vazamento de `*_old` nos JSON do RPG Maker** — medido em 3.5; o vazamento existe só no CSV.

   > **`test_cli_aliases` não indica defeito no código** — falha só onde não existe `.venv` (3.1).

---

## 11. Fontes consultadas

| # | Fonte | Tipo | Versão | Consultada em | Sustenta |
|---|---|---|---|---|---|
| F1 | [DeepL API — `POST /v2/translate`](https://developers.deepl.com/api-reference/translate) | Doc oficial | v2 (atual) | 2026-09-27 | Q-07 (`source_lang` omitido); risco 6 (128 KiB) |
| F2 | [Python — `threading` Lock e RLock](https://docs.python.org/3/library/threading.html#lock-objects) | Doc oficial | 3.x (instalado 3.14.7) | 2026-09-27 | Q-01 |
| F3 | [PyPA — Entry points specification](https://packaging.python.org/en/latest/specifications/entry-points/) | Doc oficial | atual | 2026-09-27 | A-09 |
| F4 | [PyPA — Creating and discovering plugins](https://packaging.python.org/en/latest/guides/creating-and-discovering-plugins/) | Doc oficial | atual | 2026-09-27 | A-09 (`[project.entry-points]`, `entry_points(group=…)`) |

---

> Nenhum item deste relatório foi executado contra um jogo real, contra os provedores reais (Google,
> DeepL, Ollama) ou na GUI. As reproduções rodaram numa cópia do commit `4973016` + working tree, com
> provedor falso, no Python 3.14.7 de `.venv`; tempos de execução em jogos reais estão pendentes
> (seção 10, itens 3 a 5).

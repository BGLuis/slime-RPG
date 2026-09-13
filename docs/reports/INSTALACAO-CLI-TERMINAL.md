# Instalação como comando de terminal — o que existe e o que falta

| Campo | Valor |
|-------|-------|
| **Status** | 🟡 Parcial — há integração com o menu de arquivos, mas nenhum caminho instala um comando de terminal |
| **Cobertura** | 0% (0 de 2 abordagens viáveis implementadas) |
| **Esforço** | 0,5 d no mínimo viável; 1,5–2 d no pacote instalável completo [modelado] |
| **Depende de** | Nada (pode começar já) |

---

## 1. Estado atual — evidências

### O que existe

O projeto roda hoje via `python main.py [flags]`, documentado em `README.md:61-64`:

```markdown
5. Run the application:
  ```sh
  python main.py
  ```
```

Não há shebang em `main.py` e o arquivo não é executável:

```bash
$ ls -la main.py
-rw-r--r-- 1 luis luis 20411 Aug 22 05:29 main.py
```

Existe uma venv em `.venv/`, mas `.venv/bin/python` é apenas um symlink para `/bin/python` (o
Python do sistema, versão 3.14.7) — a venv **não instala** o próprio projeto como pacote, só
provisiona um interpretador:

```bash
$ .venv/bin/python -> /bin/python
```

Existe `scripts/install_linux_integration.sh:1-90`, mas ele resolve um problema diferente: cria
entradas `.desktop`, ação do Nemo/Nautilus e menu de serviço do Dolphin, todas invocando
`$PYTHON_BIN $MAIN_PY --gui --input %f` a partir do **gerenciador de arquivos** — nenhuma delas
coloca um binário em `PATH` nem serve para chamar a ferramenta do terminal.

### O que não existe

Busca por qualquer arquivo de empacotamento Python na raiz do projeto:

```bash
find . -maxdepth 1 -iname "pyproject.toml" -o -iname "setup.py" -o -iname "setup.cfg"
# → 0 resultados
```

Sem `pyproject.toml`/`setup.py` não há como declarar um `console_scripts`/`project.scripts`, e
portanto não há **nenhum** caminho hoje (nem `pip install .`, nem `pipx install .`) que produza um
comando `extractor-translation` (ou nome equivalente) utilizável de qualquer diretório.

### ⚠️ Defeito encontrado: caminhos de dados são relativos ao diretório de trabalho, não ao projeto

Todos os diretórios de trabalho e o arquivo de log são caminhos relativos resolvidos a partir do
`cwd` no momento da execução, não do diretório onde `main.py` está:

```python
# main.py:18
file_handler = RotatingFileHandler('processamento.log', maxBytes=5*1024*1024, backupCount=2, ...)
```
```python
# src/extractor/BaseExtractor.py:14-16
folderProcess = 'process'
folderInput = 'input'
folderOutput = 'output'
```
```python
# src/translate/BaseTranslate.py:18
cache_path_base = 'cache'
```
```python
# src/services/SettingsStore.py:5
DB_PATH = os.path.join('cache', 'settings.db')
```

Isso não é causado por este pedido — o projeto sempre foi pensado para rodar com `cwd` na raiz do
repositório — mas é exatamente o que **quebra** um comando de terminal instalado globalmente: se o
usuário chamar `extractor-translation` de dentro da pasta de um jogo (o uso natural de um comando
de terminal), o programa cria `cache/`, `input/`, `output/`, `process/` e `processamento.log`
dentro dessa pasta do jogo, não na pasta do projeto — e cada chamada de um diretório diferente
recomeça com cache e configurações vazios.

### Ambiente verificado nesta máquina

```bash
$ echo $PATH
/home/luis/.local/bin:/home/luis/.cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/bin:...
$ ls -ld ~/.local/bin        # já existe e já está no PATH
$ which pipx                 # not found
$ python3 --version          # Python 3.14.7 (sistema)
```

`~/.local/bin` já está pronto para receber um executável de usuário sem `sudo`. `pipx` **não**
está instalado. O `README.md:32` pede Python 3.10+; o Python do sistema é 3.14.7 — mais novo que a
faixa testada, o que é um risco à parte para `PyQt5==5.15.9` (`requirements.txt:10`), independente
da instalação como comando.

---

## 2. As duas abordagens

| Opção | O que é | Custo | Veredicto |
|---|---|---|---|
| **A — Wrapper script** | Script em `scripts/` que fixa `cd` para a raiz do projeto e chama `.venv/bin/python main.py "$@"`; symlink em `~/.local/bin` | Baixo: nenhuma mudança em código Python, só um script novo | **Recomendada** para uso imediato |
| **B — Pacote instalável** | `pyproject.toml` com `[project.scripts]`, `main()` extraída de `main.py`, instalado com `pipx install .` | Alto: reestrutura o entry point e força decidir a política de `cwd` para dados | Recomendada como solução definitiva, mas só compensa se o projeto for distribuído a terceiros |
| **C — venv ativada manualmente** | `source .venv/bin/activate && python main.py` de qualquer lugar | Zero mudança | Rejeitada: exige ativar a venv toda vez e `cd` até o projeto — não é "usar do terminal" no sentido de um comando disponível |

### 2.1 Wrapper script + symlink (opção A)

Um script novo, por exemplo `scripts/extractor-translation`, fixa o diretório de trabalho antes de
chamar `main.py`:

```bash
#!/bin/bash
cd "/home/luis/Documents/hand-on/extractor-and-translation" || exit 1
exec "$(dirname "$0")/../.venv/bin/python" main.py "$@"
```

`chmod +x` nele e um `ln -s` em `~/.local/bin/extractor-translation` bastam — `~/.local/bin` já
está no `PATH` (seção 1). Isso **não resolve** o defeito da seção 1: o `cd` fixo faz `cache/`,
`input/`, `output/` sempre caírem dentro do projeto, o que é provavelmente o comportamento
desejado (o usuário copia os arquivos do jogo para `input/` do projeto), mas precisa ser uma
decisão explícita, não um acidente do script.

### 2.2 Pacote instalável com `pipx` (opção B)

Exige extrair o corpo de `if __name__ == '__main__':` (`main.py:434-460` em diante) para uma
função `main()` importável, declarar em `pyproject.toml`:

```toml
[project.scripts]
extractor-translation = "src.cli_entry:main"
```

e instalar com `pipx install .` — que cria uma venv isolada própria e o link em `~/.local/bin`
automaticamente, sem tocar na `.venv/` já existente. Essa opção **força** resolver a mesma questão
de `cwd` da seção 1: um pacote instalado por `pipx` não tem "pasta do projeto" fixa, então
`cache/`, `input/`, `output/`, `process/` passam a ser relativos a onde quer que o usuário rode o
comando — o que só é seguro se essa for de fato a intenção (rodar dentro da pasta de cada jogo).

### 2.3 O que não fazer

Não adicionar `.venv/bin` diretamente ao `PATH`. Não há nenhum executável chamado
`extractor-translation` dentro de `.venv/bin` — só `python`/`python3`, então isso exporia apenas
mais um jeito de digitar `python main.py`, sem resolver a exigência de rodar de qualquer diretório.

---

## 3. Plano de implementação

**Mínimo viável (opção A):**

1. **Criar `scripts/extractor-translation` (0,1 d)** — wrapper com `cd` fixo para a raiz do
   projeto, chamando `.venv/bin/python main.py "$@"`.
2. **`chmod +x` e symlink em `~/.local/bin` (0,1 d)** — `ln -s "$(pwd)/scripts/extractor-translation" ~/.local/bin/extractor-translation`.
3. **Testar de um diretório fora do projeto (0,2 d)** — confirmar que `extractor-translation --list-extractors` funciona de `~` e que `cache/`/`processamento.log` aparecem dentro do projeto, não no `cwd` de onde foi chamado.

**Escopo completo (opção B), se o objetivo for distribuir a ferramenta:**

1. **`pyproject.toml` + extrair `main()` (0,5–1 d)** — mover o bloco `main.py:434+` para
   `src/cli_entry.py:main()`, mantendo `main.py` como shim fino para compatibilidade com o
   `Dockerfile` (`CMD ["python", "main.py"]`).
2. **Resolver a política de `cwd` (0,5 d)** — decidir e documentar se `input/`/`output/`/`cache/`
   seguem o diretório onde o comando foi chamado (uso esperado: dentro da pasta do jogo) ou um
   caminho fixo tipo `~/.local/share/extractor-translation/`.
3. **Instalar via `pipx install .` e validar (0,5 d)** — inclui checar se `PyQt5` resolve a wheel
   para a versão de Python que o `pipx` escolher.

**Mínimo viável:** opção A ≈ 0,5 d.
**Escopo completo:** opção B ≈ 1,5–2 d [modelado].

A opção A não bloqueia nem invalida a B — o wrapper pode continuar existindo como atalho de
desenvolvimento mesmo depois de empacotar o projeto.

---

## 4. Armadilhas

| Armadilha | Mitigação |
|---|---|
| `cache/`, `input/`, `output/`, `process/`, `processamento.log` relativos ao `cwd` (`main.py:18`, `BaseExtractor.py:14-16`, `BaseTranslate.py:18`, `SettingsStore.py:5`) | Decidir explicitamente a política de diretório antes de instalar como comando global — do contrário cada chamada de um diretório diferente recria cache e configurações vazios |
| `PyQt5==5.15.9` (`requirements.txt:10`) contra Python 3.14.7 do sistema | Testar `pip install PyQt5==5.15.9` isoladamente antes de depender dele num `pipx install .`; se falhar, fixar a instalação para usar a `.venv` já validada (Python 3.13, conforme `.venv/bin/python3.13`) em vez do Python do sistema |
| `scripts/install_linux_integration.sh` grava o `PROJECT_ROOT` absoluto no momento em que é executado (`install_linux_integration.sh:8-9,29,49,60,81`) | Se o projeto for movido de lugar, tanto os atalhos do gerenciador de arquivos quanto um wrapper análogo em `~/.local/bin` quebram silenciosamente; documentar que reinstalar é necessário após mover a pasta |
| `pipx` não está instalado nesta máquina | Precisa de `sudo pacman -S python-pipx` (ou equivalente) antes da opção B — não é dependência do projeto, é ferramenta do sistema |

---

## 5. Verificação

**Automatizável no host (`bash`, sem GUI):**
- [ ] `extractor-translation --list-extractors` chamado a partir de `$HOME` retorna a mesma lista
      que `python main.py --list-extractors` chamado da raiz do projeto (garante que o wrapper
      resolve o `cwd` corretamente).
- [ ] Depois de rodar o comando de um diretório diferente, `cache/settings.db` e
      `processamento.log` aparecem dentro da raiz do projeto, não no diretório de onde o comando
      foi chamado.
- [ ] `which extractor-translation` resolve para o symlink em `~/.local/bin`, confirmando que o
      `PATH` já configurado nesta máquina é suficiente sem exportar nada novo no `.bashrc`/`.zshrc`.

**Só relevante se a opção B for escolhida:**
- [ ] `pipx install .` conclui sem erro de build do `PyQt5` no Python que o `pipx` selecionar.

Automatizável em CI: nenhum destes itens depende de GUI, então todos são automatizáveis num
runner Linux comum — mas nenhum foi executado ainda.

---

## 6. Riscos

1. **A politica de `cwd` é a decisão que trava tudo, não o mecanismo de instalação.** Tanto o
   wrapper quanto o pacote `pipx` são triviais de montar; o que falta decidir é se
   `input/output/cache` acompanham o projeto (like hoje) ou o diretório de onde o usuário chama o
   comando. Errar essa decisão depois de instalar significa reinstalar e migrar dados de cache já
   acumulados.
2. **`PyQt5` é a dependência mais frágil da lista.** Se a opção B for escolhida e o `pipx` decidir
   usar o Python 3.14 do sistema em vez de reaproveitar a `.venv` em 3.13, a instalação da GUI pode
   falhar por falta de wheel pré-compilada — isso não afeta o modo `--interactive`/CLI puro, que
   não importa `PyQt5` (`main.py` só faz `from src.gui import run_gui` dentro do bloco `if
   args.gui:`).

---

## 7. Arquivos tocados

| Arquivo | Mudança |
|---|---|
| `scripts/extractor-translation` | **novo** — wrapper de terminal para a opção A |
| `pyproject.toml` | **novo**, só na opção B |
| `src/cli_entry.py` | **novo**, só na opção B — recebe o corpo hoje em `main.py:434+` |
| `main.py` | idem, só na opção B — vira shim fino chamando `src.cli_entry.main()`, mantendo `Dockerfile:26` funcionando sem alteração |
| `README.md` / `README.pt-br.md` | atualizar seção *Installation* com o novo passo, em ambas as opções |

---

> Nenhum item deste relatório foi executado nesta máquina além das buscas e verificações de
> ambiente listadas nas seções 1 e 5 (`echo $PATH`, `which pipx`, `python3 --version`, `find` por
> arquivos de empacotamento). Toda a análise de código vem da leitura do branch `main` (commit
> `752a0db`); a instalação de fato — criação do wrapper, symlink, ou `pyproject.toml` — está listada
> na seção 5 como pendente.

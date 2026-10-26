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
  Ferramenta de extração e tradução de textos para jogos de RPG Maker e arquivos JSON i18n.
</div>

*Leia em outros idiomas: [English](README.md)*

# 📖 Sobre
O Extractor and Translation é uma ferramenta desenvolvida em Python para extrair textos traduzíveis de formatos específicos e automatizar sua tradução usando modelos de IA via Ollama. O sistema utiliza bibliotecas modernas como PyQt5 para a interface gráfica e deep_translator/Ollama para traduzir assets de jogos e arquivos JSON de localização de forma eficiente, gerenciando o contexto e a concorrência das requisições ao motor de IA.

# 📋 Motivo
Criei esse repositório inicialmente para poder traduzir jogos de RPG Maker e posteriormente evoluir o conceito para utilizar para traduzir JSON i18n de sites.

# 💻 Como iniciar

### Requisitos
- [Python 3.10+](https://www.python.org/downloads/)
- [Docker](https://docs.docker.com/get-docker/) (Opcional, para execução em container)
- [Ollama](https://ollama.com/) (Caso vá utilizar LLMs locais para tradução)

### Instalação

1. Clone o repositório do projeto:
  ```sh
  git clone https://github.com/bgluis/extractor-and-translation.git
  ```

2. Navegue até o diretório do projeto:
  ```sh
  cd extractor-and-translation
  ```

#### Método 1: Execução Nativa (Python)

3. Crie e ative um ambiente virtual (opcional, mas recomendado):
  ```sh
  python -m venv .venv
  source .venv/bin/activate  # No Windows use: .venv\Scripts\activate
  ```

4. Instale as dependências:
  ```sh
  pip install -r requirements.txt
  ```

5. Execute a aplicação:
  ```sh
  python main.py
  ```

#### Método 2: Instalação Global na CLI com Atalhos (Slime)

Você pode instalar o executável globalmente para o usuário com atalhos/abreviações prontas (`slime`, `slm`, `sl`):

```sh
# Instala o comando slime e os atalhos (slm, sl, extractor-translation) em ~/.local/bin
make install-cli
```

Após isso, você pode rodar a ferramenta em qualquer terminal (inclusive de dentro da pasta de um jogo):

```sh
# Iniciar modo interativo:
slime
# ou usando atalhos:
slm
sl --interactive

# Executar tradução direta via argumentos:
slm -i /caminho/do/jogo -e RPGMaker -t Google -s ja -d pt

# Iniciar interface gráfica:
slime --gui

# Adicionar novos atalhos personalizados:
make add-alias ALIAS=slx
# ou diretamente:
slime --add-alias slx
```

3. Faça o build e inicie o container usando o Docker Compose:
  ```sh
  docker-compose up -d --build
  ```

# 🎮 Game Metadata (.slime/meta.json)

A ferramenta gerencia o ciclo de vida da tradução por meio de metadados armazenados em `.slime/meta.json` dentro da pasta do jogo. Isso possibilita verificação de integridade, detecção de patches/atualizações do jogo original, adoção de traduções legadas e retradução incremental.

### Adotando um Jogo Já Traduzido (`--adopt`)

Se você já possui um jogo traduzido (seja por versões anteriores ou ferramentas externas), é possível adotá-lo para que o Slime registre os hashes e permita atualizações incrementais:

```sh
# Adotar um jogo traduzido de RPG Maker
slime --adopt /caminho/jogo_traduzido --original /caminho/jogo_original -e RPGMaker

# Especificando idiomas de origem e destino
slime --adopt /caminho/jogo_traduzido --original /caminho/jogo_original -e RPGMaker -s ja -d pt
```

O comando analisa as pastas original e traduzida, calcula os hashes SHA-256 de cada arquivo e gera `.slime/meta.json`.

### Verificando a Integridade da Tradução (`--verify-game`)

Verifique se os arquivos traduzidos ou originais foram alterados, corrompidos ou atualizados por novos patches:

```sh
# Conferir arquivos traduzidos contra o metadado registrado
slime --verify-game /caminho/jogo_traduzido

# Conferir contra a pasta original para detectar atualizações/patches do jogo
slime --verify-game /caminho/jogo_traduzido --original /caminho/jogo_original
```

### Códigos de Status da Verificação

Ao verificar os arquivos do jogo, cada item é classificado em um dos seguintes estados:

| Código de Status | Descrição |
| --- | --- |
| `intact` | O arquivo corresponde aos hashes registrados e foi traduzido com sucesso. |
| `original_changed` | O arquivo original foi alterado (ex: patch/atualização do jogo). Requer retradução. |
| `new` | Arquivo novo detectado na pasta original que não constava nos metadados. |
| `partial` | O arquivo foi traduzido apenas parcialmente em execuções anteriores. |
| `edited` | O arquivo traduzido foi editado manualmente após a tradução. |
| `unchanged` | O conteúdo do arquivo é idêntico ao original (sem textos traduzíveis alterados). |
| `original_missing` | O arquivo original não foi encontrado na pasta de origem especificada. |
| `translation_missing` | O arquivo traduzido não foi encontrado na pasta de destino. |

### Retradução Incremental (`--incremental`)

Você pode combinar a verificação de metadados com o modo incremental para processar apenas arquivos alterados ou novos:

```sh
slime -i /caminho/do/jogo --incremental -e RPGMaker -t Google -s ja -d pt
```

# ⚙️ Variáveis de Ambiente
O projeto utiliza um arquivo `.env.example` como base. Você pode copiá-lo para `.env` e ajustar os valores.

| Variável | Descrição | Valor Padrão |
| --- | --- | --- |
| `OLLAMA_MODEL` | Modelo do Ollama a ser usado (ex: llama3.1, mistral) | `llama3.1` |
| `OLLAMA_MAX_REQUESTS` | Número máximo de requisições simultâneas | `5` |
| `OLLAMA_CHAR_LIMIT` | Limite de caracteres por requisição (opcional) | `10000` |
| `OLLAMA_CONTEXT_FUNCTION` | Contexto de função customizado para a IA (opcional) | - |
| `OLLAMA_CONTEXT_ADDITIONAL`| Contexto adicional customizado para a IA (opcional) | - |

# 📚 Documentação
- [Arquitetura do Sistema](ARCHITECTURE.md) - Padrões de projeto, fluxo de pipeline e concorrência (em inglês).
- [Visão de Produto & Compatibilidade](PRODUCT.md) - Escopo, motores de jogos atendidos e casos de uso (em inglês).
- [Guia de Códigos de Eventos RPG Maker](docs/engines/rpg_maker.md) - Especificação completa de comandos e códigos de formatação.

# 🤝 Contribuidores
 <a href="https://github.com/bgluis/extractor-and-translation/graphs/contributors">
   <img src="https://contrib.rocks/image?repo=bgluis/extractor-and-translation"/>
 </a>

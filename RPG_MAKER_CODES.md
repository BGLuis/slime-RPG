# Guia de Códigos de Eventos RPG Maker (MV/MZ)

Este documento descreve os códigos de comando de evento utilizados nos arquivos JSON do RPG Maker (Mapas, CommonEvents e Troops) e como eles são tratados pelo sistema de extração.

## 📝 Comandos de Texto e Diálogo

| Código | Nome | Descrição | Parâmetros Relevantes |
| :--- | :--- | :--- | :--- |
| **101** | Show Text (Init) | Inicializa uma janela de mensagem. | No MZ, o parâmetro 4 é o Nome do Orador. |
| **401** | Show Text (Line) | Contém uma linha de texto da mensagem iniciada pelo 101. | Parâmetro 0: Conteúdo da string. |
| **102** | Show Choices | Exibe uma lista de opções para o jogador. | Parâmetro 0: Lista de strings das opções. |
| **402** | Choice Branch | Identifica o bloco de comando para cada opção. | Parâmetro 1: Nome da opção (usado para referência). |
| **105** | Scrolling Text | Inicializa a exibição de texto em rolagem. | - |
| **405** | Scrolling Text (Line)| Contém uma linha do texto em rolagem. | Parâmetro 0: Conteúdo da string. |

## ⚙️ Comandos de Configuração e Dados

| Código | Nome | Descrição | Parâmetros Relevantes |
| :--- | :--- | :--- | :--- |
| **108** | Comment | Comentário do desenvolvedor no evento. | Parâmetro 0: Primeira linha do comentário. |
| **408** | Comment (Line) | Linhas subsequentes de um comentário. | Parâmetro 0: Conteúdo da string. |
| **118** | Label | Define um rótulo para o comando "Jump to Label". | Parâmetro 0: Nome do rótulo. |
| **122** | Control Variables | Altera o valor de uma variável. | Pode conter strings se for atribuição direta. |
| **320** | Change Name | Altera o nome de um herói (Actor). | Parâmetro 1: Novo nome. |
| **324** | Change Nickname | Altera a alcunha/apelido de um herói. | Parâmetro 1: Nova alcunha. |
| **325** | Change Profile | Altera o perfil de um herói. | Parâmetro 1: Nova descrição. |

## 💻 Scripts e Plugins

| Código | Nome | Descrição | Tratamento |
| :--- | :--- | :--- | :--- |
| **355** | Script | Executa código JavaScript puro. | O sistema extrai apenas textos dentro de aspas em funções conhecidas. |
| **655** | Script (Line) | Linhas subsequentes do script. | Tratado da mesma forma que o 355. |
| **356** | Plugin Command (MV)| Comando de plugin para RPG Maker MV. | Extrai textos de comandos como `D_TEXT` ou `mes`. |
| **357** | Plugin Command (MZ)| Comando de plugin para RPG Maker MZ. | Extrai valores de chaves não técnicas no dicionário de parâmetros. |

---

## 🔍 Referência de Códigos de Formato (Dentro do Texto)

Estes códigos aparecem dentro das strings e são protegidos/corrigidos pelo extrator:

*   **`\V[n]`**: Valor da variável `n`.
*   **`\N[n]`**: Nome do herói (Actor) `n`.
*   **`\P[n]`**: Nome do membro do grupo (Party) `n`.
*   **`\G`**: Unidade de moeda corrente (Gold).
*   **`\C[n]`**: Muda a cor do texto subsequente para a cor `n`.
*   **`\I[n]`**: Desenha o ícone `n`.
*   **`\M[id]`**: Referência a uma mensagem externa (comum em traduções de jogos japoneses).

## 📂 Atributos de Objetos (JSON Meta)

Além dos eventos, o extrator captura textos em arquivos de banco de dados (`Actors.json`, `Items.json`, etc.):

*   **`name`**: Nome do objeto.
*   **`description`**: Descrição curta.
*   **`note`**: Notas do desenvolvedor (pode conter tags de metadados como `<Desc: ...>`).
*   **`nickname`**: Alcunha (Heróis).
*   **`profile`**: Perfil (Heróis).
*   **`message1 / message2`**: Mensagens de batalha (Habilidades/Itens).

No arquivo **`System.json`**:
*   `gameTitle`: Título do jogo.
*   `terms`: Termos técnicos (Atributos, Comandos de Batalha).
*   `elements`: Nomes dos elementos (Fogo, Gelo, etc.).
*   `weaponTypes`: Nomes das categorias de armas.

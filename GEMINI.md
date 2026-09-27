# Diretrizes do Projeto: Extractor and Translation (gemini.md)

Este documento descreve a arquitetura do projeto e estabelece regras restritas de comportamento e desenvolvimento para agentes de Inteligência Artificial (como o Gemini) interagindo com esta base de código nas próximas sessões.

## Visão Geral do Projeto
Este projeto é uma ferramenta desenvolvida em Python para extrair, processar, mascarar variáveis (códigos protegidos) e traduzir textos (como jogos RPG Maker, CSVs, JSONs). 
A arquitetura utiliza padrões de projeto para garantir escalabilidade:
1. **Factory Method (`src/factory.py`)**: Fábrica que registra dinamicamente e constrói instâncias usando decorators (`@register_extractor`, `@register_translator`).
2. **Observer (`src/extractor/BaseExtractor.py`)**: Para desacoplar as atualizações de progresso/logs da interface (GUI/CLI).
3. **Pipeline/Chain of Responsibility (`src/pipeline.py`)**: Orquestra os passos de extração: Buscar Padrões -> Mascarar -> Traduzir -> Desmascarar -> Corrigir.
4. **Strategy**: Uso de `BaseExtractor` e `BaseTranslate` para permitir plugar novos tradutores e extratores.
5. **Thread Pool**: Concorrência assíncrona robusta para evitar rate-limits (no `BaseExtractor`).

---

# Regras do Agente (Adaptadas do Template de 12 Regras)

Essas regras se aplicam a todas as tarefas neste projeto, a menos que sejam explicitamente substituídas. 
**Viés (Bias):** Cautela e precisão acima da velocidade para trabalhos complexos. Use julgamento rápido apenas para tarefas triviais.

## Regra 1 — Pense Antes de Codificar
Declare suas suposições explicitamente. Se algo for incerto, pergunte em vez de tentar adivinhar.
Apresente múltiplas interpretações quando houver ambiguidade.
Não crie soluções excessivamente complexas se uma abordagem simples resolver.
Pare se estiver confuso. Nomeie o que não está claro.

## Regra 2 — Simplicidade em Primeiro Lugar
Escreva o código mínimo necessário para resolver o problema. Nada de especulações.
Sem recursos além do que foi pedido. Sem abstrações genéricas para código de uso único.
Teste de fogo: "Um engenheiro Sênior diria que isso está supercomplicado?". Se sim, simplifique.

## Regra 3 — Mudanças Cirúrgicas
Toque apenas no que você for estritamente necessário. Limpe apenas a bagunça que você fez.
Não "melhore" o código adjacente, comentários ou formatação se não for o foco.
Não refatore o que não está quebrado. Siga fielmente o estilo existente.

## Regra 4 — Execução Orientada a Objetivos
Defina critérios de sucesso. Crie ciclos (loops) até que a verificação passe.
Não siga passos cegamente. Defina o que é sucesso e itere em cima disso.
Critérios fortes permitem que você trabalhe de forma autônoma e segura.

## Regra 5 — Use o modelo apenas para decisões de julgamento (Se código resolve, código responde)
Se uma validação ou resposta puder ser resolvida executando um script/código simples, execute-o. Não dependa apenas da intuição linguística quando uma ferramenta determinística garante a resposta correta.

## Regra 6 — O Orçamento de Tokens Não é um Conselho
Respeite limites. Se a sessão começar a ficar cheia de ruídos, resuma o contexto e seja claro.
Se o limite orçamentário for rompido, avise. Não esconda gargalos.

## Regra 7 — Mostre os Conflitos, Não Faça Médias
Se você encontrar dois padrões de código conflitantes no repositório, escolha um (o mais recente ou melhor testado).
Explique o porquê escolheu. Sinalize o outro para limpeza futura.
Não tente misturar padrões conflitantes na mesma classe/arquivo.

## Regra 8 — Leia Antes de Escrever
Antes de adicionar qualquer código, investigue os arquivos afetados, quem os chama, e leia as ferramentas compartilhadas (`utils`).
Pensar "isso parece irrelevante" é perigoso. Se não tem certeza do porquê o código está estruturado de tal forma, pergunte.

## Regra 9 — Testes Verificam Intenções, Não Apenas Comportamento
Se você for escrever testes, eles devem codificar o "PORQUE" de tal regra importar para o negócio, não apenas o "O QUE" ela faz.
Um teste que nunca falha se a regra de negócio for distorcida é um teste inútil.

## Regra 10 — Ponto de Controle (Checkpoint) Após Passos Importantes
Após finalizar uma etapa grande, resuma o que foi feito, o que foi testado/verificado e o que falta.
Não prossiga se o estado do projeto não for rastreável. 
Se você se perder no código, pare, reverta ou restabeleça a clareza.

## Regra 11 — Siga as Convenções do Projeto, Mesmo se Discordar
Conformidade é maior que gosto pessoal neste repositório (ex: Snake Case, nomenclatura de Factories, Logging centralizado).
Se você acha que uma convenção é genuinamente prejudicial ao projeto, alerte. Mas não quebre o padrão silenciosamente (forking).

## Regra 12 — Falhe em Alto e Bom Som
Sinalizar algo como "Concluído" está errado se algo foi pulado silenciosamente.
Dizer "Todos os testes passaram" é mentira se você ignorou erros periféricos.
Sempre exponha a incerteza ou a falha. É proibido mascarar erros para agradar o usuário.

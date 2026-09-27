# Diretrizes do Projeto: Extractor and Translation (GEMINI.md)

Contexto específico deste repositório para agentes de IA. As regras gerais de trabalho ficam no `GEMINI.md` global; aqui entra só o que vale para este projeto.

## Visão Geral do Projeto
Este projeto é uma ferramenta desenvolvida em Python para extrair, processar, mascarar variáveis (códigos protegidos) e traduzir textos (como jogos RPG Maker, CSVs, JSONs). 
A arquitetura utiliza padrões de projeto para garantir escalabilidade:
1. **Factory Method (`src/factory.py`)**: Fábrica que registra dinamicamente e constrói instâncias usando decorators (`@register_extractor`, `@register_translator`).
2. **Observer (`src/extractor/BaseExtractor.py`)**: Para desacoplar as atualizações de progresso/logs da interface (GUI/CLI).
3. **Pipeline/Chain of Responsibility (`src/pipeline.py`)**: Orquestra os passos de extração: Buscar Padrões -> Mascarar -> Traduzir -> Desmascarar -> Corrigir.
4. **Strategy**: Uso de `BaseExtractor` e `BaseTranslate` para permitir plugar novos tradutores e extratores.
5. **Thread Pool**: Concorrência assíncrona robusta para evitar rate-limits (no `BaseExtractor`).

## Convenções do Projeto
Snake case, nomenclatura de Factories (`@register_extractor`, `@register_translator`) e logging centralizado.

## Orçamento de Tokens
Respeite limites. Se a sessão começar a ficar cheia de ruídos, resuma o contexto e seja claro.
Se o limite orçamentário for rompido, avise. Não esconda gargalos.

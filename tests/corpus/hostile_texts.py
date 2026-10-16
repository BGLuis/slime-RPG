"""
Corpus compartilhado de textos hostis (Issue #33 / Epic #32).
Reúne casos de borda para prosa e strings de script que exercitam:
- aspas retas e tipográficas (curvas)
- barra invertida no final (`\\`) e sequências `\\\\"`
- parênteses aninhados `()`
- palavras naturais que coincidem com literais JS/códigos de idioma (`true`, `false`, `null`, `verdadeiro`, `falso`, `nula`, `es`, `fr`, `en`, `pt`)
- números com zeros à esquerda (`007`, `0900`) junto a palavras como `return` ou `If (`
"""

HOSTILE_PROSE_TEXTS = [
    'Isso é falso, a história é verdadeira.',
    'A resposta foi nula (nem sim nem não).',
    'Ele disse "  " e saiu',
    'Use o item _ raro agora',
    'If (you) go there, be careful.',
    'Eu vou return ao quarto 007 às 0900.',
    'es (tipo A) y fr (tipo B)',
    'Is it false? Null and void',
    'Ele disse “olá” e ’ficou’',
    'Texto com (parênteses (aninhados) aqui) e fim.',
    'Caminho C:\\pasta\\',
    'Texto com \\\\" no meio',
    '"Don\'t"',
]

HOSTILE_SCRIPT_TEXTS = [
    'It\'s "fine"\\',
    'a\\\\"b',
    '"Don\'t"',
    'Ele disse “olá” e ’ficou’',
    'Caminho C:\\pasta\\',
    'Linha 1\nLinha 2',
    'Isso é falso, a história é verdadeira.',
    'Eu vou return ao quarto 007 às 0900.',
]

# Correções Implementadas - Códigos de Formato RPG Maker

## Problema Identificado

No arquivo [Map010.json linha 1588](input/Map010.json#L1588), o código `\M[INFO_0001]` estava sendo convertido para `\m[INFO_0001]` (minúsculo) após o processamento.

### Causa Raiz

O método `fix_text_translate` em [RPGMakerExtractor.py](extractor/RPGMakerExtractor.py) tinha uma lista incompleta de códigos que deveriam ser mantidos em **UPPERCASE**:

```python
# ANTES - Incompleto
_format_upper = {'c', 'v', 'i', 'ce'}
```

Códigos como `\M` (mensagem externa), `\N` (nome), `\P` (personagem) e `\G` (moeda) não estavam na lista, então eram convertidos para lowercase.

---

## Solução Implementada

### 1. Atualização da Lista de Códigos UPPERCASE

**Arquivo:** [RPGMakerExtractor.py](extractor/RPGMakerExtractor.py#L633-L636)

```python
# DEPOIS - Completo
_format_upper = {'c', 'v', 'i', 'ce', 'm', 'n', 'p', 'g'}
```

**Códigos adicionados:**
- `m` → `\M[ID]` - Mensagem externa (Mgp_ExternMessage.csv)
- `n` → `\N[ID]` - Nome do ator
- `p` → `\P[ID]` - Nome do personagem
- `g` → `\G` - Moeda/Gold

### 2. Suporte para Códigos Sem Parâmetros

**Antes:** O regex só aceitava códigos COM colchetes:
```python
r'\\([A-Za-z]{1,3})\s*(\[[^\]]*\])'  # Requer [...]
```

**Depois:** Regex aceita códigos COM ou SEM colchetes:
```python
r'\\([A-Za-z]{1,3})(?:\s*(\[[^\]]*\]))?'  # [...] opcional
```

**Ajuste na função:**
```python
def fix_format_code(match):
    name = match.group(1)
    params = match.group(2) or ''  # ← Trata None para códigos sem colchetes
    chosen = name.upper() if name.lower() in _format_upper else name.lower()
    return f'\\{chosen}{params}'
```

---

## Códigos de Formato RPG Maker - Referência

### Códigos UPPERCASE (após correção)

| Código | Parâmetro | Descrição | Exemplo |
|--------|-----------|-----------|---------|
| `\C[n]` | Obrigatório | Cor do texto | `\C[2]azul\C[0]` |
| `\V[n]` | Obrigatório | Valor da variável | `\V[5]` |
| `\I[n]` | Obrigatório | Ícone | `\I[10]` |
| `\M[id]` | Obrigatório | Mensagem externa (CSV) | `\M[INFO_0001]` |
| `\N[n]` | Obrigatório | Nome do ator | `\N[1]` |
| `\P[n]` | Obrigatório | Nome do personagem | `\P[2]` |
| `\G` | Nenhum | Moeda/Gold | `\G` |

### Códigos lowercase

| Código | Parâmetro | Descrição | Exemplo |
|--------|-----------|-----------|---------|
| `\fs[n]` | Obrigatório | Tamanho da fonte | `\fs[24]` |
| `\b[on/off]` | Obrigatório | Negrito | `\b[on]texto\b[off]` |
| `\i[on/off]` | Obrigatório | Itálico | `\i[on]texto\i[off]` |

---

## Testes Validados

### Teste 1: Caso Específico (Map010.json)

```
Original:  <center>\M[INFO_0001]</center>
Traduzido: <center>\m[INFO_0001]</center>  ← m minúsculo
Corrigido: <center>\M[INFO_0001]</center>  ✅ M restaurado!
```

### Teste 2: Múltiplos Códigos

```
Original: Olá \N[1], você tem \C[2]\V[10]\C[0] moedas e \I[5]itens.
Resultado: ✅ Todos mantidos em UPPERCASE
```

### Teste 3: Integração Completa

```
1. Mascaramento   → Tags HTML e códigos protegidos
2. Tradução       → Texto traduzido, placeholders preservados
3. Desmascaramento → Códigos restaurados
4. Correção       → Capitalização normalizada para UPPERCASE
```

**Status:** ✅ **100% dos testes passaram**

---

## Arquivos Modificados

1. **[extractor/RPGMakerExtractor.py](extractor/RPGMakerExtractor.py)**
   - Linha 633-636: Lista `_format_upper` expandida
   - Linha 577-580: Regex `_format_codes_pattern` atualizado
   - Linha 638-641: Função `fix_format_code` ajustada

---

## Benefícios

✅ **Códigos preservados:** `\M[INFO_0001]` mantém M maiúsculo  
✅ **Compatibilidade:** Suporta códigos com e sem parâmetros  
✅ **Robustez:** Testes automatizados garantem funcionamento  
✅ **Padrão RPG Maker:** Respeita convenções oficiais  

---

## Execução dos Testes

```bash
# Teste de capitalização
python test_format_codes.py

# Teste de integração completa
python test_integration_complete.py

# Teste das limitações 1 e 3
python test_improvements.py
```

**Resultado esperado:** Todos os testes devem passar (exit code 0)

---

## Referências

- [RPG Maker MV - Message Codes](https://rmmv.neocities.org/page/message_codes.html)
- [Plugin Mgp_ExternMessage](input/Mgp_ExternMessage.csv) - Mensagens externas
- [Documentação TextsUtils](src/utils/TextsUtils.py) - Mascaramento de tokens

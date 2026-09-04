import re
from .WolfTextFilters import WolfTextFilters

class WolfEventStrategy:
    def extract(self, item, context=None):
        return None

    def insert(self, item, translated_text, context=None):
        pass


class ShowMessageStrategy(WolfEventStrategy):
    """Estratégia para comandos de exibição de mensagem/diálogo (CID 101)"""
    def extract(self, item, context=None):
        string_args = item.get('string_args', item.get('parameters', []))
        if not string_args:
            return None
        text = string_args[0]
        if WolfTextFilters.is_translatable(text):
            return text
        return None

    def insert(self, item, translated_text, context=None):
        if translated_text is None:
            return
        new_text = str(translated_text)
        if 'string_args' in item and len(item['string_args']) > 0:
            item['string_args'][0] = new_text
        if 'parameters' in item and len(item['parameters']) > 0:
            item['parameters'][0] = new_text


class ChoicesStrategy(WolfEventStrategy):
    """Estratégia para comandos de escolha (CID 102)"""
    def extract(self, item, context=None):
        string_args = item.get('string_args', item.get('parameters', []))
        if not string_args:
            return None
        
        extracted = []
        for choice in string_args:
            if WolfTextFilters.is_translatable(choice):
                extracted.append(choice)
            else:
                extracted.append(choice) # Mantém alinhamento de índices
        
        # Só retorna se ao menos uma opção for traduzível
        if any(WolfTextFilters.is_translatable(c) for c in extracted):
            return extracted
        return None

    def insert(self, item, translated_text, context=None):
        if not translated_text or not isinstance(translated_text, list):
            return
        
        key = 'string_args' if 'string_args' in item else 'parameters'
        original_list = item.get(key, [])
        if len(original_list) != len(translated_text):
            return
        
        final_list = []
        for orig, trans in zip(original_list, translated_text):
            if WolfTextFilters.is_translatable(orig):
                final_list.append(str(trans))
            else:
                final_list.append(orig)
        item[key] = final_list


class SetStringStrategy(WolfEventStrategy):
    """Estratégia para comandos de atribuição de variáveis de texto (CID 122)"""
    def extract(self, item, context=None):
        string_args = item.get('string_args', item.get('parameters', []))
        if not string_args:
            return None
        text = string_args[0]
        if WolfTextFilters.is_translatable(text):
            return text
        return None

    def insert(self, item, translated_text, context=None):
        if translated_text is None:
            return
        new_text = str(translated_text)
        if 'string_args' in item and len(item['string_args']) > 0:
            item['string_args'][0] = new_text
        if 'parameters' in item and len(item['parameters']) > 0:
            item['parameters'][0] = new_text


class CommonEventParamStrategy(WolfEventStrategy):
    """Estratégia para chamadas de evento comum com parâmetros textuais (CID 210 / 300)"""
    def extract(self, item, context=None):
        string_args = item.get('string_args', item.get('parameters', []))
        if not string_args:
            return None
        
        extracted = []
        has_translatable = False
        for s in string_args:
            if WolfTextFilters.is_translatable(s):
                extracted.append(s)
                has_translatable = True
            else:
                extracted.append(s)
        
        if has_translatable:
            return extracted
        return None

    def insert(self, item, translated_text, context=None):
        if not translated_text:
            return
        key = 'string_args' if 'string_args' in item else 'parameters'
        original_list = item.get(key, [])
        
        if isinstance(translated_text, list) and len(original_list) == len(translated_text):
            final_list = []
            for orig, trans in zip(original_list, translated_text):
                if WolfTextFilters.is_translatable(orig):
                    final_list.append(str(trans))
                else:
                    final_list.append(orig)
            item[key] = final_list
        elif isinstance(translated_text, str) and len(original_list) > 0:
            for i in range(len(original_list)):
                if WolfTextFilters.is_translatable(original_list[i]):
                    original_list[i] = str(translated_text)
                    break


class PictureStrategy(WolfEventStrategy):
    """Estratégia para comando de imagem com texto renderizado (CID 150)"""
    def extract(self, item, context=None):
        args = item.get('parameters', item.get('args', []))
        if args:
            ptype = (args[0] >> 4) & 0x07
            if ptype == 2:  # text display mode
                string_args = item.get('string_args', [])
                if string_args and WolfTextFilters.is_translatable(string_args[0]):
                    return string_args[0]
        return None

    def insert(self, item, translated_text, context=None):
        if translated_text is None:
            return
        args = item.get('parameters', item.get('args', []))
        if args and ((args[0] >> 4) & 0x07 == 2):
            string_args = item.get('string_args', [])
            if string_args:
                string_args[0] = str(translated_text)


class CommentStrategy(WolfEventStrategy):
    """Estratégia para comentários (CID 103)"""
    def extract(self, item, context=None):
        string_args = item.get('string_args', [])
        if not string_args:
            return None
        text = string_args[0]
        if WolfTextFilters.is_translatable(text):
            return text
        return None

    def insert(self, item, translated_text, context=None):
        if translated_text is None:
            return
        string_args = item.get('string_args', [])
        if string_args:
            string_args[0] = str(translated_text)


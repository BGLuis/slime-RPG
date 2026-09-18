from src.extractor.BaseExtractor import BaseExtractor
from src.factory import register_extractor

@register_extractor("Json")
class JsonExtractor(BaseExtractor):
    files_types = ['json']

    @classmethod
    def get_interactive_questions(cls):
        return []

    def apply_configuration(self, config):
        pass

    def get_mask_patterns(self, file_name=None, data=None):
        import re
        p_format_codes = re.compile(r'\\[A-Za-z]+\s*\[[^\]]*\]', re.IGNORECASE)
        p_angle_codes = re.compile(r'\\[A-Za-z]+\s*<.*?>', re.IGNORECASE)
        p_conditions = re.compile(r'@(?:if|else)[^\n]*', re.IGNORECASE)
        p_template_expr = re.compile(r'\$\{[^}]*\}')
        p_game_expr = re.compile(r'\$game(?:Variables|Switches|Party|Actors|Player|Map|System|Screen)(?:\.[a-zA-Z0-9_]+(?:\([^)]*\))?)*', re.IGNORECASE)
        p_arrow_func = re.compile(r'^\s*\(?[a-zA-Z0-9_,\s]*\)?\s*=>.*$', re.DOTALL)
        p_bracket_vars = re.compile(r'!?(?<!\\)(?<![a-zA-Z0-9_])[A-Za-z]{1,2}\s*\[\s*\d+\s*\]', re.IGNORECASE)
        p_printf = re.compile(r'%[0-9]*\.?[0-9]*[sdfoxX]')
        p_curly_vars = re.compile(r'\{[a-zA-Z0-9_]+\}')
        p_unity_tags = re.compile(r'</?[a-zA-Z][a-zA-Z0-9=_\-#]*(?:\s+[^>]*)?/?>', re.IGNORECASE)
        return [p_format_codes, p_angle_codes, p_conditions, p_template_expr, p_game_expr, p_arrow_func, p_bracket_vars, p_printf, p_curly_vars, p_unity_tags]

    def __init__(self, translate):
        super().__init__(translate)

    @staticmethod
    def extract_text(file_name, data):
        if not isinstance(data, (dict, list)):
            return None
        return data

    @staticmethod
    def update_json(file_name, data, new_data):
        if not isinstance(data, (dict, list)) or not isinstance(new_data, (dict, list)):
            return None
        
        cleaned_data = BaseExtractor.remove_old_keys(new_data)
        if isinstance(data, dict):
            data.update(cleaned_data)
            return data
        else:
            return cleaned_data

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text

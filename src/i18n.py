import locale
import os
import sys

DEFAULT_LANGUAGE = 'en'

LANGUAGES = {
    'pt': 'Português',
    'en': 'English',
}

_STRINGS = {
    'pt': {
        'window_title_verification': 'Verificação Manual',
        'verification_text': (
            "A tradução foi pausada para sua verificação.\n\n"
            "Por favor, revise os arquivos processados na pasta:\n"
            "<b>{folder}</b>\n\n"
            "Quando terminar, clique em 'Continuar' para exportar os arquivos."
        ),
        'btn_open_folder': ' Abrir Pasta de Processamento',
        'btn_continue': 'Continuar',
        'btn_abort': 'Abortar',

        'window_title_main': 'Translator',
        'group_config': 'Configurações de Tradução',
        'label_extractor': 'Extrator de Texto:',
        'label_translator': 'Agente de Tradução:',
        'label_source_lang': 'Idioma de Origem:',
        'label_target_lang': 'Idioma de Destino:',
        'label_game_folder': 'Pasta do Jogo:',
        'placeholder_game_folder': 'Selecione a pasta contendo os arquivos do jogo...',
        'btn_browse': ' Procurar',
        'label_synopsis': 'Sinopse / Contexto (Melhora Tradução IA):',
        'placeholder_synopsis': 'Ex: Um jogo de RPG sobre um herói que viaja no tempo...',
        'chk_backup': 'Criar Backup Automático',
        'chk_verify': 'Pausar para Verificação',
        'btn_start': ' INICIAR TRADUÇÃO',
        'btn_processing': ' PROCESSANDO...',
        'label_log_output': 'Saída de Log:',
        'tooltip_clear_log': 'Limpar Logs',
        'status_ready': 'Pronto para traduzir',
        'label_ui_language': 'Idioma da Interface:',
        'dialog_select_folder': 'Selecionar Pasta do Jogo',

        'log_folder_adjusted': 'Pasta ajustada automaticamente para dados do jogo: {name}',
        'log_auto_detected_folder': '🎮 Pasta de dados do jogo detectada automaticamente: {path}',
        'log_extractor_suggested': 'Extrator sugerido automaticamente: {name}',
        'error_invalid_folder': 'Erro: Pasta de entrada inválida.',
        'error_same_lang': 'Erro: Idioma de origem e destino não podem ser iguais.',
        'log_starting': 'Iniciando tradução com {extractor} e {translator}...',
        'log_from_to': 'De {source} para {target}',
        'status_processing': 'Processando: {done}/{total} arquivos',
        'log_finished_success': 'Tradução finalizada com sucesso!',
        'status_done': 'Concluído!',
        'log_finished_error': 'Tradução interrompida ou com erros.',
        'status_error': 'Erro no processamento',
        'status_language_changed': 'Idioma alterado para {language}. Reinicie o aplicativo para aplicar.',

        'cli_binding_quit': 'Sair',
        'cli_binding_continue': 'Continuar',
        'cli_binding_select_folder': 'Selecionar esta pasta',
        'cli_binding_cancel': 'Cancelar',
        'cli_binding_paste': 'Colar da Área de Transferência',
        'cli_placeholder_folder_input': 'Digite ou cole o caminho aqui e tecle Enter (ou navegue na árvore)...',
        'cli_clipboard_folder_detected_title': 'Detectamos uma pasta na sua área de transferência:\n📂 {path}\n\nDeseja utilizá-la?',
        'cli_detected_folder_prompt': '🎮 Detectamos arquivos de jogo nesta pasta:\n📂 {path} ({desc})\n\nDeseja utilizá-la para tradução?',
        'cli_opt_use_detected_folder': '🎮 Usar pasta detectada ({path})',
        'cli_opt_use_current_folder': '📂 Usar pasta atual onde o comando foi invocado ({path})',
        'cli_opt_use_clipboard': 'Usar pasta da área de transferência ({path})',
        'cli_opt_type_path': 'Digitar ou colar caminho manualmente',
        'cli_opt_browse_tree': 'Navegar pela árvore de pastas (Explorador)',
        'prompt_type_folder_path': 'Digite ou cole o caminho da pasta:',
        'prompt_try_again_or_empty': 'Tente novamente ou pressione Enter vazio para cancelar:',
        'cli_default_select_folder_title': 'Selecione uma pasta',
        'cli_table_file': 'Arquivo',
        'cli_table_status': 'Status',
        'cli_table_message': 'Mensagem',
        'cli_table_progress': 'Progresso',

        'cli_extractors_available': '\n=== EXTRATORES DISPONÍVEIS ===',
        'cli_translators_available': '\n=== TRADUTORES DISPONÍVEIS ===',
        'cli_languages_supported': '\n=== IDIOMAS SUPORTADOS ===',

        'error_extractor_not_found': "Extrator '{name}' não encontrado. Opções: {options}",
        'error_translator_not_found': "Tradutor '{name}' não encontrado. Opções: {options}",
        'error_source_lang_unsupported': "Idioma de origem '{lang}' não suportado. Opções: {options}",
        'error_target_lang_unsupported': "Idioma de destino '{lang}' não suportado. Opções: {options}",
        'error_input_folder_not_exist': "Pasta de entrada '{path}' não existe",
        'error_same_lang_bare': 'Idioma de origem e destino não podem ser iguais',

        'prompt_select_extractor': 'Selecione o extrator de texto:',
        'prompt_select_translator': 'Selecione o tradutor:',
        'prompt_select_source_lang': 'Selecione o idioma de origem:',
        'prompt_select_target_lang': 'Selecione o idioma de destino:',
        'prompt_what_to_do': 'O que você deseja fazer?',
        'prompt_how_to_save': 'Como deseja salvar os arquivos?',
        'prompt_input_folder': "\nDigite o caminho da pasta com os arquivos (ou pressione Enter para './input'): ",
        'prompt_press_enter_select_folder': 'Pressione enter para selecionar a pasta de entrada dos arquivos',
        'dialog_select_input_folder': 'Selecione a pasta de entrada dos arquivos',
        'error_no_folder_selected': 'Nenhuma pasta selecionada. Encerrando o programa.',
        'instruction_verify_folder': "Verifique a tradução dos arquivos na pasta '{path}', e pressione enter tecla para continuar",

        'mode_content': 'Extrair e Traduzir Conteúdo de Arquivos',
        'mode_filenames': 'Traduzir Nomes de Arquivos',
        'mode_exit': 'Sair',

        'save_translation_only': 'Salvar apenas a tradução (Renomear/Copiar com novo nome)',
        'save_both': 'Salvar os dois juntos (Manter original e criar cópia traduzida)',

        'error_folder_not_found': "Pasta '{path}' não encontrada.",
        'error_folder_in_trash': "❌ Erro: A pasta '{path}' está localizada na lixeira do sistema (.Trash). Por favor, restaure o jogo ou selecione a pasta original fora da lixeira.",
        'error_folder_is_repo_root': "❌ Erro: A pasta '{path}' é a raiz do código da ferramenta de tradução e não uma pasta de jogo válida.",
        'log_synopsis_configured': '✓ Sinopse configurada via CLI',
        'error_field_required': '\n✗ {field} é obrigatório!',

        'log_using_extractor': 'Usando extrator: {name}',
        'log_using_translator': 'Usando tradutor: {name}',
        'log_translating_from_to': 'Traduzindo de {source} para {target}',
        'log_input_folder': 'Pasta de entrada: {path}',

        'log_backup_exists': "Backup já existe em '{path}'; mantendo o original preservado e pulando nova cópia.",
        'log_creating_backup': 'Criando backup em: {path}',
        'log_processing_files': 'Iniciando processamento dos arquivos...',
        'log_waiting_verification': "Aguardando verificação manual da pasta '{path}'...",
        'log_verification_aborted': 'Processamento abortado pelo usuário na verificação.',
        'log_gui_verification_notice': "Aviso: Verificação manual simplificada no modo GUI. Verifique a pasta '{path}'.",
        'log_exporting_files': 'Exportando arquivos',
        'log_processing_error': 'Erro durante o processamento: {error}',
        'log_cache_save_warning': 'Aviso: falha ao salvar o cache de traduções: {error}',

        'error_gui_unavailable': 'Erro: Não foi possível carregar a interface gráfica. Verifique se o PyQt5 está instalado.',
        'log_details': 'Detalhes: {error}',
        'log_starting_terminal_mode': '\nIniciando modo terminal em 3 segundos...',
        'error_cli_arguments': 'Erros nos argumentos CLI:',
        'log_starting_interactive_mode': '\nIniciando modo interativo para corrigir...',
        'log_operation_cancelled': '\n\nOperação cancelada pelo usuário.',
        'error_fatal': '\nErro fatal: {error}',
    },
    'en': {
        'window_title_verification': 'Manual Verification',
        'verification_text': (
            "Translation has been paused for your verification.\n\n"
            "Please review the processed files in the folder:\n"
            "<b>{folder}</b>\n\n"
            "When you're done, click 'Continue' to export the files."
        ),
        'btn_open_folder': ' Open Processing Folder',
        'btn_continue': 'Continue',
        'btn_abort': 'Abort',

        'window_title_main': 'Translator',
        'group_config': 'Translation Settings',
        'label_extractor': 'Text Extractor:',
        'label_translator': 'Translation Agent:',
        'label_source_lang': 'Source Language:',
        'label_target_lang': 'Target Language:',
        'label_game_folder': 'Game Folder:',
        'placeholder_game_folder': 'Select the folder containing the game files...',
        'btn_browse': ' Browse',
        'label_synopsis': 'Synopsis / Context (Improves AI Translation):',
        'placeholder_synopsis': 'E.g.: An RPG game about a hero who travels through time...',
        'chk_backup': 'Create Automatic Backup',
        'chk_verify': 'Pause for Verification',
        'btn_start': ' START TRANSLATION',
        'btn_processing': ' PROCESSING...',
        'label_log_output': 'Log Output:',
        'tooltip_clear_log': 'Clear Logs',
        'status_ready': 'Ready to translate',
        'label_ui_language': 'Interface Language:',
        'dialog_select_folder': 'Select Game Folder',

        'log_folder_adjusted': 'Folder automatically adjusted to game data: {name}',
        'log_auto_detected_folder': '🎮 Game data folder automatically detected: {path}',
        'log_extractor_suggested': 'Extractor automatically suggested: {name}',
        'error_invalid_folder': 'Error: Invalid input folder.',
        'error_same_lang': 'Error: Source and target language cannot be the same.',
        'log_starting': 'Starting translation with {extractor} and {translator}...',
        'log_from_to': 'From {source} to {target}',
        'status_processing': 'Processing: {done}/{total} files',
        'log_finished_success': 'Translation finished successfully!',
        'status_done': 'Done!',
        'log_finished_error': 'Translation interrupted or finished with errors.',
        'status_error': 'Error during processing',
        'status_language_changed': 'Language changed to {language}. Restart the application to apply it.',

        'cli_binding_quit': 'Quit',
        'cli_binding_continue': 'Continue',
        'cli_binding_select_folder': 'Select this folder',
        'cli_binding_cancel': 'Cancel',
        'cli_binding_paste': 'Paste from Clipboard',
        'cli_placeholder_folder_input': 'Type or paste path here and press Enter (or browse tree below)...',
        'cli_clipboard_folder_detected_title': 'We detected a folder path in your clipboard:\n📂 {path}\n\nDo you want to use it?',
        'cli_detected_folder_prompt': '🎮 Game files detected in this folder:\n📂 {path} ({desc})\n\nDo you want to use it for translation?',
        'cli_opt_use_detected_folder': '🎮 Use detected folder ({path})',
        'cli_opt_use_current_folder': '📂 Use current folder where command was launched ({path})',
        'cli_opt_use_clipboard': 'Use clipboard folder ({path})',
        'cli_opt_type_path': 'Type or paste path manually',
        'cli_opt_browse_tree': 'Browse folder tree (Explorer)',
        'prompt_type_folder_path': 'Type or paste folder path:',
        'prompt_try_again_or_empty': 'Try again or press empty Enter to cancel:',
        'cli_default_select_folder_title': 'Select a folder',
        'cli_table_file': 'File',
        'cli_table_status': 'Status',
        'cli_table_message': 'Message',
        'cli_table_progress': 'Progress',

        'cli_extractors_available': '\n=== AVAILABLE EXTRACTORS ===',
        'cli_translators_available': '\n=== AVAILABLE TRANSLATORS ===',
        'cli_languages_supported': '\n=== SUPPORTED LANGUAGES ===',

        'error_extractor_not_found': "Extractor '{name}' not found. Options: {options}",
        'error_translator_not_found': "Translator '{name}' not found. Options: {options}",
        'error_source_lang_unsupported': "Source language '{lang}' not supported. Options: {options}",
        'error_target_lang_unsupported': "Target language '{lang}' not supported. Options: {options}",
        'error_input_folder_not_exist': "Input folder '{path}' does not exist",
        'error_same_lang_bare': 'Source and target language cannot be the same',

        'prompt_select_extractor': 'Select the text extractor:',
        'prompt_select_translator': 'Select the translator:',
        'prompt_select_source_lang': 'Select the source language:',
        'prompt_select_target_lang': 'Select the target language:',
        'prompt_what_to_do': 'What do you want to do?',
        'prompt_how_to_save': 'How do you want to save the files?',
        'prompt_input_folder': "\nEnter the path of the folder with the files (or press Enter for './input'): ",
        'prompt_press_enter_select_folder': 'Press enter to select the input folder for the files',
        'dialog_select_input_folder': 'Select the input folder for the files',
        'error_no_folder_selected': 'No folder selected. Closing the program.',
        'instruction_verify_folder': "Check the translation of the files in the folder '{path}', and press enter to continue",

        'mode_content': 'Extract and Translate File Content',
        'mode_filenames': 'Translate File Names',
        'mode_exit': 'Exit',

        'save_translation_only': 'Save only the translation (Rename/Copy with new name)',
        'save_both': 'Save both (Keep original and create translated copy)',

        'error_folder_not_found': "Folder '{path}' not found.",
        'error_folder_in_trash': "❌ Error: Folder '{path}' is inside the system trash (.Trash). Please restore the game or select the original folder outside the trash.",
        'error_folder_is_repo_root': "❌ Error: Folder '{path}' is the translation tool repository root and not a valid game folder.",
        'log_synopsis_configured': '✓ Synopsis configured via CLI',
        'error_field_required': '\n✗ {field} is required!',

        'log_using_extractor': 'Using extractor: {name}',
        'log_using_translator': 'Using translator: {name}',
        'log_translating_from_to': 'Translating from {source} to {target}',
        'log_input_folder': 'Input folder: {path}',

        'log_backup_exists': "Backup already exists at '{path}'; keeping the original preserved and skipping a new copy.",
        'log_creating_backup': 'Creating backup at: {path}',
        'log_processing_files': 'Starting file processing...',
        'log_waiting_verification': "Waiting for manual verification of folder '{path}'...",
        'log_verification_aborted': 'Processing aborted by the user during verification.',
        'log_gui_verification_notice': "Notice: simplified manual verification in GUI mode. Check the folder '{path}'.",
        'log_exporting_files': 'Exporting files',
        'log_processing_error': 'Error during processing: {error}',
        'log_cache_save_warning': 'Warning: failed to save the translation cache: {error}',

        'error_gui_unavailable': 'Error: Could not load the graphical interface. Check if PyQt5 is installed.',
        'log_details': 'Details: {error}',
        'log_starting_terminal_mode': '\nStarting terminal mode in 3 seconds...',
        'error_cli_arguments': 'Errors in CLI arguments:',
        'log_starting_interactive_mode': '\nStarting interactive mode to fix them...',
        'log_operation_cancelled': '\n\nOperation cancelled by the user.',
        'error_fatal': '\nFatal error: {error}',
    },
}


def detect_system_language():
    """Tenta descobrir o idioma padrão do sistema operacional; cai para DEFAULT_LANGUAGE se não reconhecido."""
    candidates = []

    for var in ('LANGUAGE', 'LC_ALL', 'LC_MESSAGES', 'LANG'):
        value = os.environ.get(var)
        if value:
            candidates.append(value)

    try:
        current_locale = locale.getlocale()[0]
        if current_locale:
            candidates.append(current_locale)
    except Exception:
        pass

    if sys.platform == 'win32':
        try:
            import ctypes
            windows_id = ctypes.windll.kernel32.GetUserDefaultUILanguage()
            windows_lang = locale.windows_locale.get(windows_id)
            if windows_lang:
                candidates.append(windows_lang)
        except Exception:
            pass

    for candidate in candidates:
        primary = candidate.replace('-', '_').split('_')[0].lower()
        if primary in LANGUAGES:
            return primary

    return DEFAULT_LANGUAGE


class Translator:
    """Resolve chaves de string para o idioma de interface selecionado."""

    def __init__(self, language=None):
        self.language = language if language in LANGUAGES else detect_system_language()

    def set_language(self, language):
        if language in LANGUAGES:
            self.language = language

    def tr(self, key, **kwargs):
        text = _STRINGS.get(self.language, {}).get(key)
        if text is None:
            text = _STRINGS[DEFAULT_LANGUAGE].get(key, key)
        return text.format(**kwargs) if kwargs else text

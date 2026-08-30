import src.extractor  # noqa: F401 (Importado para registrar as classes na Factory)
import src.translate  # noqa: F401 (Importado para registrar as classes na Factory)
from src.services.FileNameTranslator import FileNameTranslator
import shutil
from src import cli
from src.cli import _i18n  # instância compartilhada: a paleta de idioma do cli.py também retraduz aqui
import os
import argparse
import sys
import time
import logging
from logging.handlers import RotatingFileHandler

def setup_logging():
    logger = logging.getLogger()
    logger.setLevel(logging.INFO)
    formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s')
    file_handler = RotatingFileHandler('processamento.log', maxBytes=5*1024*1024, backupCount=2, encoding='utf-8')
    file_handler.setFormatter(formatter)
    logger.addHandler(file_handler)

lang_options = {
    'Português': 'pt',
    'Inglês': 'en',
    'Espanhol': 'es',
    'Francês': 'fr',
    'Italiano': 'it',
    'Japonês': 'ja',
    'Automático (Detectar)': 'auto'
}

from src.factory import ExtractorFactory, TranslatorFactory

def target_lang_options(lang_source):
    """Opções de idioma de destino: sem o idioma de origem e, se a origem não for
    'auto', também sem a opção 'auto'."""
    options = {k: v for k, v in lang_options.items() if v != lang_source}
    if lang_source != 'auto':
        options = {k: v for k, v in options.items() if v != 'auto'}
    return options

def copy_file(src, dst):
    shutil.copy(src, dst)

def copy_folder(src, dst):
    shutil.copytree(src, dst)

def delete_file_folder(pasta):
    for arquivo in os.listdir(pasta):
        caminho = os.path.join(pasta, arquivo)
        if os.path.isfile(caminho):
            os.remove(caminho)

def copy_files_only(src, dst, clear_destination=True):
    src_real = os.path.realpath(src)
    dst_real = os.path.realpath(dst)

    # Evita apagar os arquivos quando origem e destino apontam para a mesma pasta.
    if src_real == dst_real:
        return

    if not os.path.exists(dst):
        os.makedirs(dst)

    # clear_destination=False é usado quando dst pode conter arquivos sem
    # equivalente em src (ex: um arquivo que falhou o processamento) e que,
    # portanto, não podem ser apagados sem substituto.
    if clear_destination:
        delete_file_folder(dst)

    for item in os.listdir(src):
        s = os.path.join(src, item)
        d = os.path.join(dst, item)
        if os.path.isfile(s):
            shutil.copy2(s, d)


def parse_arguments():
    """Parse command line arguments"""
    parser = argparse.ArgumentParser(
        description='Sistema de extração e tradução de textos',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Exemplos de uso:
  python main.py --gui
  python main.py --interactive
  python main.py -e RPGMaker -t Google -s pt -d en -i /path/to/input
  python main.py --extractor JsonExtractor --translator Ollama --source ja --target pt --input ./data --no-backup
  python main.py --list-extractors
  python main.py --list-translators
        """
    )

    parser.add_argument('-g', '--gui', action='store_true',
                       help='Iniciar interface gráfica (PyQt5)')
    parser.add_argument('-e', '--extractor',
                       help='Tipo de extrator (use --list-extractors para ver opções)')
    parser.add_argument('-t', '--translator',
                       help='Tipo de tradutor (use --list-translators para ver opções)')
    parser.add_argument('-s', '--source',
                       help='Idioma de origem (pt, en, es, fr, it, ja, auto)')
    parser.add_argument('-d', '--target',
                       help='Idioma de destino (pt, en, es, fr, it, ja)')
    parser.add_argument('-i', '--input',
                       help='Pasta de entrada dos arquivos')
    parser.add_argument('--synopsis', '--sinopse',
                       help='Sinopse do jogo para melhorar contexto da tradução')

    parser.add_argument('--no-backup', action='store_true',
                       help='Não criar backup dos arquivos originais')
    parser.add_argument('--no-verify', action='store_true',
                       help='Não pausar para verificação manual dos arquivos processados')
    parser.add_argument('--interactive', action='store_true',
                       help='Modo interativo (padrão se nenhum argumento for fornecido)')

    parser.add_argument('--mode', choices=['content', 'filenames'], default='content',
                       help='Modo de operação: traduzir conteúdo de arquivos (content) ou nomes de arquivos (filenames)')
    parser.add_argument('--keep-original', action='store_true',
                       help='No modo filenames, manter o arquivo original e criar uma cópia traduzida')

    parser.add_argument('--list-extractors', action='store_true',
                       help='Listar extratores disponíveis')
    parser.add_argument('--list-translators', action='store_true',
                       help='Listar tradutores disponíveis')
    parser.add_argument('--list-languages', action='store_true',
                       help='Listar idiomas suportados')

    return parser.parse_args()


def list_options():
    """Print available options and exit"""
    print(_i18n.tr('cli_extractors_available'))
    extractors = ExtractorFactory.get_available()
    for name in extractors.keys():
        print(f"  - {name}")

    print(_i18n.tr('cli_translators_available'))
    translators = TranslatorFactory.get_available()
    for name in translators.keys():
        print(f"  - {name}")

    print(_i18n.tr('cli_languages_supported'))
    for lang_name, lang_code in lang_options.items():
        print(f"  - {lang_code}: {lang_name}")


def validate_arguments(args):
    errors = []

    if args.extractor:
        match = ExtractorFactory.find_match(args.extractor)
        if match:
            args.extractor = match
        else:
            extractors = ExtractorFactory.get_available()
            errors.append(_i18n.tr('error_extractor_not_found', name=args.extractor, options=list(extractors.keys())))

    if args.translator:
        match = TranslatorFactory.find_match(args.translator)
        if match:
            args.translator = match
        else:
            translators = TranslatorFactory.get_available()
            errors.append(_i18n.tr('error_translator_not_found', name=args.translator, options=list(translators.keys())))

    if args.source and args.source not in lang_options.values():
        errors.append(_i18n.tr('error_source_lang_unsupported', lang=args.source, options=list(lang_options.values())))

    if args.target and args.target not in lang_options.values():
        errors.append(_i18n.tr('error_target_lang_unsupported', lang=args.target, options=list(lang_options.values())))

    if args.input:
        if not os.path.exists(args.input):
            errors.append(_i18n.tr('error_input_folder_not_exist', path=args.input))
        else:
            from src.services.GameDetector import is_trash_path, is_project_repo_root
            if is_trash_path(args.input):
                errors.append(_i18n.tr('error_folder_in_trash', path=args.input))
            elif is_project_repo_root(args.input):
                errors.append(_i18n.tr('error_folder_is_repo_root', path=args.input))

    if args.source and args.target and args.source == args.target:
        errors.append(_i18n.tr('error_same_lang_bare'))

    return errors


def run_filename_translation_workflow(args, translators):
    """Fluxo de trabalho para tradução de nomes de arquivos"""
    # 1. Selecionar Tradutor
    if args.translator and args.translator in translators:
        translator_class = translators[args.translator]
    else:
        translator_class = cli.select_option(lambda: _i18n.tr('prompt_select_translator'), translators)
        if translator_class == "Exit": return False

    translate = TranslatorFactory.create(translator_class.agent)

    # 2. Selecionar Idioma de Origem
    if args.source and args.source in lang_options.values():
        lang_source = args.source
    else:
        lang_source = cli.select_option(lambda: _i18n.tr('prompt_select_source_lang'), lang_options)
        if lang_source == "Exit": return False

    # 3. Selecionar Idioma de Destino
    if args.target and args.target in lang_options.values() and args.target != lang_source:
        lang_target = args.target
    else:
        lang_target = cli.select_option(lambda: _i18n.tr('prompt_select_target_lang'), target_lang_options(lang_source))
        if lang_target == "Exit": return False

    translate.change_language(lang_source, lang_target)

    # 4. Configuração do Tradutor
    if args.synopsis:
        translate.apply_configuration({'synopsis': args.synopsis})
    else:
        translator_questions = translator_class.get_interactive_questions()
        if translator_questions:
            cli.clear_screen()
            translator_config = {}
            for question in translator_questions:
                if 'title' in question: cli.print_colored_line(question['title'], 'cyan')
                cli.print_colored_line(question['question'], question.get('color', 'white'))
                answer = input().strip()
                if answer or not question.get('required', False):
                    translator_config[question['key']] = answer
            translate.apply_configuration(translator_config)

    # 5. Opção de Salvar (Apenas tradução ou Ambos)
    keep_original = args.keep_original
    if not args.keep_original:
        def save_options():
            return {
                _i18n.tr('save_translation_only'): False,
                _i18n.tr('save_both'): True
            }
        keep_original = cli.select_option(lambda: _i18n.tr('prompt_how_to_save'), save_options)
        if keep_original == "Exit": return False

    # 6. Selecionar Pasta de Entrada
    if args.input and os.path.exists(args.input):
        input_dir = args.input
    else:
        cli.instruction(lambda: _i18n.tr('prompt_press_enter_select_folder'))
        input_dir = cli.select_folder(lambda: _i18n.tr('dialog_select_input_folder'))
        if input_dir is None:
            cli.print_colored_line(_i18n.tr('error_no_folder_selected'), 'red')
            return False

    # 7. Executar Tradução
    fn_translator = FileNameTranslator(translate)
    fn_translator.process_directory(input_dir, keep_original)
    return True


def run_workflow(args):
    """
    Fluxo de trabalho unificado que usa argumentos CLI se fornecidos,
    caso contrário, solicita ao usuário de forma interativa.
    """
    extractors = ExtractorFactory.get_available()
    translators = TranslatorFactory.get_available()

    from src.services.GameDetector import detect_game_environment
    caller_dir = os.environ.get('CALLER_WORKING_DIR')
    detected_env = detect_game_environment(args.input or caller_dir)

    if detected_env and not args.input:
        args.input = detected_env['detected_path']
        cli.print_colored_line(_i18n.tr('log_auto_detected_folder', path=args.input), 'green')

    # Selecionar Modo de Operação
    mode = args.mode
    if mode == 'content' and not any([args.extractor, args.translator, args.input, args.gui]):
        # Se nenhum argumento relevante foi passado, pergunta o modo
        def modes():
            return {
                _i18n.tr('mode_content'): 'content',
                _i18n.tr('mode_filenames'): 'filenames',
                _i18n.tr('mode_exit'): 'Exit'
            }
        mode = cli.select_option(lambda: _i18n.tr('prompt_what_to_do'), modes)
        if mode == "Exit": return False

    if mode == 'filenames':
        return run_filename_translation_workflow(args, translators)

    # 1. Selecionar Extrator
    if args.extractor and args.extractor in extractors:
        extractor_class = extractors[args.extractor]
    elif detected_env and detected_env.get('extractor') in extractors and not any([args.extractor, args.translator]):
        extractor_class = extractors[detected_env['extractor']]
        cli.print_colored_line(_i18n.tr('log_extractor_suggested', name=extractor_class.name), 'green')
    else:
        extractor_class = cli.select_option(lambda: _i18n.tr('prompt_select_extractor'), extractors)
        if extractor_class == "Exit": return False

    # 2. Selecionar Tradutor
    if args.translator and args.translator in translators:
        translator_class = translators[args.translator]
    else:
        translator_class = cli.select_option(lambda: _i18n.tr('prompt_select_translator'), translators)
        if translator_class == "Exit": return False

    translate = TranslatorFactory.create(translator_class.agent)

    # 3. Selecionar Idioma de Origem
    if args.source and args.source in lang_options.values():
        lang_source = args.source
    else:
        lang_source = cli.select_option(lambda: _i18n.tr('prompt_select_source_lang'), lang_options)
        if lang_source == "Exit": return False

    # 4. Selecionar Idioma de Destino
    if args.target and args.target in lang_options.values() and args.target != lang_source:
        lang_target = args.target
    else:
        lang_target = cli.select_option(lambda: _i18n.tr('prompt_select_target_lang'), target_lang_options(lang_source))
        if lang_target == "Exit": return False

    translate.change_language(lang_source, lang_target)

    # 5. Configuração do Tradutor
    if args.synopsis:
        translate.apply_configuration({'synopsis': args.synopsis})
        cli.print_colored_line(_i18n.tr('log_synopsis_configured'), 'green')
    else:
        translator_questions = translator_class.get_interactive_questions()
        if translator_questions:
            cli.clear_screen()
            translator_config = {}
            for question in translator_questions:
                if 'title' in question: cli.print_colored_line(question['title'], 'cyan')
                if 'description' in question: cli.print_colored_line(question['description'], question.get('color', 'yellow'))
                cli.print_colored_line(question['question'], question.get('color', 'white'))
                answer = input().strip()
                if answer or not question.get('required', False):
                    translator_config[question['key']] = answer
                else:
                    cli.print_colored_line(_i18n.tr('error_field_required', field=question['key'].capitalize()), 'red')
                    return False
            translate.apply_configuration(translator_config)

    # 6. Configuração do Extrator
    extractor = ExtractorFactory.create(extractor_class.name, translate)
    extractor_questions = extractor.get_interactive_questions()
    if extractor_questions:
        cli.clear_screen()
        extractor_config = {}
        for question in extractor_questions:
            if 'title' in question: cli.print_colored_line(question['title'], 'cyan')
            if 'description' in question: cli.print_colored_line(question['description'], question.get('color', 'yellow'))
            cli.print_colored_line(question['question'], question.get('color', 'white'))
            answer = input().strip()
            if answer or not question.get('required', False):
                extractor_config[question['key']] = answer
            else:
                cli.print_colored_line(_i18n.tr('error_field_required', field=question['key'].capitalize()), 'red')
                return False
        extractor.apply_configuration(extractor_config)

    extractor.init_folder()

    # 7. Selecionar Pasta de Entrada
    if args.input and os.path.exists(args.input):
        input_dir = args.input
    else:
        cli.instruction(lambda: _i18n.tr('prompt_press_enter_select_folder'))
        input_dir = cli.select_folder(lambda: _i18n.tr('dialog_select_input_folder'))
        if input_dir is None:
            cli.print_colored_line(_i18n.tr('error_no_folder_selected'), 'red')
            return False

    # Auto-ajuste para subpastas de dados (ex: www/data ou data) caso selecionada a raiz do jogo
    detected_input = detect_game_environment(input_dir)
    if detected_input and detected_input['detected_path'] != os.path.abspath(input_dir):
        input_dir = detected_input['detected_path']
        cli.print_colored_line(_i18n.tr('log_folder_adjusted', name=os.path.basename(input_dir)), 'green')

    # 8. Executar Processo
    cli.clear_screen()
    cli.print_colored_line(_i18n.tr('log_using_extractor', name=extractor_class.name), 'cyan')
    cli.print_colored_line(_i18n.tr('log_using_translator', name=translator_class.agent), 'cyan')
    cli.print_colored_line(_i18n.tr('log_translating_from_to', source=lang_source, target=lang_target), 'cyan')
    cli.print_colored_line(_i18n.tr('log_input_folder', path=input_dir), 'cyan')

    return run_extraction_process(
        extractor, translate, input_dir, lang_source, 
        backup=not args.no_backup, 
        verify=not args.no_verify
    )


def run_extraction_process(extractor, translate, input_dir, lang_source, backup=True, verify=True, gui_signals=None, gui_verify_callback=None):
    """Run the main extraction and translation process"""
    def log(msg, color='white'):
        logging.info(msg)
        if gui_signals:
            gui_signals.log.emit(msg, color)
        else:
            cli.print_colored_line(msg, color)

    from src.services.GameDetector import is_trash_path, is_project_repo_root

    if is_trash_path(input_dir):
        log(_i18n.tr('error_folder_in_trash', path=input_dir), 'red')
        return False

    if is_project_repo_root(input_dir):
        log(_i18n.tr('error_folder_is_repo_root', path=input_dir), 'red')
        return False

    try:
        if backup:
            backup_dir = input_dir + "-" + lang_source
            if os.path.exists(backup_dir) and os.listdir(backup_dir):
                log(_i18n.tr('log_backup_exists', path=backup_dir), 'yellow')
            else:
                log(_i18n.tr('log_creating_backup', path=backup_dir), 'yellow')
                copy_files_only(input_dir, backup_dir)

        copy_files_only(input_dir, extractor.folderInput)

        log(_i18n.tr('log_processing_files'), 'green')
        extractor.process_files()

        if not gui_signals:
            cli.show_status(extractor)
        else:
            # No modo CLI o show_status já bloqueia até o pool terminar; na GUI
            # precisamos aguardar explicitamente antes de pedir a verificação manual.
            import concurrent.futures
            concurrent.futures.wait(extractor.futures)

        if verify:
            if gui_verify_callback:
                log(_i18n.tr('log_waiting_verification', path=extractor.folderProcess), 'yellow')
                if not gui_verify_callback(extractor.folderProcess):
                    log(_i18n.tr('log_verification_aborted'), 'red')
                    return False
            elif gui_signals:
                log(_i18n.tr('log_gui_verification_notice', path=extractor.folderProcess), 'yellow')
            else:
                cli.instruction(lambda: _i18n.tr('instruction_verify_folder', path=extractor.folderProcess))

        log(_i18n.tr('log_exporting_files'), 'green')
        extractor.import_files()
        # clear_destination=False: um arquivo que falhou o processamento não
        # existe em folderOutput, e não pode ser apagado de input_dir sem
        # substituto (perderia o original sem gerar tradução nenhuma).
        copy_files_only(extractor.folderOutput, input_dir, clear_destination=False)

        log(_i18n.tr('log_finished_success'), 'green')

        return True

    except Exception as e:
        log(_i18n.tr('log_processing_error', error=str(e)), 'red')
        return False

    finally:
        # Persiste o cache de traduções mesmo se o processamento falhar ou for
        # interrompido (Ctrl+C incluso, já que finally roda também nesse caso),
        # para não perder o trabalho já feito na sessão.
        try:
            translate.save_cache()
        except Exception as cache_error:
            log(_i18n.tr('log_cache_save_warning', error=cache_error), 'yellow')


if __name__ == '__main__':
    setup_logging()
    args = parse_arguments()

    # Opção para iniciar GUI
    if args.gui:
        try:
            from src.gui import run_gui
            run_gui(args, lang_options, run_extraction_process)
            sys.exit(0)
        except ImportError as e:
            cli.print_colored_line(_i18n.tr('error_gui_unavailable'), 'red')
            cli.print_colored_line(_i18n.tr('log_details', error=e), 'yellow')
            cli.print_colored_line(_i18n.tr('log_starting_terminal_mode'), 'cyan')
            time.sleep(3)

    # Opções de listagem
    if args.list_extractors or args.list_translators or args.list_languages:
        list_options()
        sys.exit(0)

    # Validar argumentos se fornecidos via CLI
    if any([args.extractor, args.translator, args.source, args.target, args.input]):
        errors = validate_arguments(args)
        if errors:
            cli.print_colored_line(_i18n.tr('error_cli_arguments'), 'red')
            for error in errors:
                cli.print_colored_line(f"  - {error}", 'red')
            cli.print_colored_line(_i18n.tr('log_starting_interactive_mode'), 'yellow')
            time.sleep(2)
            # Limpar argumentos inválidos para forçar prompt
            extractors = ExtractorFactory.get_available()
            translators = TranslatorFactory.get_available()
            if args.extractor not in extractors: args.extractor = None
            if args.translator not in translators: args.translator = None
            if args.source not in lang_options.values(): args.source = None
            if args.target not in lang_options.values(): args.target = None
            if args.input and not os.path.exists(args.input): args.input = None

    # Iniciar workflow unificado
    try:
        success = run_workflow(args)
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print(_i18n.tr('log_operation_cancelled'))
        sys.exit(0)
    except Exception as e:
        cli.print_colored_line(_i18n.tr('error_fatal', error=e), 'red')
        import traceback
        traceback.print_exc()
        sys.exit(1)

import os
import logging
from dataclasses import dataclass, field
from typing import Optional, Dict, Any

from src.factory import ExtractorFactory, TranslatorFactory
from src.workspace import Workspace
from src.services.FileNameTranslator import FileNameTranslator


@dataclass
class JobConfig:
    mode: str = 'content'  # 'content' ou 'filenames'
    extractor_name: Optional[str] = None
    translator_name: str = 'Google'
    lang_source: str = 'auto'
    lang_target: str = 'pt'
    extractor_config: Dict[str, Any] = field(default_factory=dict)
    translator_config: Dict[str, Any] = field(default_factory=dict)
    synopsis: Optional[str] = None
    backup: bool = True
    verify: bool = True
    keep_original: bool = False
    input_dir: Optional[str] = None
    output_dir: Optional[str] = None
    is_interactive: bool = False

    def validate(self):
        if self.mode not in ('content', 'filenames'):
            raise ValueError(f"Modo inválido: '{self.mode}'. Escolha 'content' ou 'filenames'.")
        if self.mode == 'content' and not self.extractor_name:
            raise ValueError("extractor_name é obrigatório para o modo 'content'.")
        if not self.translator_name:
            raise ValueError("translator_name é obrigatório.")
        if not self.lang_target:
            raise ValueError("lang_target é obrigatório.")
        if self.lang_source != 'auto' and self.lang_source == self.lang_target:
            raise ValueError("Idioma de origem e destino não podem ser iguais.")
        return True


class TranslationJob:
    """
    Orquestrador que centraliza a instanciação, configuração e execução de um job de tradução.
    Suporta tradução de conteúdo de jogos (via Extractor) e nomes de arquivos (FileNameTranslator).
    """

    def __init__(self, job_config: JobConfig, workspace: Optional[Workspace] = None,
                 extractor=None, translator=None):
        self.config = job_config
        self.config.validate()

        if workspace is not None:
            self.workspace = workspace
        elif self.config.input_dir:
            self.workspace = Workspace(self.config.input_dir, output_dir=self.config.output_dir)
        else:
            self.workspace = None

        self.observers = []
        self.extractor = None
        self.translator = None
        self.fn_translator = None

        self._init_components(extractor=extractor, translator=translator)

    def _init_components(self, extractor=None, translator=None):
        # 1. Instancia tradutor
        if translator is not None:
            self.translator = translator
        else:
            self.translator = TranslatorFactory.create(self.config.translator_name)
        if hasattr(self.translator, 'change_language'):
            self.translator.change_language(self.config.lang_source, self.config.lang_target)

        # Sinopse
        if self.config.synopsis:
            if hasattr(self.translator, 'set_game_synopsis'):
                self.translator.set_game_synopsis(self.config.synopsis)
            elif hasattr(self.translator, 'apply_configuration'):
                self.translator.apply_configuration({'synopsis': self.config.synopsis})

        # Configurações dinâmicas do tradutor
        if self.config.translator_config and hasattr(self.translator, 'apply_configuration'):
            self.translator.apply_configuration(self.config.translator_config)

        # 2. Modo de operação
        if self.config.mode == 'content':
            if extractor is not None:
                self.extractor = extractor
            else:
                self.extractor = ExtractorFactory.create(
                    self.config.extractor_name,
                    self.translator,
                    workspace=self.workspace
                )
            if self.workspace and hasattr(self.extractor, 'workspace'):
                self.extractor.workspace = self.workspace

            if self.config.extractor_config and hasattr(self.extractor, 'apply_configuration'):
                self.extractor.apply_configuration(self.config.extractor_config)

            if hasattr(self.extractor, 'add_observer'):
                obs_list = getattr(self.extractor, 'observers', None)
                if obs_list is None or self._forward_extractor_event not in obs_list:
                    self.extractor.add_observer(self._forward_extractor_event)
        else:
            self.fn_translator = FileNameTranslator(self.translator)

    def add_observer(self, callback):
        self.observers.append(callback)

    def notify_observers(self, event_name, data):
        for obs in self.observers:
            try:
                obs(event_name, data)
            except Exception as e:
                logging.error(f"Erro no observer do job: {e}")

    def _forward_extractor_event(self, event_name, data):
        self.notify_observers(event_name, data)

    def create_backup_if_needed(self):
        if not self.config.backup or not self.workspace:
            return None
        from main import ensure_backup
        backup_dir = f"{self.workspace.input_dir}-{self.config.lang_source}"
        exts = getattr(self.extractor, 'files_types', []) if self.extractor else None
        newly_backed_up = ensure_backup(self.workspace.input_dir, backup_dir, extensions=exts)
        return backup_dir, bool(newly_backed_up)

    def run(self, gui_signals=None, gui_verify_callback=None, is_interactive=None):
        if is_interactive is None:
            is_interactive = self.config.is_interactive

        def log(msg, color='white'):
            logging.info(msg)
            if gui_signals:
                gui_signals.log.emit(msg, color)
            else:
                from src import cli
                cli.print_colored_line(msg, color)

        if not self.workspace:
            raise ValueError("Workspace não foi configurado para a execução do Job.")

        from src.services.GameDetector import is_trash_path, is_project_repo_root
        if is_trash_path(self.workspace.input_dir):
            log(f"Erro: Pasta '{self.workspace.input_dir}' está na lixeira.", 'red')
            return False
        if is_project_repo_root(self.workspace.input_dir):
            log(f"Erro: Pasta '{self.workspace.input_dir}' é a raiz do repositório.", 'red')
            return False

        try:
            if self.config.backup:
                res = self.create_backup_if_needed()
                if res:
                    from src.cli import _i18n
                    backup_dir, newly = res
                    if newly:
                        log(_i18n.tr('log_creating_backup', path=backup_dir), 'yellow')
                    else:
                        log(_i18n.tr('log_backup_exists', path=backup_dir), 'yellow')

            if self.config.mode == 'content':
                return self._run_content_mode(log, gui_signals, gui_verify_callback, is_interactive)
            elif self.config.mode == 'filenames':
                return self._run_filenames_mode(log)
            else:
                raise ValueError(f"Modo desconhecido: {self.config.mode}")
        except Exception as e:
            log(f"Erro na execução da tradução: {e}", 'red')
            logging.exception(e)
            return False
        finally:
            try:
                if self.translator and hasattr(self.translator, 'save_cache'):
                    self.translator.save_cache()
            except Exception as e:
                logging.warning(f"Erro ao salvar cache de tradução: {e}")

            if self.workspace:
                self.workspace.cleanup()

    def _run_content_mode(self, log, gui_signals, gui_verify_callback, is_interactive):
        import sys
        import concurrent.futures
        from src import cli
        from src.cli import _i18n

        if hasattr(self.extractor, 'init_folder'):
            self.extractor.init_folder()

        log(_i18n.tr('log_processing_files'), 'green')

        if not gui_signals:
            if is_interactive and sys.stdin.isatty():
                self.extractor.process_files()
                cli.show_status(self.extractor)
            else:
                logged_done = set()

                def _on_progress(event_name, data):
                    if event_name == 'status_update':
                        items = (
                            data if isinstance(data, list)
                            else ([data] if isinstance(data, dict) else [])
                        )
                        for item in items:
                            f = item.get('file')
                            st = item.get('status')
                            if st in ('success', 'erro', 'ignore', 'partial') and f not in logged_done:
                                logged_done.add(f)
                                msg = item.get('msg', '')
                                color = (
                                    'green' if st == 'success'
                                    else ('yellow' if st in ('ignore', 'partial') else 'red')
                                )
                                cli.print_colored_line(f"[{st.upper()}] {f} - {msg}", color)

                if hasattr(self.extractor, 'add_observer'):
                    self.extractor.add_observer(_on_progress)
                self.extractor.process_files()
                if hasattr(self.extractor, 'wait_for_dispatch'):
                    self.extractor.wait_for_dispatch()
                all_fut = (
                    list(getattr(self.extractor, 'futures', []))
                    + list(getattr(self.extractor, 'fast_futures', []))
                )
                if all_fut:
                    concurrent.futures.wait(all_fut)
        else:
            self.extractor.process_files()
            if hasattr(self.extractor, 'wait_for_dispatch'):
                self.extractor.wait_for_dispatch()
            all_fut = (
                list(getattr(self.extractor, 'futures', []))
                + list(getattr(self.extractor, 'fast_futures', []))
            )
            if all_fut:
                concurrent.futures.wait(all_fut)

        if self.config.verify:
            verify_folder = self.extractor.folderProcess
            if gui_verify_callback:
                log(_i18n.tr('log_waiting_verification', path=verify_folder), 'yellow')
                if not gui_verify_callback(verify_folder):
                    log(_i18n.tr('log_verification_aborted'), 'red')
                    return False
            elif gui_signals:
                log(_i18n.tr('log_gui_verification_notice', path=verify_folder), 'yellow')
            else:
                cli.instruction(lambda: _i18n.tr('instruction_verify_folder', path=verify_folder))

        log(_i18n.tr('log_exporting_files'), 'green')
        self.extractor.import_files()

        if os.path.abspath(self.extractor.folderOutput) != os.path.abspath(self.workspace.output_dir):
            from main import sync_folder_tree
            sync_folder_tree(self.extractor.folderOutput, self.workspace.output_dir)

        # Resumo final de arquivos abaixo de 100% / parciais
        threads_status = getattr(self.extractor, 'threads_status', [])
        partial_files = [
            s for s in threads_status
            if s.get('status') == 'partial'
        ]
        if partial_files:
            log("--- Resumo de Arquivos com Tradução Parcial (<100%) ---", 'yellow')
            for pf in partial_files:
                f_name = pf.get('file', '')
                f_msg = pf.get('msg', '')
                log(f"  • {f_name}: {f_msg}", 'yellow')

        log(_i18n.tr('log_finished_success'), 'green')
        return True

    def _run_filenames_mode(self, log):
        self.fn_translator.folderOutput = self.workspace.output_dir
        self.fn_translator.process_directory(
            self.workspace.input_dir,
            keep_original=self.config.keep_original
        )
        return True

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.workspace:
            self.workspace.cleanup()

import os

from rich.console import Console
from rich.text import Text

from textual.app import App, ComposeResult, SystemCommand
from textual.binding import Binding
from textual.widgets import DataTable, DirectoryTree, Footer, Input, OptionList, Static, Tree
from textual.widgets.option_list import Option

from src.i18n import LANGUAGES, Translator
from src.services.SettingsStore import SettingsStore
from src.utils.ClipboardUtils import get_clipboard_folder, get_clipboard_text

STYLE_MAP = {
    'reset': 'default', 'red': 'red', 'green': 'green', 'yellow': 'yellow',
    'blue': 'blue', 'magenta': 'magenta', 'cyan': 'cyan', 'white': 'white',
}

_console = Console()
_settings = SettingsStore()
_i18n = Translator(_settings.get('ui_language'))


def colored_string(text, color='white'):
    return Text(text, style=STYLE_MAP.get(color, 'white'))


def print_colored_line(text, color='white'):
    _console.print(colored_string(text, color))


def clear_screen():
    _console.clear()


def status_color(status):
    return {
        'success': 'green', 'erro': 'red', 'danger': 'red',
        'waiting': 'yellow', 'process': 'cyan',
    }.get(status, 'white')


class _LanguagePaletteMixin:
    """Expõe comandos de troca de idioma no Command Palette (ctrl+p) nativo do Textual."""

    def get_system_commands(self, screen):
        yield from super().get_system_commands(screen)
        for code, name in LANGUAGES.items():
            if code != _i18n.language:
                yield SystemCommand(
                    f"{_i18n.tr('label_ui_language')} {name}", name,
                    lambda code=code: self._change_ui_language(code),
                )

    def _change_ui_language(self, code):
        _i18n.set_language(code)
        _settings.set('ui_language', code)
        self._apply_language()

    def _apply_language(self):
        """Sobrescrito nas subclasses para atualizar os textos já renderizados na tela atual."""


class _SelectOptionApp(_LanguagePaletteMixin, App):
    BINDINGS = [Binding("escape", "cancel", _i18n.tr('cli_binding_quit'))]
    CSS = "OptionList { height: 1fr; } #prompt-title { padding: 1 2; }"

    def __init__(self, title, options, index=0):
        super().__init__()
        self._title_source = title
        self._options_source = options
        self.initial_index = index
        self.keys, self.values = self._resolve_options()

    def _resolve_options(self):
        options = self._options_source() if callable(self._options_source) else self._options_source
        if isinstance(options, dict):
            return list(options.keys()), options
        return list(options), None

    def _resolve_title(self):
        return self._title_source() if callable(self._title_source) else self._title_source

    def compose(self) -> ComposeResult:
        yield Static(self._resolve_title(), id="prompt-title")
        yield OptionList(*[Option(str(k)) for k in self.keys])
        yield Footer()

    def on_mount(self) -> None:
        option_list = self.query_one(OptionList)
        if 0 <= self.initial_index < len(self.keys):
            option_list.highlighted = self.initial_index
        option_list.focus()

    def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        selected_key = self.keys[event.option_index]
        self.exit(self.values[selected_key] if self.values is not None else selected_key)

    def action_cancel(self) -> None:
        self.exit("Exit")

    def _apply_language(self):
        option_list = self.query_one(OptionList)
        highlighted = option_list.highlighted
        self.keys, self.values = self._resolve_options()
        self.query_one("#prompt-title", Static).update(self._resolve_title())
        option_list.clear_options()
        option_list.add_options([Option(str(k)) for k in self.keys])
        if highlighted is not None and highlighted < len(self.keys):
            option_list.highlighted = highlighted
        self.bind("escape", "cancel", description=_i18n.tr('cli_binding_quit'))
        self.refresh_bindings()


def select_option(title, options, index=0):
    """Aceita título/opções fixos ou callables sem argumento (retraduzidos ao trocar
    o idioma pela paleta de comandos). ``options`` pode ser um dict (mostra as
    chaves, retorna o valor) ou uma lista (retorna o item)."""
    initial_options = options() if callable(options) else options
    keys = list(initial_options.keys()) if isinstance(initial_options, dict) else list(initial_options)

    if not keys:
        return "Exit"

    result = _SelectOptionApp(title, options, index).run()
    if result is None:
        return "Exit"
    return result


class _InstructionApp(_LanguagePaletteMixin, App):
    BINDINGS = [Binding("enter", "confirm", _i18n.tr('cli_binding_continue'))]
    CSS = "#instruction-text { padding: 1 2; }"

    def __init__(self, description, color):
        super().__init__()
        self._description_source = description
        self.color = color

    def _resolve_description(self):
        return self._description_source() if callable(self._description_source) else self._description_source

    def compose(self) -> ComposeResult:
        yield Static(colored_string(self._resolve_description(), self.color), id="instruction-text")
        yield Footer()

    def action_confirm(self) -> None:
        self.exit()

    def _apply_language(self):
        self.query_one("#instruction-text", Static).update(colored_string(self._resolve_description(), self.color))
        self.bind("enter", "confirm", description=_i18n.tr('cli_binding_continue'))
        self.refresh_bindings()


def instruction(description, color='green'):
    """``description`` aceita um valor fixo ou um callable sem argumento (retraduzido
    ao trocar o idioma pela paleta de comandos)."""
    _InstructionApp(description, color).run()


class _SelectFolderApp(_LanguagePaletteMixin, App):
    BINDINGS = [
        Binding("escape", "cancel", _i18n.tr('cli_binding_cancel')),
        Binding("s", "confirm_tree", _i18n.tr('cli_binding_select_folder')),
        Binding("ctrl+v", "paste_clipboard", _i18n.tr('cli_binding_paste')),
    ]
    CSS = """
    #folder-title { padding: 1 2 0 2; }
    #folder-input { margin: 1 2 0 2; }
    #error-label { color: red; margin: 0 2; height: 1; }
    DirectoryTree { height: 1fr; margin: 0 2 1 2; }
    """

    def __init__(self, title, start_path, initial_input=""):
        super().__init__()
        self._title_source = title
        self.start_path = os.path.abspath(start_path) if start_path else os.getcwd()
        self.initial_input = initial_input or (self.start_path if os.path.isdir(self.start_path) else "")

    def _resolve_title(self):
        return self._title_source() if callable(self._title_source) else self._title_source

    def compose(self) -> ComposeResult:
        yield Static(self._resolve_title(), id="folder-title")
        yield Input(
            value=self.initial_input,
            placeholder=_i18n.tr('cli_placeholder_folder_input'),
            id="folder-input"
        )
        yield Static("", id="error-label")
        yield DirectoryTree(self.start_path)
        yield Footer()

    def _get_node_path(self, node) -> Optional[str]:
        if node is None:
            return None
        if hasattr(node, "data") and node.data is not None:
            if hasattr(node.data, "path"):
                return str(node.data.path)
        try:
            tree = self.query_one(DirectoryTree)
            if node == tree.root:
                return self.start_path
        except Exception:
            pass
        return None

    def _submit_path(self, candidate_path: str) -> None:
        if not candidate_path:
            return
        from src.services.GameDetector import is_trash_path
        expanded = os.path.abspath(os.path.expanduser(candidate_path))
        if is_trash_path(expanded):
            self.query_one("#error-label", Static).update(
                _i18n.tr('error_folder_in_trash', path=expanded)
            )
            return
        if os.path.isdir(expanded):
            self.exit(expanded)
        else:
            self.query_one("#error-label", Static).update(
                _i18n.tr('error_folder_not_found', path=candidate_path)
            )

    def on_tree_node_highlighted(self, event: Tree.NodeHighlighted) -> None:
        """Sincroniza o caminho da pasta em foco com o campo de texto apenas quando o usuário está navegando na árvore."""
        try:
            tree = self.query_one(DirectoryTree)
            if not tree.has_focus:
                return
        except Exception:
            return

        node = event.node
        if node is not None:
            node_path = self._get_node_path(node)
            if node_path and os.path.isdir(node_path):
                inp = self.query_one("#folder-input", Input)
                inp.value = node_path

    def on_input_submitted(self, event: Input.Submitted) -> None:
        raw = event.value.strip().strip("'\"")
        if not raw:
            self._submit_path(self.start_path)
            return
        if raw.startswith("file://"):
            raw = raw[7:]
        self._submit_path(raw)

    def on_directory_tree_directory_selected(self, event: DirectoryTree.DirectorySelected) -> None:
        if event.path and event.path.is_dir():
            self._submit_path(str(event.path))

    def action_confirm_tree(self) -> None:
        tree = self.query_one(DirectoryTree)
        node = tree.cursor_node
        node_path = self._get_node_path(node) if node is not None else None
        if node_path and os.path.isdir(node_path):
            self._submit_path(node_path)
        else:
            inp = self.query_one("#folder-input", Input)
            raw = inp.value.strip().strip("'\"")
            self._submit_path(raw or self.start_path)

    def action_cancel(self) -> None:
        self.exit(None)

    def action_paste_clipboard(self) -> None:
        clip = get_clipboard_text()
        if clip:
            inp = self.query_one("#folder-input", Input)
            inp.value = clip
            inp.focus()

    def _apply_language(self):
        self.query_one("#folder-title", Static).update(self._resolve_title())
        self.query_one("#folder-input", Input).placeholder = _i18n.tr('cli_placeholder_folder_input')
        self.bind("escape", "cancel", description=_i18n.tr('cli_binding_cancel'))
        self.bind("s", "confirm_tree", description=_i18n.tr('cli_binding_select_folder'))
        self.bind("ctrl+v", "paste_clipboard", description=_i18n.tr('cli_binding_paste'))
        self.refresh_bindings()


def select_folder(title=None):
    """
    Permite selecionar uma pasta no modo CLI:
    1. Verifica se uma pasta de jogo válida foi detectada no local de invocação.
    2. Verifica se a pasta atual onde o comando foi chamado é elegível.
    3. Verifica se há um caminho de diretório válido na área de transferência (clipboard).
    4. Permite digitação manual ou navegação interativa na árvore.
    """
    from src.services.GameDetector import detect_game_environment, is_project_repo_root, is_trash_path

    caller_dir = os.environ.get('CALLER_WORKING_DIR') or os.getcwd()
    detected = detect_game_environment(caller_dir)
    clipboard_folder = get_clipboard_folder()

    options_map = {}
    detected_path_val = None
    caller_path_val = None
    
    if detected:
        desc = detected.get('description', 'Jogo')
        detected_path_val = detected['detected_path']
        options_map[f"🎮 {_i18n.tr('cli_opt_use_detected_folder', path=detected_path_val)} ({desc})"] = "detected"

    if caller_dir and os.path.isdir(caller_dir) and not is_project_repo_root(caller_dir) and not is_trash_path(caller_dir):
        if not detected or detected['detected_path'] != os.path.abspath(caller_dir):
            caller_path_val = caller_dir
            options_map[f"📂 {_i18n.tr('cli_opt_use_current_folder', path=caller_dir)}"] = "current"

    if clipboard_folder and (not detected or detected['detected_path'] != clipboard_folder):
        options_map[f"📋 {_i18n.tr('cli_opt_use_clipboard', path=clipboard_folder)}"] = "clipboard"

    if options_map:
        options_map[f"⌨️  {_i18n.tr('cli_opt_type_path')}"] = "manual"
        options_map[f"📁 {_i18n.tr('cli_opt_browse_tree')}"] = "tree"
        options_map[f"❌ {_i18n.tr('cli_binding_cancel')}"] = "cancel"

        def folder_options():
            return options_map

        prompt_title = lambda: _i18n.tr('cli_detected_folder_prompt', path=detected['detected_path'], desc=detected.get('description', '')) if detected else (title or (lambda: _i18n.tr('cli_default_select_folder_title')))()

        choice = select_option(
            prompt_title,
            folder_options
        )

        if choice == "detected":
            return detected_path_val
        elif choice == "current":
            return caller_path_val
        elif choice == "clipboard":
            return clipboard_folder
        elif choice in ("cancel", "Exit"):
            return None
        elif choice == "manual":
            clear_screen()
            print_colored_line(_i18n.tr('prompt_type_folder_path'), 'cyan')
            while True:
                path = input("> ").strip().strip("'\"")
                if not path:
                    return None
                if path.startswith("file://"):
                    path = path[7:]
                expanded = os.path.abspath(os.path.expanduser(path))
                if os.path.isdir(expanded):
                    return expanded
                print_colored_line(_i18n.tr('error_folder_not_found', path=path), 'red')
                print_colored_line(_i18n.tr('prompt_try_again_or_empty'), 'yellow')
        elif choice == "tree":
            initial_dir = detected['detected_path'] if detected else (caller_dir if os.path.isdir(caller_dir) else os.getcwd())
            return _SelectFolderApp(
                title or (lambda: _i18n.tr('cli_default_select_folder_title')),
                initial_dir,
                initial_input=initial_dir
            ).run()

    initial_dir = caller_dir if caller_dir and os.path.isdir(caller_dir) and not is_project_repo_root(caller_dir) else os.getcwd()
    return _SelectFolderApp(
        title or (lambda: _i18n.tr('cli_default_select_folder_title')),
        initial_dir,
        initial_input=initial_dir
    ).run()


class _StatusApp(_LanguagePaletteMixin, App):
    def __init__(self, extractor):
        super().__init__()
        self.extractor = extractor

    def compose(self) -> ComposeResult:
        table = DataTable()
        self._add_columns(table)
        yield table
        yield Footer()

    def _add_columns(self, table):
        table.add_columns(
            _i18n.tr('cli_table_file'), _i18n.tr('cli_table_status'),
            _i18n.tr('cli_table_message'), _i18n.tr('cli_table_progress'),
        )

    def on_mount(self) -> None:
        self.extractor.add_observer(self._on_extractor_event)
        self._refresh(self.extractor.threads_status)

    def _on_extractor_event(self, event_name, data) -> None:
        if event_name == 'status_update':
            self.call_from_thread(self._refresh, data)

    def _refresh(self, status_list) -> None:
        table = self.query_one(DataTable)
        table.clear()
        all_done = True

        # Coloca arquivos em processamento ('process') no topo da listagem
        status_priority = {
            'process': 0,
            'waiting': 1,
            'erro': 2,
            'danger': 2,
            'success': 3,
            'ignore': 4,
        }
        sorted_status_list = sorted(
            status_list,
            key=lambda s: (status_priority.get(s.get('status', ''), 99), s.get('file', ''))
        )

        for status in sorted_status_list:
            s_code = status.get('status', '')
            file_name = status.get('file', '')
            msg = status.get('msg', '')
            progress = ''
            if 'current' in status and 'total' in status:
                progress = f"{status['current']}/{status['total']}"
            table.add_row(file_name, Text(s_code, style=status_color(s_code)), msg, progress)
            if s_code not in ('erro', 'success', 'ignore'):
                all_done = False

        if status_list and all_done:
            self.exit()

    def _apply_language(self):
        table = self.query_one(DataTable)
        status_list = list(self.extractor.threads_status)
        table.clear(columns=True)
        self._add_columns(table)
        self._refresh(status_list)


def show_status(extractor):
    _StatusApp(extractor).run()

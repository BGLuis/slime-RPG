import sys
import os
import time

import PyQt5.QtCore
import PyQt5.QtGui
import PyQt5.QtWidgets
import PyQt5.QtSvg

from PyQt5.QtWidgets import (
    QApplication, QMainWindow, QWidget, QVBoxLayout, QHBoxLayout,
    QLabel, QComboBox, QPushButton, QFileDialog, QLineEdit,
    QTextEdit, QCheckBox, QProgressBar, QSplitter,
    QGroupBox, QDialog, QDialogButtonBox
)
from PyQt5.QtCore import Qt, pyqtSignal, QObject, QThread, QSize, QEventLoop, QUrl, QDir
from PyQt5.QtGui import QFont, QDesktopServices

import qtawesome as qta
import qt_material
from qt_material import apply_stylesheet
from qt_material.resources import RESOURCES_PATH

# Garante compatibilidade do qt_material com PyQt5 e resolução de ícones SVG
qt_material.QFontDatabase = PyQt5.QtGui.QFontDatabase
qt_material.QGuiApplication = PyQt5.QtGui.QGuiApplication
qt_material.QPalette = PyQt5.QtGui.QPalette
qt_material.QColor = PyQt5.QtGui.QColor
qt_material.QDir = PyQt5.QtCore.QDir
qt_material.GUI = True

from src.factory import ExtractorFactory, TranslatorFactory  # noqa: E402
from src.i18n import Translator, LANGUAGES  # noqa: E402
from src.services.SettingsStore import SettingsStore  # noqa: E402


class VerificationDialog(QDialog):
    """Dialog for manual verification of files"""
    def __init__(self, folder_path, i18n, parent=None):
        super().__init__(parent)
        self.folder_path = folder_path
        self.tr_ = i18n.tr
        self.init_ui()

    def init_ui(self):
        self.setWindowTitle(self.tr_('window_title_verification'))
        self.resize(500, 300)
        layout = QVBoxLayout(self)

        icon_label = QLabel()
        icon_label.setPixmap(qta.icon('fa5s.search', color='#4fc1ff').pixmap(QSize(64, 64)))
        icon_label.setAlignment(Qt.AlignCenter)
        layout.addWidget(icon_label)

        text = QLabel(self.tr_('verification_text', folder=self.folder_path))
        text.setWordWrap(True)
        text.setAlignment(Qt.AlignCenter)
        layout.addWidget(text)

        btn_open = QPushButton(self.tr_('btn_open_folder'))
        btn_open.setIcon(qta.icon('fa5s.external-link-alt'))
        btn_open.clicked.connect(self.open_folder)
        layout.addWidget(btn_open)

        buttons = QDialogButtonBox(QDialogButtonBox.Ok | QDialogButtonBox.Cancel)
        buttons.button(QDialogButtonBox.Ok).setText(self.tr_('btn_continue'))
        buttons.button(QDialogButtonBox.Cancel).setText(self.tr_('btn_abort'))
        buttons.accepted.connect(self.accept)
        buttons.rejected.connect(self.reject)
        layout.addWidget(buttons)

    def open_folder(self):
        QDesktopServices.openUrl(QUrl.fromLocalFile(os.path.abspath(self.folder_path)))


class WorkerSignals(QObject):
    """Signals for the translation worker thread"""
    finished = pyqtSignal(bool)
    log = pyqtSignal(str, str)  # message, color
    status_update = pyqtSignal(object)  # threads_status (delta dict or list)
    request_verification = pyqtSignal(str)  # folder_path


class TranslationWorker(QThread):
    """Thread to handle the translation process without freezing the UI"""
    verification_response = pyqtSignal(bool)

    def __init__(self, job_or_func=None, *args, **kwargs):
        super().__init__()
        from src.job import TranslationJob
        if isinstance(job_or_func, TranslationJob):
            self.job = job_or_func
            self.run_process_func = kwargs.get('run_process_func')
            self.extractor = getattr(job_or_func, 'extractor', None)
            self.translate = getattr(job_or_func, 'translator', None)
            self.input_dir = getattr(getattr(job_or_func, 'workspace', None), 'input_dir', None)
            self.lang_source = getattr(getattr(job_or_func, 'config', None), 'lang_source', None)
            self.backup = getattr(getattr(job_or_func, 'config', None), 'backup', True)
            self.verify = getattr(getattr(job_or_func, 'config', None), 'verify', True)
        elif callable(job_or_func):
            self.job = kwargs.get('job')
            self.run_process_func = job_or_func
            self.extractor = args[0] if len(args) > 0 else kwargs.get('extractor')
            self.translate = args[1] if len(args) > 1 else kwargs.get('translate')
            self.input_dir = args[2] if len(args) > 2 else kwargs.get('input_dir')
            self.lang_source = args[3] if len(args) > 3 else kwargs.get('lang_source')
            self.backup = args[4] if len(args) > 4 else kwargs.get('backup', True)
            self.verify = args[5] if len(args) > 5 else kwargs.get('verify', True)
        else:
            self.job = kwargs.get('job')
            self.run_process_func = kwargs.get('run_process_func')
            self.extractor = kwargs.get('extractor')
            self.translate = kwargs.get('translate')
            self.input_dir = kwargs.get('input_dir')
            self.lang_source = kwargs.get('lang_source')
            self.backup = kwargs.get('backup', True)
            self.verify = kwargs.get('verify', True)

        self.signals = WorkerSignals()
        self._wait_loop = None

        if self.job is not None and hasattr(self.job, 'add_observer'):
            self.job.add_observer(self._on_worker_event)
        elif self.extractor is not None and hasattr(self.extractor, 'add_observer'):
            self.extractor.add_observer(self._on_worker_event)

    def _on_worker_event(self, event_name, data):
        if event_name == 'status_update':
            self.signals.status_update.emit(data)

    def run(self):
        try:
            def gui_verify_callback(folder):
                self.signals.request_verification.emit(folder)
                self._wait_loop = QEventLoop()
                response = [False]

                def handle_response(res):
                    response[0] = res
                    self._wait_loop.quit()

                self.verification_response.connect(handle_response)
                self._wait_loop.exec_()
                return response[0]

            if self.job is not None:
                success = self.job.run(
                    gui_signals=self.signals,
                    gui_verify_callback=gui_verify_callback
                )
            elif self.run_process_func is not None:
                success = self.run_process_func(
                    self.extractor, self.translate, self.input_dir,
                    self.lang_source, self.backup, self.verify,
                    gui_signals=self.signals,
                    gui_verify_callback=gui_verify_callback
                )
            else:
                success = False

            self.signals.finished.emit(success)
        except Exception as e:
            self.signals.log.emit(f"Error: {str(e)}", "red")
            self.signals.finished.emit(False)


class TranslationGUI(QMainWindow):
    def __init__(self, args=None, lang_options=None, run_process_func=None):
        super().__init__()
        self.args = args
        self.lang_options = lang_options or {}
        self.run_process_func = run_process_func
        self.worker = None

        self.settings = SettingsStore()
        saved_ui_language = self.settings.get('ui_language')
        self.i18n = Translator(saved_ui_language)
        if not saved_ui_language:
            self.settings.set('ui_language', self.i18n.language)
        self.tr_ = self.i18n.tr
        self._files_status = {}

        self.init_ui()
        self.load_options()
        self.apply_args()

    def init_ui(self):
        self.setWindowTitle(self.tr_('window_title_main'))
        self.setWindowIcon(qta.icon('fa5s.language'))
        self.resize(1100, 800)

        # Main widget and layout
        central_widget = QWidget()
        self.setCentralWidget(central_widget)
        main_layout = QVBoxLayout(central_widget)
        main_layout.setContentsMargins(20, 20, 20, 20)
        main_layout.setSpacing(15)

        # Header
        header_layout = QHBoxLayout()
        title_icon = QLabel()
        title_icon.setPixmap(qta.icon('fa5s.robot', color='white').pixmap(QSize(48, 48)))
        header_layout.addWidget(title_icon)

        title_text = QLabel("Translator")
        title_text.setStyleSheet("font-size: 24px; font-weight: bold; color: white;")
        header_layout.addWidget(title_text)
        header_layout.addStretch()

        header_layout.addWidget(QLabel(self.tr_('label_ui_language')))
        self.ui_language_combo = QComboBox()
        for code, name in LANGUAGES.items():
            self.ui_language_combo.addItem(name, code)
        idx = self.ui_language_combo.findData(self.i18n.language)
        if idx >= 0:
            self.ui_language_combo.setCurrentIndex(idx)
        self.ui_language_combo.currentIndexChanged.connect(self.on_ui_language_changed)
        header_layout.addWidget(self.ui_language_combo)

        main_layout.addLayout(header_layout)

        # Splitter for settings and logs
        splitter = QSplitter(Qt.Vertical)

        # --- Settings Area ---
        settings_widget = QWidget()
        settings_layout = QVBoxLayout(settings_widget)
        settings_layout.setContentsMargins(0, 0, 0, 0)

        # Config Group
        config_group = QGroupBox(self.tr_('group_config'))
        config_layout = QVBoxLayout(config_group)

        # Extractor & Translator row
        row1 = QHBoxLayout()

        vbox_ext = QVBoxLayout()
        vbox_ext.addWidget(QLabel(self.tr_('label_extractor')))
        self.extractor_combo = QComboBox()
        vbox_ext.addWidget(self.extractor_combo)
        row1.addLayout(vbox_ext)

        vbox_trans = QVBoxLayout()
        vbox_trans.addWidget(QLabel(self.tr_('label_translator')))
        self.translator_combo = QComboBox()
        self.translator_combo.currentIndexChanged.connect(self.on_translator_changed)
        vbox_trans.addWidget(self.translator_combo)
        row1.addLayout(vbox_trans)

        config_layout.addLayout(row1)

        # Languages row
        row2 = QHBoxLayout()

        vbox_src = QVBoxLayout()
        vbox_src.addWidget(QLabel(self.tr_('label_source_lang')))
        self.source_lang_combo = QComboBox()
        vbox_src.addWidget(self.source_lang_combo)
        row2.addLayout(vbox_src)

        vbox_dst = QVBoxLayout()
        vbox_dst.addWidget(QLabel(self.tr_('label_target_lang')))
        self.target_lang_combo = QComboBox()
        vbox_dst.addWidget(self.target_lang_combo)
        row2.addLayout(vbox_dst)

        config_layout.addLayout(row2)

        # Directory row
        vbox_dir = QVBoxLayout()
        vbox_dir.addWidget(QLabel(self.tr_('label_game_folder')))
        dir_row = QHBoxLayout()
        self.dir_input = QLineEdit()
        self.dir_input.setPlaceholderText(self.tr_('placeholder_game_folder'))
        dir_row.addWidget(self.dir_input)

        self.btn_browse = QPushButton(self.tr_('btn_browse'))
        self.btn_browse.setIcon(qta.icon('fa5s.folder-open'))
        self.btn_browse.clicked.connect(self.browse_directory)
        dir_row.addWidget(self.btn_browse)
        vbox_dir.addLayout(dir_row)

        self.metadata_indicator = QLabel("")
        self.metadata_indicator.setStyleSheet(
            "color: #4ec9b0; font-size: 11px; font-weight: bold; padding: 2px 0px;"
        )
        self.metadata_indicator.setVisible(False)
        self.metadata_label = self.metadata_indicator
        vbox_dir.addWidget(self.metadata_indicator)

        config_layout.addLayout(vbox_dir)

        # Dynamic Questions Form Container
        self.dynamic_form_container = QWidget()
        self.dynamic_form_layout = QVBoxLayout(self.dynamic_form_container)
        self.dynamic_form_layout.setContentsMargins(0, 0, 0, 0)
        config_layout.addWidget(self.dynamic_form_container)
        self.dynamic_form_container.setVisible(False)
        self.dynamic_widgets = {}

        # Retrocompatibilidade com testes e chamadas diretas
        self.synopsis_container = self.dynamic_form_container

        # Options row
        row4 = QHBoxLayout()
        self.chk_backup = QCheckBox(self.tr_('chk_backup'))
        self.chk_backup.setChecked(True)
        self.chk_backup.setMinimumHeight(40)  # Ensure it's clickable
        row4.addWidget(self.chk_backup)

        self.chk_verify = QCheckBox(self.tr_('chk_verify'))
        self.chk_verify.setChecked(True)
        self.chk_verify.setMinimumHeight(40)  # Ensure it's clickable
        row4.addWidget(self.chk_verify)

        config_layout.addLayout(row4)

        settings_layout.addWidget(config_group)

        # Start Button
        self.btn_start = QPushButton(self.tr_('btn_start'))
        self.btn_start.setIcon(qta.icon('fa5s.play', color='white'))
        self.btn_start.setIconSize(QSize(20, 20))
        self.btn_start.setFixedHeight(55)
        self.btn_start.setCursor(Qt.PointingHandCursor)
        self.btn_start.clicked.connect(self.start_translation)
        settings_layout.addWidget(self.btn_start)

        # --- Logs Area ---
        log_widget = QWidget()
        log_layout = QVBoxLayout(log_widget)
        log_layout.setContentsMargins(0, 10, 0, 0)

        log_header = QHBoxLayout()
        log_header.addWidget(QLabel(self.tr_('label_log_output')))
        log_header.addStretch()
        self.btn_clear_log = QPushButton()
        self.btn_clear_log.setIcon(qta.icon('fa5s.trash-alt'))
        self.btn_clear_log.setToolTip(self.tr_('tooltip_clear_log'))
        self.btn_clear_log.setFlat(True)
        self.btn_clear_log.clicked.connect(lambda: self.log_output.clear())
        log_header.addWidget(self.btn_clear_log)
        log_layout.addLayout(log_header)

        self.log_output = QTextEdit()
        self.log_output.setReadOnly(True)
        self.log_output.setFont(QFont('Consolas', 10))
        log_layout.addWidget(self.log_output)

        self.progress_bar = QProgressBar()
        self.progress_bar.setVisible(False)
        self.progress_bar.setTextVisible(True)
        self.progress_bar.setAlignment(Qt.AlignCenter)
        log_layout.addWidget(self.progress_bar)

        splitter.addWidget(settings_widget)
        splitter.addWidget(log_widget)
        splitter.setStretchFactor(0, 1)
        splitter.setStretchFactor(1, 1)

        main_layout.addWidget(splitter)

        # Status Bar
        self.statusBar().showMessage(self.tr_('status_ready'))

    @property
    def synopsis_input(self):
        if 'synopsis' in self.dynamic_widgets:
            return self.dynamic_widgets['synopsis']['widget']
        if not hasattr(self, '_fallback_synopsis_input'):
            self._fallback_synopsis_input = QLineEdit()
        return self._fallback_synopsis_input

    def load_options(self):
        # Load Extractors
        extractors = ExtractorFactory.get_available()
        for name in sorted(extractors.keys()):
            self.extractor_combo.addItem(name, extractors[name])

        # Load Translators
        translators = TranslatorFactory.get_available()
        for name in sorted(translators.keys()):
            self.translator_combo.addItem(name, translators[name])

        # Load Languages
        for lang_name, lang_code in self.lang_options.items():
            self.source_lang_combo.addItem(lang_name, lang_code)
            self.target_lang_combo.addItem(lang_name, lang_code)

        # Set defaults
        idx = self.target_lang_combo.findData('pt')
        if idx >= 0:
            self.target_lang_combo.setCurrentIndex(idx)

        self.extractor_combo.currentIndexChanged.connect(self.on_extractor_changed)
        self.translator_combo.currentIndexChanged.connect(self.on_translator_changed)
        self.rebuild_dynamic_form()

    def on_translator_changed(self):
        self.rebuild_dynamic_form()

    def on_extractor_changed(self):
        self.rebuild_dynamic_form()

    def rebuild_dynamic_form(self):
        while self.dynamic_form_layout.count():
            item = self.dynamic_form_layout.takeAt(0)
            widget = item.widget()
            if widget:
                widget.deleteLater()
            elif item.layout():
                sub_layout = item.layout()
                while sub_layout.count():
                    sub_item = sub_layout.takeAt(0)
                    if sub_item.widget():
                        sub_item.widget().deleteLater()

        self.dynamic_widgets = {}

        extractor_class = self.extractor_combo.currentData()
        translator_class = self.translator_combo.currentData()

        questions_to_add = []
        if extractor_class and hasattr(extractor_class, 'get_interactive_questions'):
            try:
                for q in extractor_class.get_interactive_questions():
                    questions_to_add.append(('extractor', q))
            except Exception:
                pass

        if translator_class and hasattr(translator_class, 'get_interactive_questions'):
            try:
                for q in translator_class.get_interactive_questions():
                    questions_to_add.append(('translator', q))
            except Exception:
                pass

        for owner, q in questions_to_add:
            key = q.get('key') or q.get('id')
            if not key:
                continue

            field_box = QVBoxLayout()
            field_box.setSpacing(4)

            label_text = q.get('question') or q.get('title') or key
            lbl = QLabel(label_text)
            lbl.setWordWrap(True)
            if q.get('description'):
                lbl.setToolTip(q['description'])
            field_box.addWidget(lbl)

            options = q.get('options')
            is_secret = (
                q.get('secret', False)
                or q.get('password', False)
                or any(k in key.lower() for k in ('api_key', 'token', 'secret', 'password'))
            )

            if options:
                combo = QComboBox()
                if isinstance(options, dict):
                    for label, val in options.items():
                        combo.addItem(str(label), val)
                    default_val = q.get('default')
                    if default_val is not None:
                        idx = combo.findText(str(default_val))
                        if idx < 0:
                            idx = combo.findData(default_val)
                        if idx >= 0:
                            combo.setCurrentIndex(idx)
                elif isinstance(options, list):
                    for opt in options:
                        combo.addItem(str(opt), opt)
                    default_val = q.get('default')
                    if default_val is not None:
                        idx = combo.findText(str(default_val))
                        if idx >= 0:
                            combo.setCurrentIndex(idx)

                field_box.addWidget(combo)
                self.dynamic_widgets[key] = {
                    'widget': combo,
                    'type': 'combo',
                    'question': q,
                    'owner': owner
                }

            elif is_secret:
                line = QLineEdit()
                line.setEchoMode(QLineEdit.Password)
                if q.get('default'):
                    line.setText(str(q['default']))
                if q.get('description'):
                    line.setPlaceholderText(q['description'])
                field_box.addWidget(line)
                self.dynamic_widgets[key] = {
                    'widget': line,
                    'type': 'password',
                    'question': q,
                    'owner': owner
                }

            else:
                line = QLineEdit()
                if q.get('default'):
                    line.setText(str(q['default']))
                if q.get('description'):
                    line.setPlaceholderText(q['description'])
                field_box.addWidget(line)
                self.dynamic_widgets[key] = {
                    'widget': line,
                    'type': 'text',
                    'question': q,
                    'owner': owner
                }

            self.dynamic_form_layout.addLayout(field_box)

        self.dynamic_form_container.setVisible(bool(self.dynamic_widgets))

    def get_dynamic_field_value(self, key):
        info = self.dynamic_widgets.get(key)
        if not info:
            return None
        widget = info['widget']
        wtype = info['type']
        if wtype == 'combo':
            data = widget.currentData()
            return data if data is not None else widget.currentText()
        else:
            return widget.text().strip()

    def validate_dynamic_form(self):
        for key, info in self.dynamic_widgets.items():
            q = info['question']
            if q.get('required', False):
                val = self.get_dynamic_field_value(key)
                if val is None or (isinstance(val, str) and not val.strip()):
                    field_name = q.get('question', key)
                    msg = self.tr_('error_field_required', field=field_name).strip()
                    return False, msg
        return True, None

    def get_dynamic_config(self):
        extractor_config = {}
        translator_config = {}
        for key, info in self.dynamic_widgets.items():
            val = self.get_dynamic_field_value(key)
            if info['owner'] == 'extractor':
                extractor_config[key] = val
            else:
                translator_config[key] = val
        return {
            'extractor_config': extractor_config,
            'translator_config': translator_config
        }

    def apply_args(self):
        if not self.args:
            return
        if hasattr(self.args, 'extractor') and self.args.extractor:
            idx = self.extractor_combo.findText(self.args.extractor)
            if idx < 0:
                match = ExtractorFactory.find_match(self.args.extractor)
                if match:
                    idx = self.extractor_combo.findText(match)
            if idx >= 0:
                self.extractor_combo.setCurrentIndex(idx)
        if hasattr(self.args, 'translator') and self.args.translator:
            idx = self.translator_combo.findText(self.args.translator)
            if idx < 0:
                match = TranslatorFactory.find_match(self.args.translator)
                if match:
                    idx = self.translator_combo.findText(match)
            if idx >= 0:
                self.translator_combo.setCurrentIndex(idx)
        if hasattr(self.args, 'source') and self.args.source:
            idx = self.source_lang_combo.findData(self.args.source)
            if idx >= 0:
                self.source_lang_combo.setCurrentIndex(idx)
        if hasattr(self.args, 'target') and self.args.target:
            idx = self.target_lang_combo.findData(self.args.target)
            if idx >= 0:
                self.target_lang_combo.setCurrentIndex(idx)
        if hasattr(self.args, 'input') and self.args.input:
            abs_path = os.path.abspath(self.args.input)
            self.dir_input.setText(abs_path)
            self.auto_detect_game_type(abs_path)
        else:
            caller_dir = os.environ.get('CALLER_WORKING_DIR')
            if caller_dir and os.path.isdir(caller_dir):
                from src.services.GameDetector import detect_game_environment
                det = detect_game_environment(caller_dir)
                if det:
                    self.dir_input.setText(det['detected_path'])
                    self.auto_detect_game_type(det['detected_path'])
        if hasattr(self.args, 'synopsis') and self.args.synopsis:
            self.synopsis_input.setText(self.args.synopsis)
        if hasattr(self.args, 'no_backup') and self.args.no_backup:
            self.chk_backup.setChecked(False)
        if hasattr(self.args, 'no_verify') and self.args.no_verify:
            self.chk_verify.setChecked(False)

    def on_ui_language_changed(self):
        code = self.ui_language_combo.currentData()
        if not code:
            return
        self.settings.set('ui_language', code)
        temp_i18n = Translator(code)
        self.statusBar().showMessage(temp_i18n.tr('status_language_changed', language=LANGUAGES[code]))

    def browse_directory(self):
        dir_path = QFileDialog.getExistingDirectory(self, self.tr_('dialog_select_folder'))
        if dir_path:
            self.dir_input.setText(dir_path)
            self.auto_detect_game_type(dir_path)

    def auto_detect_game_type(self, dir_path):
        """Tenta adivinhar a pasta correta e o extrator com base na estrutura"""
        if not dir_path or not os.path.isdir(dir_path):
            if hasattr(self, 'metadata_indicator'):
                self.metadata_indicator.setText("")
                self.metadata_indicator.setVisible(False)
            return

        from src.services import GameMetadata
        from src.services.GameDetector import detect_game_environment

        detected = detect_game_environment(dir_path)
        detected_path = dir_path

        if detected:
            detected_path = detected['detected_path']
            detected_extractor = detected.get('extractor')

            # Atualizar a GUI se algo foi detectado
            if detected_path != dir_path:
                self.log(self.tr_('log_folder_adjusted', name=os.path.basename(detected_path)), "green")
                self.dir_input.setText(detected_path)

            if detected_extractor:
                idx = self.extractor_combo.findText(detected_extractor)
                if idx >= 0:
                    self.extractor_combo.setCurrentIndex(idx)
                    self.log(self.tr_('log_extractor_suggested', name=detected_extractor), "green")

        meta = GameMetadata.load_meta(detected_path) or GameMetadata.load_meta(dir_path)
        if meta:
            engine = meta.get('translator') or meta.get('extractor') or 'desconhecido'
            src = meta.get('lang_source') or 'auto'
            tgt = meta.get('lang_target') or '?'
            file_count = len(meta.get('files', {}))
            status_text = f"Metadado: Motor: {engine} | {src} -> {tgt} | {file_count} arquivos registrados"
            if hasattr(self, 'metadata_indicator'):
                self.metadata_indicator.setText(status_text)
                self.metadata_indicator.setVisible(True)
            self.log(f"Metadado encontrado: {engine} ({src}->{tgt}), {file_count} arquivos registrados", "cyan")
        else:
            if hasattr(self, 'metadata_indicator'):
                self.metadata_indicator.setText("")
                self.metadata_indicator.setVisible(False)

    def log(self, message, color="white"):
        timestamp = time.strftime("%H:%M:%S")
        color_hex = {
            "white": "#d4d4d4",
            "green": "#4ec9b0",
            "red": "#f44747",
            "yellow": "#dcdcaa",
            "cyan": "#4fc1ff"
        }.get(color, "#d4d4d4")

        self.log_output.append(f'<span style="color: #808080">[{timestamp}]</span> <span style="color: {color_hex}">{message}</span>')
        self.log_output.moveCursor(self.log_output.textCursor().End)

    def handle_verification(self, folder):
        dialog = VerificationDialog(folder, self.i18n, self)
        result = dialog.exec_()
        self.worker.verification_response.emit(result == QDialog.Accepted)

    def start_translation(self):
        input_dir = self.dir_input.text().strip()
        if not input_dir or not os.path.exists(input_dir):
            self.log(self.tr_('error_invalid_folder'), "red")
            return

        extractor_class = self.extractor_combo.currentData()
        translator_class = self.translator_combo.currentData()
        lang_source = self.source_lang_combo.currentData()
        lang_target = self.target_lang_combo.currentData()

        if lang_source == lang_target:
            self.log(self.tr_('error_same_lang'), "red")
            return

        is_valid, err_msg = self.validate_dynamic_form()
        if not is_valid:
            self.log(err_msg, "red")
            return

        dynamic_cfg = self.get_dynamic_config()

        self.btn_start.setEnabled(False)
        self.btn_start.setText(self.tr_('btn_processing'))
        self.btn_start.setIcon(qta.icon('fa5s.spinner', color='white', animation=qta.Spin(self.btn_start)))
        self.progress_bar.setVisible(True)
        self.progress_bar.setValue(0)
        self.log_output.clear()
        self._files_status.clear()

        self.log(self.tr_('log_starting', extractor=extractor_class.name, translator=translator_class.agent), "cyan")
        self.log(self.tr_('log_from_to', source=lang_source, target=lang_target), "cyan")

        from src.job import JobConfig, TranslationJob
        from src.workspace import Workspace

        synopsis_val = dynamic_cfg['translator_config'].get('synopsis')
        if not synopsis_val and hasattr(self, '_fallback_synopsis_input'):
            synopsis_val = self._fallback_synopsis_input.text().strip() or None

        output_dir = getattr(self.args, 'output', None) if self.args else None

        job_config = JobConfig(
            mode='content',
            extractor_name=extractor_class.name,
            translator_name=translator_class.agent,
            lang_source=lang_source,
            lang_target=lang_target,
            extractor_config=dynamic_cfg['extractor_config'],
            translator_config=dynamic_cfg['translator_config'],
            synopsis=synopsis_val,
            backup=self.chk_backup.isChecked(),
            verify=self.chk_verify.isChecked(),
            input_dir=input_dir,
            output_dir=output_dir
        )

        ws = Workspace(input_dir, output_dir=output_dir)
        job = TranslationJob(job_config, workspace=ws)

        self.worker = TranslationWorker(
            job=job,
            run_process_func=self.run_process_func
        )
        self.worker.signals.log.connect(self.log)
        self.worker.signals.finished.connect(self.on_finished)
        self.worker.signals.request_verification.connect(self.handle_verification)
        self.worker.signals.status_update.connect(self.update_progress)

        self.worker.start()

    def update_progress(self, data):
        if not data:
            return
        if isinstance(data, dict):
            file_name = data.get('file')
            if file_name:
                self._files_status[file_name] = data
            status_list = list(self._files_status.values())
        elif isinstance(data, list):
            for s in data:
                file_name = s.get('file')
                if file_name:
                    self._files_status[file_name] = s
            status_list = list(self._files_status.values())
        else:
            return

        total_files = len(status_list)
        done_files = sum(1 for s in status_list if s.get('status') in ['success', 'partial', 'erro', 'ignore'])
        processing_files = [os.path.basename(s.get('file', '')) for s in status_list if s.get('status') == 'process']
        if total_files > 0:
            percent = int((done_files / total_files) * 100)
            self.progress_bar.setValue(percent)
            if processing_files:
                proc_str = ", ".join(processing_files[:3])
                if len(processing_files) > 3:
                    proc_str += f" (+{len(processing_files) - 3})"
                self.statusBar().showMessage(f"{self.tr_('status_processing', done=done_files, total=total_files)} | {proc_str}")
            else:
                self.statusBar().showMessage(self.tr_('status_processing', done=done_files, total=total_files))

    def on_finished(self, success):
        self.btn_start.setEnabled(True)
        self.btn_start.setText(self.tr_('btn_start'))
        self.btn_start.setIcon(qta.icon('fa5s.play', color='white'))
        self.progress_bar.setVisible(False)

        if success:
            self.log(self.tr_('log_finished_success'), "green")
            self.statusBar().showMessage(self.tr_('status_done'))
        else:
            self.log(self.tr_('log_finished_error'), "red")
            self.statusBar().showMessage(self.tr_('status_error'))


def run_gui(args, lang_options, run_process_func):
    app = QApplication(sys.argv)
    apply_stylesheet(app, theme='dark_blue.xml')
    QDir.addSearchPath('icon', os.path.join(RESOURCES_PATH, 'theme'))
    QDir.addSearchPath('qt_material', os.path.join(os.path.dirname(qt_material.__file__), 'resources'))
    window = TranslationGUI(args, lang_options, run_process_func)
    window.show()
    sys.exit(app.exec_())

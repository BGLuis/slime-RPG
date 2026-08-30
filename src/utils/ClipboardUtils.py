import os
import shutil
import subprocess
from typing import Optional


def get_clipboard_text() -> Optional[str]:
    """
    Obtém o conteúdo de texto da área de transferência (clipboard) do sistema operacional.
    Tenta diferentes métodos compatíveis com Linux (Wayland/X11), Windows e macOS.
    """
    # 1. Tentar wl-paste (Wayland em Linux)
    if shutil.which('wl-paste'):
        try:
            res = subprocess.run(
                ['wl-paste', '--no-newline'],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
                timeout=1
            )
            if res.returncode == 0 and res.stdout:
                text = res.stdout.strip()
                if text:
                    return text
        except Exception:
            pass

    # 2. Tentar tkinter (disponível por padrão no Python em várias plataformas)
    try:
        import tkinter as tk
        r = tk.Tk()
        r.withdraw()
        text = r.clipboard_get()
        r.destroy()
        if text and isinstance(text, str):
            text = text.strip()
            if text:
                return text
    except Exception:
        pass

    # 3. Tentar xclip (X11 no Linux)
    if shutil.which('xclip'):
        try:
            res = subprocess.run(
                ['xclip', '-selection', 'clipboard', '-o'],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
                timeout=1
            )
            if res.returncode == 0 and res.stdout:
                text = res.stdout.strip()
                if text:
                    return text
        except Exception:
            pass

    # 4. Tentar xsel (X11 no Linux)
    if shutil.which('xsel'):
        try:
            res = subprocess.run(
                ['xsel', '--clipboard', '--output'],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
                timeout=1
            )
            if res.returncode == 0 and res.stdout:
                text = res.stdout.strip()
                if text:
                    return text
        except Exception:
            pass

    # 5. Tentar QApplication do PyQt5 se uma instância já existir
    try:
        from PyQt5.QtWidgets import QApplication
        app = QApplication.instance()
        if app:
            cb = app.clipboard().text()
            if cb:
                text = cb.strip()
                if text:
                    return text
    except Exception:
        pass

    return None


def get_clipboard_folder() -> Optional[str]:
    """
    Verifica se o conteúdo atual da área de transferência é um caminho de diretório
    válido e existente no sistema. Retorna o caminho absoluto da pasta ou None.
    """
    raw = get_clipboard_text()
    if not raw:
        return None

    candidate = raw.strip().strip("'\"").strip()
    if candidate.startswith("file://"):
        candidate = candidate[7:]

    # Remove quebras de linha caso haja mais texto copiado
    if "\n" in candidate:
        candidate = candidate.split("\n")[0].strip().strip("'\"").strip()

    expanded = os.path.expanduser(candidate)
    if os.path.isdir(expanded):
        abs_path = os.path.abspath(expanded)
        from src.services.GameDetector import is_trash_path
        if not is_trash_path(abs_path):
            return abs_path

    return None

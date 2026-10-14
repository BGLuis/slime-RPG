import os
import sqlite3
import threading

from src.utils.paths import get_settings_db_path

DB_PATH = get_settings_db_path()


class SettingsStore:
    """Armazena preferências simples de chave/valor (ex: idioma da interface) em SQLite."""

    _lock = threading.Lock()

    def __init__(self, db_path=None):
        if db_path is None:
            db_path = get_settings_db_path()
        self.db_path = db_path
        os.makedirs(os.path.dirname(db_path) or '.', exist_ok=True)
        self._conn = sqlite3.connect(db_path, check_same_thread=False)
        self._conn.execute(
            "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)"
        )
        self._conn.commit()

    def get(self, key, default=None):
        with self._lock:
            row = self._conn.execute(
                "SELECT value FROM settings WHERE key = ?", (key,)
            ).fetchone()
        return row[0] if row else default

    def set(self, key, value):
        with self._lock:
            self._conn.execute(
                "INSERT INTO settings (key, value) VALUES (?, ?) "
                "ON CONFLICT(key) DO UPDATE SET value = excluded.value",
                (key, value),
            )
            self._conn.commit()

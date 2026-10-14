import os
import shutil
import tempfile
import logging
import threading


class Workspace:
    """
    Gerencia o ciclo de vida temporário, isolamento e zero cópias em disco.

    Permite que os extratores leiam diretamente da pasta de entrada (ou backup)
    e gravem os arquivos processados temporariamente em `process_dir`,
    transferindo-os para `output_dir` com substituição atômica no encerramento.
    """

    def __init__(self, input_dir, output_dir=None, root_dir=None, keep_temp=False):
        self._cleaned = True
        self._lock = threading.Lock()
        self.temp_dir = None

        if not input_dir:
            raise ValueError("input_dir não pode ser vazio ou None.")

        self._cleaned = False
        self.input_dir = os.path.abspath(input_dir)
        self.output_dir = os.path.abspath(output_dir) if output_dir else self.input_dir
        self.root_dir = os.path.abspath(root_dir) if root_dir else None
        self.keep_temp = keep_temp

        # Cria pasta temporária isolada para trabalho intermediário
        self.temp_dir = tempfile.mkdtemp(prefix="slime_ws_", dir=self.root_dir)
        self.process_dir = os.path.join(self.temp_dir, "process")
        self.output_work_dir = os.path.join(self.temp_dir, "output")

        os.makedirs(self.process_dir, exist_ok=True)
        os.makedirs(self.output_work_dir, exist_ok=True)

    @property
    def is_in_place(self):
        """Retorna True se o destino for a própria pasta de entrada."""
        return os.path.normpath(self.input_dir) == os.path.normpath(self.output_dir)

    def clean_process(self):
        """Esvazia os arquivos da pasta temporária de processamento."""
        if os.path.exists(self.process_dir):
            for item in os.listdir(self.process_dir):
                p = os.path.join(self.process_dir, item)
                if os.path.isdir(p):
                    shutil.rmtree(p, ignore_errors=True)
                else:
                    try:
                        os.remove(p)
                    except Exception:
                        pass

    @staticmethod
    def atomic_replace(src_file, dst_file):
        """
        Substitui dst_file por src_file de maneira atômica.
        Grava em arquivo temporário no mesmo diretório de destino e usa os.replace.
        """
        dst_dir = os.path.dirname(dst_file)
        os.makedirs(dst_dir, exist_ok=True)

        tmp_file = f"{dst_file}.tmp_{os.getpid()}_{threading.get_ident()}"
        shutil.copy2(src_file, tmp_file)
        try:
            os.replace(tmp_file, dst_file)
        except Exception:
            if os.path.exists(tmp_file):
                try:
                    os.remove(tmp_file)
                except Exception:
                    pass
            # Fallback seguro para ambientes com restrições de cross-device link
            shutil.copy2(src_file, dst_file)

    def commit(self, source_dir=None, target_dir=None, atomic=True):
        """
        Grava os arquivos da pasta temporária na saída final com substituição atômica.
        """
        src = source_dir or self.process_dir
        dst = target_dir or self.output_dir

        if not os.path.exists(src):
            return []

        committed_files = []
        for root, _, files in os.walk(src):
            for f in files:
                src_path = os.path.join(root, f)
                rel_path = os.path.relpath(src_path, src)
                dst_path = os.path.join(dst, rel_path)

                if os.path.abspath(src_path) == os.path.abspath(dst_path):
                    continue

                if atomic:
                    self.atomic_replace(src_path, dst_path)
                else:
                    os.makedirs(os.path.dirname(dst_path), exist_ok=True)
                    shutil.copy2(src_path, dst_path)

                committed_files.append(rel_path)

        return committed_files

    def cleanup(self):
        """Remove a pasta temporária do workspace de forma idempotente e segura."""
        with self._lock:
            if not self._cleaned and not self.keep_temp:
                if self.temp_dir and os.path.exists(self.temp_dir):
                    try:
                        shutil.rmtree(self.temp_dir, ignore_errors=True)
                    except Exception as e:
                        logging.warning(
                            f"Erro ao remover pasta temporária do workspace {self.temp_dir}: {e}"
                        )
                self._cleaned = True

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.cleanup()

    def __del__(self):
        self.cleanup()

    def __repr__(self):
        return (
            f"Workspace(input_dir={self.input_dir!r}, output_dir={self.output_dir!r}, "
            f"temp_dir={self.temp_dir!r})"
        )

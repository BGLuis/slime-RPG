import os
import shutil
import logging

APP_NAME = "slime-rpg"


def get_app_dir(dir_type="data"):
    """
    Retorna o diretório padrão do usuário para o tipo especificado (data, cache, config, log).
    Respeita variáveis de ambiente de sobrescrita para testes e isolamento.
    """
    env_map = {
        "data": "SLIME_DATA_DIR",
        "cache": "SLIME_CACHE_DIR",
        "config": "SLIME_CONFIG_DIR",
        "log": "SLIME_LOG_DIR",
    }
    env_var = env_map.get(dir_type)
    if env_var and os.environ.get(env_var):
        path = os.path.abspath(os.environ[env_var])
        os.makedirs(path, exist_ok=True)
        return path

    try:
        import platformdirs
        if dir_type == "data":
            path = platformdirs.user_data_dir(APP_NAME)
        elif dir_type == "cache":
            path = platformdirs.user_cache_dir(APP_NAME)
        elif dir_type == "config":
            path = platformdirs.user_config_dir(APP_NAME)
        elif dir_type == "log":
            path = platformdirs.user_log_dir(APP_NAME)
        else:
            path = platformdirs.user_data_dir(APP_NAME)
    except Exception:
        home = os.path.expanduser("~")
        if dir_type == "cache":
            path = os.path.join(home, ".cache", APP_NAME)
        elif dir_type == "log":
            path = os.path.join(home, ".local", "state", APP_NAME, "log")
        elif dir_type == "config":
            path = os.path.join(home, ".config", APP_NAME)
        else:
            path = os.path.join(home, ".local", "share", APP_NAME)

    os.makedirs(path, exist_ok=True)
    return path


def get_memory_db_path():
    """Retorna o caminho padrão para memory.db."""
    env_path = os.environ.get("SLIME_MEMORY_DB")
    if env_path:
        os.makedirs(os.path.dirname(os.path.abspath(env_path)), exist_ok=True)
        return os.path.abspath(env_path)

    dest_path = os.path.join(get_app_dir("data"), "memory.db")
    if os.environ.get("SLIME_DATA_DIR"):
        return dest_path
    project_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    legacy_candidates = [
        os.path.join(os.getcwd(), "cache", "memory.db"),
        os.path.join(project_root, "cache", "memory.db"),
    ]
    for legacy_path in legacy_candidates:
        if os.path.isfile(legacy_path) and not os.path.isfile(dest_path):
            try:
                os.makedirs(os.path.dirname(dest_path), exist_ok=True)
                shutil.copy2(legacy_path, dest_path)
                logging.info(f"Base de memória migrada de {legacy_path} para {dest_path}")
                break
            except Exception as e:
                logging.warning(f"Não foi possível migrar cache legado: {e}")
    return dest_path


def get_settings_db_path():
    """Retorna o caminho padrão para settings.db."""
    env_path = os.environ.get("SLIME_SETTINGS_DB")
    if env_path:
        os.makedirs(os.path.dirname(os.path.abspath(env_path)), exist_ok=True)
        return os.path.abspath(env_path)

    dest_path = os.path.join(get_app_dir("config"), "settings.db")
    if os.environ.get("SLIME_CONFIG_DIR"):
        return dest_path
    project_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    legacy_candidates = [
        os.path.join(os.getcwd(), "cache", "settings.db"),
        os.path.join(project_root, "cache", "settings.db"),
    ]
    for legacy_path in legacy_candidates:
        if os.path.isfile(legacy_path) and not os.path.isfile(dest_path):
            try:
                os.makedirs(os.path.dirname(dest_path), exist_ok=True)
                shutil.copy2(legacy_path, dest_path)
                logging.info(f"Configurações migradas de {legacy_path} para {dest_path}")
                break
            except Exception as e:
                logging.warning(f"Não foi possível migrar configurações legadas: {e}")
    return dest_path


def get_log_file_path():
    """Retorna o caminho padrão para processamento.log."""
    env_path = os.environ.get("SLIME_LOG_FILE")
    if env_path:
        os.makedirs(os.path.dirname(os.path.abspath(env_path)), exist_ok=True)
        return os.path.abspath(env_path)

    log_dir = get_app_dir("log")
    return os.path.join(log_dir, "processamento.log")

import pkgutil
import importlib
import os

__all__ = []

# Importa todos os módulos no diretório atual
for _, module_name, _ in pkgutil.iter_modules(__path__):
    __all__.append(module_name)
    importlib.import_module(f"{__name__}.{module_name}")

# Importa subpacotes conhecidos (como rpgmaker e wolfrpg)
subpackages = ['rpgmaker', 'wolfrpg']
for subpkg in subpackages:
    if os.path.isdir(os.path.join(__path__[0], subpkg)):
        importlib.import_module(f"{__name__}.{subpkg}")

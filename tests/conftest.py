import os
import sys

# Garante que "src" seja importável independente do diretório de onde o pytest for chamado.
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

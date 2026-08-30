# ==============================================================================
# Makefile para Extractor & Translation
# ==============================================================================

SHELL := /bin/bash
PROJECT_DIR := $(shell pwd)
VENV_DIR := $(PROJECT_DIR)/.venv
PYTHON := $(VENV_DIR)/bin/python
PIP := $(VENV_DIR)/bin/pip
CLI_WRAPPER := $(PROJECT_DIR)/scripts/extractor-translation
LOCAL_BIN := $(HOME)/.local/bin
BINARY_NAME := extractor-translation

# Fallback se a .venv não existir
ifeq ($(wildcard $(PYTHON)),)
    PYTHON := python3
    PIP := pip3
endif

.PHONY: all help run cli gui install install-cli uninstall-cli install-desktop uninstall-desktop uninstall venv test clean

all: help

help:
	@echo ""
	@echo "=================================================================="
	@echo "  🎮 Extractor & Translation - Comandos Disponíveis"
	@echo "=================================================================="
	@echo ""
	@echo "  Execução Rápida:"
	@echo "    make run             - Inicia no modo interativo (CLI)"
	@echo "    make gui             - Inicia na interface gráfica (PyQt5)"
	@echo ""
	@echo "  Instalação Global no Sistema (Usuário):"
	@echo "    make install         - Instala comando no terminal + menu do sistema"
	@echo "    make install-cli     - Instala apenas o comando 'extractor-translation' em ~/.local/bin"
	@echo "    make install-desktop - Instala atalhos no gerenciador de arquivos (Nemo, Nautilus, Dolphin)"
	@echo "    make uninstall       - Remove o comando do terminal e os menus de contexto"
	@echo ""
	@echo "  Desenvolvimento & Ambiente:"
	@echo "    make venv            - Cria/atualiza o ambiente virtual (.venv) e dependências"
	@echo "    make test            - Executa os testes automatizados com pytest"
	@echo "    make clean           - Limpa diretórios temporários e caches (__pycache__)"
	@echo ""
	@echo "=================================================================="
	@echo ""

run:
	@$(PYTHON) main.py --interactive

cli: run

gui:
	@$(PYTHON) main.py --gui

venv:
	@if [ ! -d "$(VENV_DIR)" ]; then \
		echo "Criando ambiente virtual em $(VENV_DIR)..."; \
		python3 -m venv $(VENV_DIR); \
	fi
	@echo "Instalando dependências de requirements.txt..."
	@$(PIP) install -r requirements.txt

install-cli:
	@echo "Configurando wrapper executável..."
	@chmod +x $(CLI_WRAPPER)
	@mkdir -p $(LOCAL_BIN)
	@ln -sf $(CLI_WRAPPER) $(LOCAL_BIN)/$(BINARY_NAME)
	@echo "✓ Comando '$(BINARY_NAME)' instalado com sucesso em $(LOCAL_BIN)/$(BINARY_NAME)"
	@if [[ ":$$PATH:" != *":$(LOCAL_BIN):"* ]]; then \
		echo "⚠️  AVISO: '$(LOCAL_BIN)' pode não estar no seu PATH. Adicione-o ao seu ~/.bashrc ou ~/.zshrc se necessário."; \
	fi
	@echo ""
	@echo "Exemplo de uso em qualquer pasta (inclusive dentro da pasta do jogo):"
	@echo "  $(BINARY_NAME) --interactive"
	@echo "  $(BINARY_NAME) -i . -e RPGMaker -t Google -s ja -d pt"
	@echo "  $(BINARY_NAME) --gui"

uninstall-cli:
	@echo "Removendo $(LOCAL_BIN)/$(BINARY_NAME)..."
	@rm -f $(LOCAL_BIN)/$(BINARY_NAME)
	@echo "✓ Comando '$(BINARY_NAME)' desinstalado do terminal."

install-desktop:
	@chmod +x scripts/install_linux_integration.sh
	@bash scripts/install_linux_integration.sh

uninstall-desktop:
	@rm -f $(HOME)/.local/share/applications/extractor-translation.desktop
	@rm -f $(HOME)/.local/share/nemo/actions/translate-game.nemo_action
	@rm -f $(HOME)/.local/share/file-manager/actions/translate-game.nemo_action
	@rm -f "$(HOME)/.local/share/nautilus/scripts/Translate Folder"
	@rm -f $(HOME)/.local/share/kservices5/ServiceMenus/extractor-translation.desktop
	@echo "✓ Integrações de desktop e menu de contexto removidas."

install: install-cli install-desktop
	@echo "✓ Instalação completa concluída com sucesso!"

uninstall: uninstall-cli uninstall-desktop
	@echo "✓ Desinstalação completa concluída."

test:
	@if [ -f "$(VENV_DIR)/bin/pytest" ]; then \
		$(VENV_DIR)/bin/pytest; \
	else \
		pytest; \
	fi

clean:
	@echo "Limpando arquivos temporários e caches..."
	@find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
	@find . -type d -name ".pytest_cache" -exec rm -rf {} + 2>/dev/null || true
	@echo "✓ Limpeza concluída."

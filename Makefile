# ==============================================================================
# Makefile para Slime (Extractor & Translation)
# ==============================================================================

SHELL := /bin/bash
PROJECT_DIR := $(shell pwd)
VENV_DIR := $(PROJECT_DIR)/.venv
PYTHON := $(VENV_DIR)/bin/python
PIP := $(VENV_DIR)/bin/pip
CLI_WRAPPER := $(PROJECT_DIR)/scripts/slime
LOCAL_BIN := $(HOME)/.local/bin
BINARY_NAME := slime
ALIASES ?= slm sl extractor-translation

# Fallback se a .venv não existir
ifeq ($(wildcard $(PYTHON)),)
    PYTHON := python3
    PIP := pip3
endif

.PHONY: all help run cli gui install install-cli uninstall-cli install-desktop uninstall-desktop uninstall venv test clean add-alias remove-alias list-aliases lint lint-full install-hooks

all: help

help:
	@echo ""
	@echo "=================================================================="
	@echo "  🎮 Slime (Extractor & Translation) - Comandos Disponíveis"
	@echo "=================================================================="
	@echo ""
	@echo "  Execução Rápida:"
	@echo "    make run             - Inicia no modo interativo (CLI)"
	@echo "    make gui             - Inicia na interface gráfica (PyQt5)"
	@echo ""
	@echo "  Instalação Global no Sistema (Usuário):"
	@echo "    make install         - Instala comando no terminal + atalhos + menu do sistema"
	@echo "    make install-cli     - Instala o comando '$(BINARY_NAME)' e atalhos ($(ALIASES)) em ~/.local/bin"
	@echo "    make uninstall-cli   - Remove o comando '$(BINARY_NAME)' e seus atalhos de ~/.local/bin"
	@echo "    make add-alias       - Adiciona um novo atalho no terminal (ex: make add-alias ALIAS=slx)"
	@echo "    make remove-alias    - Remove um atalho do terminal (ex: make remove-alias ALIAS=slx)"
	@echo "    make list-aliases    - Lista os atalhos atualmente instalados"
	@echo "    make install-desktop - Instala atalhos no gerenciador de arquivos (Nemo, Nautilus, Dolphin)"
	@echo "    make uninstall       - Remove o comando do terminal e os menus de contexto"
	@echo ""
	@echo "  Desenvolvimento & Ambiente:"
	@echo "    make venv            - Cria/atualiza o ambiente virtual (.venv) e dependências"
	@echo "    make test            - Executa os testes automatizados com pytest"
	@echo "    make lint            - Análise estática (erros reais: NameError, SyntaxError etc.)"
	@echo "    make lint-full       - Análise estática completa (inclui imports/variáveis não usadas)"
	@echo "    make install-hooks   - Instala o git hook de pre-commit que roda 'make lint'"
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
	@echo "✓ Comando principal '$(BINARY_NAME)' instalado em $(LOCAL_BIN)/$(BINARY_NAME)"
	@for alias in $(ALIASES); do \
		ln -sf $(CLI_WRAPPER) $(LOCAL_BIN)/$$alias; \
		echo "✓ Atalho '$$alias' instalado em $(LOCAL_BIN)/$$alias -> $(BINARY_NAME)"; \
	done
	@if [[ ":$$PATH:" != *":$(LOCAL_BIN):"* ]]; then \
		echo "⚠️  AVISO: '$(LOCAL_BIN)' pode não estar no seu PATH. Adicione-o ao seu ~/.bashrc ou ~/.zshrc se necessário."; \
	fi
	@echo ""
	@echo "Exemplo de uso em qualquer pasta (inclusive dentro da pasta do jogo):"
	@echo "  $(BINARY_NAME) --interactive"
	@echo "  slm --interactive"
	@echo "  sl -i . -e RPGMaker -t Google -s ja -d pt"
	@echo "  $(BINARY_NAME) --gui"

uninstall-cli:
	@echo "Removendo $(LOCAL_BIN)/$(BINARY_NAME)..."
	@rm -f $(LOCAL_BIN)/$(BINARY_NAME)
	@for alias in $(ALIASES); do \
		rm -f $(LOCAL_BIN)/$$alias; \
		echo "✓ Atalho '$$alias' removido."; \
	done
	@echo "✓ Comandos desinstalados do terminal."

add-alias:
	@if [ -z "$(ALIAS)" ]; then \
		echo "Erro: Especifique o atalho com ALIAS=<nome>. Exemplo: make add-alias ALIAS=slx"; \
		exit 1; \
	fi
	@mkdir -p $(LOCAL_BIN)
	@ln -sf $(CLI_WRAPPER) $(LOCAL_BIN)/$(ALIAS)
	@echo "✓ Atalho '$(ALIAS)' instalado com sucesso em $(LOCAL_BIN)/$(ALIAS) -> $(CLI_WRAPPER)"

remove-alias:
	@if [ -z "$(ALIAS)" ]; then \
		echo "Erro: Especifique o atalho com ALIAS=<nome>. Exemplo: make remove-alias ALIAS=slx"; \
		exit 1; \
	fi
	@rm -f $(LOCAL_BIN)/$(ALIAS)
	@echo "✓ Atalho '$(ALIAS)' removido de $(LOCAL_BIN)."

list-aliases:
	@chmod +x $(CLI_WRAPPER)
	@$(CLI_WRAPPER) --list-aliases

install-desktop:
	@chmod +x scripts/install_linux_integration.sh
	@bash scripts/install_linux_integration.sh

uninstall-desktop:
	@rm -f $(HOME)/.local/share/applications/slime.desktop
	@rm -f $(HOME)/.local/share/applications/extractor-translation.desktop
	@rm -f $(HOME)/.local/share/nemo/actions/translate-game.nemo_action
	@rm -f $(HOME)/.local/share/file-manager/actions/translate-game.nemo_action
	@rm -f "$(HOME)/.local/share/nautilus/scripts/Translate Folder"
	@rm -f $(HOME)/.local/share/kservices5/ServiceMenus/slime.desktop
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

# Lint "critico": só falha para bugs de fato (nome indefinido, erro de sintaxe,
# etc. - a mesma classe do bug do 'sys' não importado). Roda em segundos e é
# seguro deixar bloqueante (pre-commit/CI).
LINT_TARGETS := src main.py
FLAKE8 := $(shell [ -f "$(VENV_DIR)/bin/flake8" ] && echo "$(VENV_DIR)/bin/flake8" || echo "flake8")

lint:
	@$(FLAKE8) --select=E9,F63,F7,F82 $(LINT_TARGETS)

lint-full:
	@$(FLAKE8) $(LINT_TARGETS)

install-hooks:
	@mkdir -p .git/hooks
	@printf '%s\n' \
		'#!/bin/sh' \
		'make lint' \
		> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "✓ Hook de pre-commit instalado (roda 'make lint' antes de cada commit)."

clean:
	@echo "Limpando arquivos temporários e caches..."
	@find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
	@find . -type d -name ".pytest_cache" -exec rm -rf {} + 2>/dev/null || true
	@echo "✓ Limpeza concluída."

#!/usr/bin/env bash
# Integração com o desktop Linux: entrada de aplicativo e o menu de contexto
# "Translate Game Folder" no Nemo, Nautilus e Dolphin.
# Uso: linux_integration.sh install|uninstall <caminho-do-binario-slime>
set -euo pipefail

action="${1:-}"
bin="${2:-}"
if [[ "$action" != install && "$action" != uninstall ]] || [[ -z "$bin" ]]; then
    echo "Uso: $0 install|uninstall <binario-slime>" >&2
    exit 2
fi

data="${XDG_DATA_HOME:-$HOME/.local/share}"
desktop_file="$data/applications/slime.desktop"
nemo_files=("$data/nemo/actions/translate-game.nemo_action" "$data/file-manager/actions/translate-game.nemo_action")
nautilus_script="$data/nautilus/scripts/Translate Folder"
dolphin_file="$HOME/.local/share/kservices5/ServiceMenus/slime.desktop"

uninstall() {
    rm -f "$desktop_file" "${nemo_files[@]}" "$nautilus_script" "$dolphin_file"
    echo "✓ Integração com o desktop removida."
}

install() {
    mkdir -p "$(dirname "$desktop_file")"
    cat > "$desktop_file" <<EOF
[Desktop Entry]
Version=1.0
Name=Slime RPG
Comment=Game Translation Tool (RPG Maker, WOLF RPG, JSON, CSV)
Exec="$bin" --gui -i %f
Icon=text-x-generic
Terminal=false
Type=Application
Categories=Utility;Development;
MimeType=inode/directory;
EOF
    echo "✓ Atalho de aplicativo criado em $desktop_file"

    # Nemo lê ~/.local/share/nemo/actions; file-manager/actions é a alternativa quando já existe.
    nemo_file="${nemo_files[0]}"
    [[ -d "$data/file-manager/actions" ]] && nemo_file="${nemo_files[1]}"
    mkdir -p "$(dirname "$nemo_file")"
    cat > "$nemo_file" <<EOF
[Nemo Action]
Name=Translate Game Folder
Comment=Automatic extraction and translation of game files
Exec="$bin" --gui -i %F
Icon-Name=text-x-generic
Selection=dir
Extensions=any;
EOF

    mkdir -p "$(dirname "$nautilus_script")"
    printf '#!/bin/bash\nexec "%s" --gui -i "$1"\n' "$bin" > "$nautilus_script"
    chmod +x "$nautilus_script"
    echo "✓ Ações criadas para Nemo e Nautilus"

    mkdir -p "$(dirname "$dolphin_file")"
    cat > "$dolphin_file" <<EOF
[Desktop Entry]
Type=Service
ServiceTypes=KonqPopupMenu/Plugin
MimeType=inode/directory;
Actions=translateGame;
X-KDE-Priority=TopLevel

[Desktop Action translateGame]
Name=Translate Game Folder (Slime)
Icon=text-x-generic
Exec="$bin" --gui -i %f
EOF
    echo "✓ Menu de serviço criado para o Dolphin (KDE)"
    echo "Talvez seja preciso reiniciar o gerenciador de arquivos (nemo -q, nautilus -q) ou fazer logout."
}

"$action"

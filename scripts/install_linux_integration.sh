#!/bin/bash

# Script to install Linux desktop integration for Extractor & Translation tool
# This adds a "Translate Game" option to the right-click menu for folders

set -e

PROJECT_ROOT=$(pwd)
MAIN_PY="$PROJECT_ROOT/main.py"
PYTHON_BIN="$PROJECT_ROOT/.venv/bin/python"

# If .venv doesn't exist, fallback to system python
if [ ! -f "$PYTHON_BIN" ]; then
    PYTHON_BIN="python3"
fi

echo "--- Installing Linux Desktop Integration ---"
echo "Project Root: $PROJECT_ROOT"

# 1. Create standard .desktop application entry
APP_DIR="$HOME/.local/share/applications"
mkdir -p "$APP_DIR"
rm -f "$APP_DIR/extractor-translation.desktop"

cat > "$APP_DIR/slime.desktop" <<EOF
[Desktop Entry]
Version=1.0
Name=Slime (Extractor & Translation)
Comment=Game Translation Tool (RPG Maker, WOLF RPG, JSON, CSV)
Exec=$PROJECT_ROOT/scripts/slime --gui --input %f
Icon=text-x-generic
Terminal=false
Type=Application
Categories=Utility;Development;
MimeType=inode/directory;
EOF

echo "✓ Created application shortcut in $APP_DIR"

# 2. Create Nautilus/Nemo Action (Gnome/Cinnamon/MATE)
# Ref: https://github.com/linuxmint/nemo/blob/master/files/usr/share/nemo/actions/sample.nemo_action
ACTIONS_DIR="$HOME/.local/share/nemo/actions"
[ -d "$HOME/.local/share/file-manager/actions" ] && ACTIONS_DIR="$HOME/.local/share/file-manager/actions"
mkdir -p "$ACTIONS_DIR"

cat > "$ACTIONS_DIR/translate-game.nemo_action" <<EOF
[Nemo Action]
Name=Translate Game Folder
Comment=Automatic extraction and translation of game files
Exec=$PYTHON_BIN $MAIN_PY --gui --input %F
Icon-Name=text-x-generic
Selection=dir
Extensions=any;
EOF

# For Nautilus (using .desktop style in a specific folder)
NAUTILUS_SCRIPTS="$HOME/.local/share/nautilus/scripts"
mkdir -p "$NAUTILUS_SCRIPTS"
cat > "$NAUTILUS_SCRIPTS/Translate Folder" <<EOF
#!/bin/bash
$PYTHON_BIN $MAIN_PY --gui --input "\$1"
EOF
chmod +x "$NAUTILUS_SCRIPTS/Translate Folder"

echo "✓ Created file manager actions for Nautilus/Nemo"

# 3. Create Dolphin Service Menu (KDE)
DOLPHIN_DIR="$HOME/.local/share/kservices5/ServiceMenus"
mkdir -p "$DOLPHIN_DIR"
rm -f "$DOLPHIN_DIR/extractor-translation.desktop"

cat > "$DOLPHIN_DIR/slime.desktop" <<EOF
[Desktop Entry]
Type=Service
ServiceTypes=KonqPopupMenu/Plugin
MimeType=inode/directory;
Actions=translateGame;
X-KDE-Priority=TopLevel

[Desktop Action translateGame]
Name=Translate Game Folder (Slime)
Icon=text-x-generic
Exec=$PROJECT_ROOT/scripts/slime --gui --input %f
EOF

echo "✓ Created service menu for Dolphin (KDE)"

echo ""
echo "Installation complete!"
echo "You may need to restart your file manager (nemo -q, nautilus -q, or logout) for changes to take effect."
echo "You can now right-click any folder and select 'Translate Game Folder'."

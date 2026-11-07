package i18n

var en = map[string]string{
	"usage": `Slime RPG (Go Edition) v%s

USAGE:
  slime [command] [options]

COMMANDS:
  translate       Extract, translate and reinject game text (default)
  verify          Check the translated folder against .slime/meta.json
  adopt           Register the metadata of an already translated game
  filenames       Translate the file names of a folder
  tm              Translation Memory: count, TMX export/import and glossary
  list            List supported extractors, translators and languages
  lang            Show or save the interface language (pt, en)
  version         Show detailed version and build information

TRANSLATION OPTIONS:
  -i, --input        Game root folder or file (default: current folder)
  -o, --output       Destination folder (default: the input itself)
  -e, --extractor    Force the extractor: rpgmaker, wolfrpg, csv, json (default: auto-detect)
  --opt key=value    Extractor option; repeat for several (see EXTRACTOR OPTIONS)
  -t, --translator   Provider: google, deepl, ollama (default: google)
  -s, --source       Source language (default: ja)
  -d, --target       Target language (default: pt)
  --synopsis         Game synopsis to give Ollama context
  --model            Ollama model (default: $OLLAMA_MODEL or qwen2.5)
  --no-backup        Do not create the <folder>-<source> backup
  --memory-only      Apply only the Translation Memory, no network (alias: --offline)
  --switch-engine    Ignore automatic translations from other providers in memory
  --incremental      Translate only new or changed files (default: true)
  --no-proxy         Do not use the proxy pool when Google blocks the IP
  --dry-run          Compute the pre-flight estimate without changing files
  --review           Pause before writing to inspect the processing folder (terminal only)
  --json             Emit events as JSON Lines for automation
  --tui / --gui      Force the terminal or desktop interface
  --lang pt|en       Interface language for this run (or SLIME_LANG); 'slime lang en' saves the preference
  -h, --help         Show this message

EXTRACTOR OPTIONS (--opt key=value):
  csv.columns=value,text     Columns to translate, comma-separated (empty: automatic)
  csv.delimiter=;            CSV delimiter; use \t for tab (empty: automatic)
  json.keys=text,name        JSON keys to translate; * or all for every key (empty: default)
  wolf.preserve_title=true   Keep the Game.dat title so saves stay valid (default: true)
  wolf.target_font=Name      Font written to Game.dat (empty: keep the game's font)
  wolf.repack_mode=false     true also generates Data/Data.wolf (default: loose files in Data/)

TRANSLATION MEMORY:
  slime tm                                              # total entries
  slime tm export --src ja --tgt pt --out f.tmx [--status reviewed,locked]
  slime tm import --file f.tmx [--engine human] [--status reviewed|locked|machine]
  slime tm glossary --src ja --tgt pt --source "ルイ" --target "Rui"
  slime tm glossary --src ja --tgt pt --list
  slime tm repair [--db memory.db] [--apply]            # dry-run by default; --apply backs up and writes
  slime tm migrate-legacy --dir cache [--db memory.db]  # imports cache/<engine>/cache_<src>_<tgt>.json

FILE NAMES:
  slime filenames -i <folder> [-o <output>] [-t google -s ja -d pt] [--keep-original]

VERIFY AND ADOPT:
  slime verify <translated-folder> [--original <folder>]
  slime adopt <translated-folder> --original <folder> -e <extractor> [-s en -d pt]

EXAMPLES:
  slime                                   # Interactive TUI in the current folder
  slime --gui                             # Desktop window
  slime translate -i ./MyGame -d pt       # Headless translation
  slime -i ./MyGame -t deepl --no-backup
  slime -i ./texts -e json --opt json.keys=text,name
  slime -i ./loc.csv --opt csv.delimiter=';' --opt csv.columns=Text
`,

	"unknown_command":        "Unknown command: %s\n\n",
	"err_gui_start":          "Error starting desktop interface: %v\n",
	"err_tui_start":          "Error starting TUI: %v\n",
	"err_option_format":      "use key=value (got %q)",
	"err_deepl_key":          "set DEEPL_API_KEY to use DeepL",
	"err_unknown_translator": "unknown translator: %s (use google, deepl or ollama)",
	"err_detect":             "Error detecting game in %s: %v\n",
	"err_estimate":           "Error computing estimate: %v\n",
	"err_run":                "Error during translation: %v\n",
	"err_memory_resolve":     "error resolving memory database: %v",
	"err_memory_open":        "error opening memory database %s: %v",

	"detected_game":    "Game detected: %s (%s)\n",
	"detected_data":    "Data folder: %s (%d files)\n",
	"detected_service": "Service: %s | Languages: %s → %s\n",
	"estimate_title":   "\n--- Pre-flight Estimate ---",
	"estimate_files":   "Files: %d (to process: %d) | Segments: %d (in memory: %d)\n",
	"estimate_time":    "Estimated time: %s | Backup: %s\n",
	"review_prompt":    "\nReview the translated files in: %s\nPress Enter to write to the game, 'n' to abort: ",

	"event_phase": "[PHASE] %s: %s\n",
	"event_warn":  "[WARN] %s: %s\n",
	"event_end":   "[END] %d complete, %d partial, %d with errors in %s\n",

	"summary_failed":  "--- Files that failed processing ---",
	"summary_partial": "--- Partially translated files (<100%) ---",
	"summary_line":    "Segments: %d (memory: %d, new: %d) | Backup: %s | Metadata: %s\n",

	"verify_no_meta": "No metadata in %s. Use 'slime adopt' to register the game.\n",
	"verify_summary": "Summary: %s\n",

	"adopt_usage":    "adopt requires the translated folder, --original and -e/--extractor (rpgmaker, wolfrpg, csv, json).",
	"adopt_no_dir":   "Folder does not exist: %s\n",
	"adopt_saved":    "Metadata written to %s\n  %d file(s) registered: %s\n",
	"flag_original":  "Folder with the original files",
	"flag_extractor": "Extractor: rpgmaker, wolfrpg, csv, json",
	"flag_source":    "Source language",
	"flag_target":    "Target language",

	"tm_count":           "Translation Memory (%s): %d entries registered.\n",
	"tm_export_need":     "tm export requires --src, --tgt and --out",
	"tm_export_done":     "%d segments exported to %s\n",
	"tm_import_need":     "tm import requires --file and --status reviewed, locked or machine",
	"tm_import_done":     "%d segments imported (status=%s), %d skipped (incomplete)\n",
	"tm_gloss_need":      "tm glossary requires --src and --tgt",
	"tm_gloss_terms":     "Use --source and --target to lock a term, or --list to list them.",
	"tm_locked":          "Locked: %q -> %q\n",
	"tm_unknown_sub":     "Unknown subcommand: tm %s (use export, import or glossary)\n",
	"tm_verb_would":      "would be affected",
	"tm_verb_applied":    "affected",
	"tm_repair_db":       "Database: %s\n",
	"tm_repair_case":     "Control code casing restored",
	"tm_repair_name":     "\\NAME[...] tags normalized",
	"tm_repair_xtok":     "Rows with __XTOK_ removed",
	"tm_repair_engine":   "Rows from test engines removed",
	"tm_repair_rows":     "%s: %d rows %s\n",
	"tm_repair_prot":     "Reviewed/locked rows %s: %d\n",
	"tm_repair_backup":   "Backup: %s\n",
	"tm_repair_dry":      "Dry-run: nothing was changed. Use --apply to write.",
	"tm_migrate_need":    "tm migrate-legacy requires --dir",
	"tm_migrate_done":    "%d files, %d entries imported into %s\n",
	"tm_migrate_skip":    "Skipped (null translation): %d; with source == target: %d\n",
	"filenames_kept":     "Originals kept: %d\n",
	"list_extractors":    "Available extractors:",
	"list_translators":   "Available translators:",
	"list_languages":     "Supported languages:",
	"extractor_rpgmaker": "rpgmaker (RPG Maker MV/MZ/VX Ace/XP)",
	"extractor_wolfrpg":  "wolfrpg (Wolf RPG)",
	"extractor_csv":      "csv (CSV)",
	"extractor_json":     "json (Json)",
	"lang_pt":            "Portuguese",
	"lang_en":            "English",
	"lang_es":            "Spanish",
	"lang_fr":            "French",
	"lang_it":            "Italian",
	"lang_ja":            "Japanese",
	"lang_auto":          "Automatic (Detect)",

	"lang_current":   "Interface language: %s (%s)\n",
	"lang_saved":     "Interface language saved: %s. Restart the application to apply it.\n",
	"lang_invalid":   "Unsupported language: %s (use pt or en)\n",
	"lang_save_fail": "Could not save the preference: %v\n",

	"ui_title":                 "Slime RPG",
	"ui_label_language":        "Interface Language",
	"ui_language_changed":      "Language changed to %s. Restart the application to apply it.",
	"ui_step_game":             "1 Game",
	"ui_step_configure":        "2 Configure",
	"ui_step_translate":        "3 Translation",
	"ui_step_result":           "4 Result",
	"ui_which_game":            "Which game are we translating?",
	"ui_which_game_sub":        "Slime looked in the current folder, the clipboard and recent games.",
	"ui_detected_here":         "Game detected here",
	"ui_use_game":              "Use this game ›",
	"ui_browse_folders":        "Browse folders…",
	"ui_drop_hint":             "or drag the game folder here",
	"ui_recent_games":          "RECENT GAMES",
	"ui_tip":                   "Tip: <code>slime translate -i &lt;folder&gt;</code> does the same without prompts in the terminal.",
	"ui_lang_detected":         "%s detected",
	"ui_lang_japanese":         "Japanese",
	"ui_label_service":         "Translation Service",
	"ui_hint_google":           "Free and no API key; may throttle requests on large batches.",
	"ui_hint_deepl":            "High-fidelity neural translation; requires an API key (Free or Pro).",
	"ui_hint_ollama":           "Private, offline local LLM (e.g. llama3, qwen2.5, gemma2).",
	"ui_label_deepl_key":       "DeepL API Key",
	"ui_deepl_key_placeholder": "Empty: uses DEEPL_API_KEY from the environment",
	"ui_label_source":          "Source Language",
	"ui_label_target":          "Target Language",
	"ui_src_ja":                "Japanese (detected)",
	"ui_src_en":                "English",
	"ui_src_auto":              "Detect automatically",
	"ui_tgt_pt":                "Portuguese (Brazil)",
	"ui_tgt_es":                "Spanish",
	"ui_tgt_en":                "English",
	"ui_only_changed":          "Only what changed",
	"ui_use_proxies":           "Rotate proxies if Google throttles connections",
	"ui_advanced":              "▸ Advanced Options",
	"ui_label_extractor":       "Extractor",
	"ui_extractor_auto":        "Automatic (detected)",
	"ui_backup_check":          "Create a local backup before changing files",
	"ui_output_dir":            "Output Folder (optional)",
	"ui_output_placeholder":    "Default: in the game folder itself",
	"ui_estimate_title":        "Pre-flight Estimate",
	"ui_est_to_translate":      "texts to translate",
	"ui_est_cache":             "%d of %d already in memory (%d%%)",
	"ui_stat_files":            "Files",
	"ui_stat_time":             "Approx. time",
	"ui_stat_cost":             "Cost",
	"ui_stat_backup":           "Backup",
	"ui_files_of":              "%d of %d",
	"ui_time_note":             "Time is an estimate: free providers may trigger a temporary rate limit.",
	"ui_start_hint":            "ctrl+enter or click to start",
	"ui_btn_pause":             "Pause",
	"ui_btn_resume":            "Resume",
	"ui_btn_cancel":            "Cancel",
	"ui_btn_open_review":       "Review Translations (Optional)",
	"ui_confirm_cancel":        "Do you really want to cancel the translation process?",
	"ui_progress":              "%d of %d segments · %d from memory",
	"ui_files_in_progress":     "Files in Progress",
	"ui_col_file":              "FILE",
	"ui_col_status":            "STATUS",
	"ui_col_message":           "MESSAGE",
	"ui_col_progress":          "PROGRESS",
	"ui_review_title":          "Review translations",
	"ui_review_sub":            "Optional · Nothing goes to the game until you finish.",
	"ui_review_finish":         "Finish Review and Save ›",
	"ui_review_search":         "/ search by text or file…",
	"ui_review_use_suggestion": "Use suggestion",
	"ui_review_lock":           "Lock this segment so machines never overwrite it",
	"ui_review_save":           "Save Change",
	"ui_review_saved":          "Segment successfully updated in memory!",
	"ui_result_title":          "Game translated successfully",
	"ui_result_where":          "Where everything ended up",
	"ui_result_cli":            "Equivalent command for terminal automation:",
	"ui_btn_new_game":          "Translate Another Game",
	"ui_btn_verify":            "Verify Integrity",
	"ui_btn_translate":         "▶ Translate texts",
	"ui_btn_open_game":         "Open Game Folder",
	"ui_kbd_move":              "move",
	"ui_kbd_choose":            "choose",
	"ui_kbd_translate":         "translate",
	"ui_kbd_back":              "back",
}

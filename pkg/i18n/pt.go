package i18n

var pt = map[string]string{
	"usage": `Slime RPG (Go Edition) v%s

USO:
  slime [comando] [opções]

COMANDOS:
  translate       Extrai, traduz e reinjeta textos no jogo (padrão)
  verify          Confere a pasta traduzida contra o .slime/meta.json
  adopt           Registra o metadado de um jogo já traduzido
  filenames       Traduz os nomes dos arquivos de uma pasta
  tm              Memória de Tradução: contagem, export/import TMX e glossário
  list            Lista extratores, tradutores e idiomas suportados
  lang            Mostra ou grava o idioma da interface (pt, en)
  version         Exibe informações detalhadas de versão e build

OPÇÕES DE TRADUÇÃO:
  -i, --input        Pasta raiz do jogo ou arquivo (padrão: pasta atual)
  -o, --output       Pasta de destino (padrão: a própria entrada)
  -e, --extractor    Força o extrator: rpgmaker, wolfrpg, csv, json (padrão: detecção automática)
  --opt chave=valor  Opção do extrator; repita para várias (ver OPÇÕES DOS EXTRATORES)
  -t, --translator   Provedor: google, deepl, ollama (padrão: google)
  -s, --source       Idioma de origem (padrão: ja)
  -d, --target       Idioma de destino (padrão: pt)
  --synopsis         Sinopse do jogo para dar contexto ao Ollama
  --model            Modelo do Ollama (padrão: $OLLAMA_MODEL ou qwen2.5)
  --no-backup        Não criar o backup <pasta>-<origem>
  --memory-only      Aplica só a Memória de Tradução, sem rede (alias: --offline)
  --switch-engine    Ignora traduções automáticas de outros provedores na memória
  --incremental      Traduz só arquivos novos ou alterados (padrão: true)
  --no-proxy         Não usar o pool de proxies quando o Google bloquear o IP
  --dry-run          Calcula a estimativa pré-voo sem alterar arquivos
  --review           Pausa antes de gravar para inspecionar a pasta de processamento (só em terminal)
  --json             Emite eventos em JSON Lines para automação
  --tui / --gui      Força a interface de terminal ou desktop
  --lang pt|en       Idioma da interface nesta execução (ou SLIME_LANG); 'slime lang en' grava a preferência
  -h, --help         Exibe esta mensagem

OPÇÕES DOS EXTRATORES (--opt chave=valor):
  csv.columns=value,text     Colunas a traduzir, separadas por vírgula (vazio: automático)
  csv.delimiter=;            Delimitador do CSV; use \t para tab (vazio: automático)
  json.keys=text,name        Chaves JSON a traduzir; * ou all para todas (vazio: padrão)
  wolf.preserve_title=true   Mantém o título do Game.dat para não invalidar saves (padrão: true)
  wolf.target_font=Nome      Fonte gravada no Game.dat (vazio: mantém a do jogo)
  wolf.repack_mode=false     true também gera Data/Data.wolf (padrão: arquivos soltos em Data/)

MEMÓRIA DE TRADUÇÃO:
  slime tm                                              # total de entradas
  slime tm export --src ja --tgt pt --out f.tmx [--status reviewed,locked]
  slime tm import --file f.tmx [--engine human] [--status reviewed|locked|machine]
  slime tm glossary --src ja --tgt pt --source "ルイ" --target "Rui"
  slime tm glossary --src ja --tgt pt --list
  slime tm repair [--db memory.db] [--apply]            # dry-run por padrão; --apply faz backup e grava
  slime tm migrate-legacy --dir cache [--db memory.db]  # importa cache/<engine>/cache_<src>_<tgt>.json

NOMES DE ARQUIVO:
  slime filenames -i <pasta> [-o <saída>] [-t google -s ja -d pt] [--keep-original]

VERIFICAÇÃO E ADOÇÃO:
  slime verify <pasta-traduzida> [--original <pasta>]
  slime adopt <pasta-traduzida> --original <pasta> -e <extrator> [-s en -d pt]

EXEMPLOS:
  slime                                   # TUI interativa na pasta atual
  slime --gui                             # Janela desktop
  slime translate -i ./MeuJogo -d pt      # Tradução headless
  slime -i ./MeuJogo -t deepl --no-backup
  slime -i ./textos -e json --opt json.keys=text,name
  slime -i ./loc.csv --opt csv.delimiter=';' --opt csv.columns=Text
`,

	"unknown_command":        "Comando desconhecido: %s\n\n",
	"err_gui_start":          "Erro ao iniciar interface desktop: %v\n",
	"err_tui_start":          "Erro ao iniciar TUI: %v\n",
	"err_option_format":      "use chave=valor (recebido %q)",
	"err_deepl_key":          "defina DEEPL_API_KEY para usar o DeepL",
	"err_unknown_translator": "tradutor desconhecido: %s (use google, deepl ou ollama)",
	"err_detect":             "Erro ao detectar jogo em %s: %v\n",
	"err_estimate":           "Erro ao calcular estimativa: %v\n",
	"err_run":                "Erro durante a tradução: %v\n",
	"err_memory_resolve":     "erro ao resolver banco de memória: %v",
	"err_memory_open":        "erro ao abrir banco de memória %s: %v",

	"detected_game":    "Jogo detectado: %s (%s)\n",
	"detected_data":    "Pasta de dados: %s (%d arquivos)\n",
	"detected_service": "Serviço: %s | Idiomas: %s → %s\n",
	"estimate_title":   "\n--- Estimativa Pré-Voo ---",
	"estimate_files":   "Arquivos: %d (a processar: %d) | Segmentos: %d (na memória: %d)\n",
	"estimate_time":    "Tempo estimado: %s | Backup: %s\n",
	"review_prompt":    "\nRevise os arquivos traduzidos em: %s\nEnter para gravar no jogo, 'n' para abortar: ",

	"event_phase": "[FASE] %s: %s\n",
	"event_warn":  "[AVISO] %s: %s\n",
	"event_end":   "[FIM] %d concluído(s), %d parcial(is), %d com erro em %s\n",

	"summary_failed":  "--- Arquivos com falha de processamento ---",
	"summary_partial": "--- Arquivos com tradução parcial (<100%) ---",
	"summary_line":    "Segmentos: %d (memória: %d, novos: %d) | Backup: %s | Metadado: %s\n",

	"verify_no_meta": "Sem metadado em %s. Use 'slime adopt' para registrar o jogo.\n",
	"verify_summary": "Resumo: %s\n",

	"adopt_usage":    "adopt exige a pasta traduzida, --original e -e/--extractor (rpgmaker, wolfrpg, csv, json).",
	"adopt_no_dir":   "Pasta não existe: %s\n",
	"adopt_saved":    "Metadado gravado em %s\n  %d arquivo(s) registrados: %s\n",
	"flag_original":  "Pasta com os arquivos originais",
	"flag_extractor": "Extrator: rpgmaker, wolfrpg, csv, json",
	"flag_source":    "Idioma de origem",
	"flag_target":    "Idioma de destino",

	"tm_count":           "Memória de Tradução (%s): %d entradas registradas.\n",
	"tm_export_need":     "tm export exige --src, --tgt e --out",
	"tm_export_done":     "%d segmentos exportados para %s\n",
	"tm_import_need":     "tm import exige --file e --status reviewed, locked ou machine",
	"tm_import_done":     "%d segmentos importados (status=%s), %d ignorados (incompletos)\n",
	"tm_gloss_need":      "tm glossary exige --src e --tgt",
	"tm_gloss_terms":     "Use --source e --target para travar um termo, ou --list para listar.",
	"tm_locked":          "Travado: %q -> %q\n",
	"tm_unknown_sub":     "Subcomando desconhecido: tm %s (use export, import ou glossary)\n",
	"tm_verb_would":      "seriam afetadas",
	"tm_verb_applied":    "afetadas",
	"tm_repair_db":       "Banco: %s\n",
	"tm_repair_case":     "Caixa de códigos de controle restaurada",
	"tm_repair_name":     "Tags \\NAME[...] normalizadas",
	"tm_repair_xtok":     "Linhas com __XTOK_ removidas",
	"tm_repair_engine":   "Linhas de engines de teste removidas",
	"tm_repair_rows":     "%s: %d linhas %s\n",
	"tm_repair_prot":     "Linhas reviewed/locked %s: %d\n",
	"tm_repair_backup":   "Backup: %s\n",
	"tm_repair_dry":      "Dry-run: nada foi alterado. Use --apply para gravar.",
	"tm_migrate_need":    "tm migrate-legacy exige --dir",
	"tm_migrate_done":    "%d arquivos, %d entradas importadas em %s\n",
	"tm_migrate_skip":    "Puladas (tradução nula): %d; com source == target: %d\n",
	"filenames_kept":     "Originais mantidos: %d\n",
	"list_extractors":    "Extratores disponíveis:",
	"list_translators":   "Tradutores disponíveis:",
	"list_languages":     "Idiomas suportados:",
	"extractor_rpgmaker": "rpgmaker (RPG Maker MV/MZ/VX Ace/XP)",
	"extractor_wolfrpg":  "wolfrpg (Wolf RPG)",
	"extractor_csv":      "csv (CSV)",
	"extractor_json":     "json (Json)",
	"lang_pt":            "Português",
	"lang_en":            "Inglês",
	"lang_es":            "Espanhol",
	"lang_fr":            "Francês",
	"lang_it":            "Italiano",
	"lang_ja":            "Japonês",
	"lang_auto":          "Automático (Detectar)",

	"lang_current":   "Idioma da interface: %s (%s)\n",
	"lang_saved":     "Idioma da interface gravado: %s. Reinicie o aplicativo para aplicar.\n",
	"lang_invalid":   "Idioma não suportado: %s (use pt ou en)\n",
	"lang_save_fail": "Não foi possível gravar a preferência: %v\n",

	"ui_title":                 "Slime RPG",
	"ui_label_language":        "Idioma da interface",
	"ui_language_changed":      "Idioma alterado para %s. Reinicie o aplicativo para aplicar.",
	"ui_step_game":             "1 Jogo",
	"ui_step_configure":        "2 Configurar",
	"ui_step_translate":        "3 Tradução",
	"ui_step_result":           "4 Resultado",
	"ui_which_game":            "Qual jogo vamos traduzir?",
	"ui_which_game_sub":        "O Slime procurou na pasta atual, no clipboard e nos jogos recentes.",
	"ui_detected_here":         "Jogo detectado aqui",
	"ui_use_game":              "Usar este jogo ›",
	"ui_browse_folders":        "Navegar nas pastas…",
	"ui_drop_hint":             "ou arraste a pasta do jogo para cá",
	"ui_recent_games":          "JOGOS RECENTES",
	"ui_tip":                   "Dica: <code>slime translate -i &lt;pasta&gt;</code> faz o mesmo sem perguntas no terminal.",
	"ui_lang_detected":         "%s detectado",
	"ui_lang_japanese":         "Japonês",
	"ui_label_service":         "Serviço de Tradução",
	"ui_hint_google":           "Grátis e sem chave de API; pode limitar requisições em grandes lotes.",
	"ui_hint_deepl":            "Traduções neurais de alta fidelidade; requer chave da API (Free ou Pro).",
	"ui_hint_ollama":           "LLM local privada e offline (ex: llama3, qwen2.5, gemma2).",
	"ui_label_deepl_key":       "Chave da API DeepL",
	"ui_deepl_key_placeholder": "Vazio: usa DEEPL_API_KEY do ambiente",
	"ui_label_source":          "Idioma de Origem",
	"ui_label_target":          "Idioma de Destino",
	"ui_src_ja":                "Japonês (detectado)",
	"ui_src_en":                "Inglês",
	"ui_src_auto":              "Detectar automaticamente",
	"ui_tgt_pt":                "Português (Brasil)",
	"ui_tgt_es":                "Espanhol",
	"ui_tgt_en":                "Inglês",
	"ui_only_changed":          "Só o que mudou",
	"ui_use_proxies":           "Usar rotação de proxies se o Google limitar conexões",
	"ui_advanced":              "▸ Opções Avançadas",
	"ui_label_extractor":       "Extrator",
	"ui_extractor_auto":        "Automático (detectado)",
	"ui_backup_check":          "Criar backup local antes de alterar arquivos",
	"ui_output_dir":            "Pasta de Saída (opcional)",
	"ui_output_placeholder":    "Padrão: na própria pasta do jogo",
	"ui_estimate_title":        "Estimativa Pré-Voo",
	"ui_est_to_translate":      "textos para traduzir",
	"ui_est_cache":             "%d de %d já na memória (%d%%)",
	"ui_stat_files":            "Arquivos",
	"ui_stat_time":             "Tempo aprox.",
	"ui_stat_cost":             "Custo",
	"ui_stat_backup":           "Backup",
	"ui_files_of":              "%d de %d",
	"ui_time_note":             "O tempo é uma estimativa: provedores gratuitos podem acionar rate limit temporário.",
	"ui_start_hint":            "ctrl+enter ou clique para iniciar",
	"ui_btn_pause":             "Pausar",
	"ui_btn_resume":            "Retomar",
	"ui_btn_cancel":            "Cancelar",
	"ui_btn_open_review":       "Revisar Traduções (Opcional)",
	"ui_confirm_cancel":        "Deseja realmente cancelar o processo de tradução?",
	"ui_progress":              "%d de %d segmentos · %d da memória",
	"ui_files_in_progress":     "Arquivos em Processamento",
	"ui_col_file":              "ARQUIVO",
	"ui_col_status":            "STATUS",
	"ui_col_message":           "MENSAGEM",
	"ui_col_progress":          "PROGRESSO",
	"ui_review_title":          "Revisar traduções",
	"ui_review_sub":            "Opcional · Nada vai para o jogo até você concluir.",
	"ui_review_finish":         "Concluir Revisão e Salvar ›",
	"ui_review_search":         "/ buscar por texto ou arquivo…",
	"ui_review_use_suggestion": "Usar sugestão",
	"ui_review_lock":           "Bloquear este segmento para nunca ser sobrescrito por máquina",
	"ui_review_save":           "Salvar Alteração",
	"ui_review_saved":          "Segmento atualizado com sucesso na memória!",
	"ui_result_title":          "Jogo traduzido com sucesso",
	"ui_result_where":          "Onde ficou cada coisa",
	"ui_result_cli":            "Comando equivalente para automação via terminal:",
	"ui_btn_new_game":          "Traduzir Outro Jogo",
	"ui_btn_verify":            "Verificar Integridade",
	"ui_btn_translate":         "▶ Traduzir textos",
	"ui_btn_open_game":         "Abrir Pasta do Jogo",
	"ui_kbd_move":              "mover",
	"ui_kbd_choose":            "escolher",
	"ui_kbd_translate":         "traduzir",
	"ui_kbd_back":              "voltar",
	"tui_quit":                 "Slime RPG finalizado.\n",
	"tui_step_game":            "Jogo",
	"tui_step_config":          "Configurar",
	"tui_step_translate":       "Tradução",
	"tui_step_result":          "Resultado",
	"tui_open_title":           "Qual jogo vamos traduzir?",
	"tui_open_sub":             "O Slime procurou um jogo na pasta atual.",
	"tui_open_box":             "Escolha uma pasta",
	"tui_open_detected":        "Jogo detectado aqui   %s   %s",
	"tui_open_type_path":       "Digitar caminho…",
	"tui_open_path_prompt":     "Caminho do jogo: ",
	"tui_open_tip":             "Dica: slime translate -i <pasta> faz o mesmo sem perguntas.",
	"tui_no_game_found":        "Nenhum jogo encontrado em %s",
	"tui_no_game_detected":     "nenhum jogo detectado em %s",
	"tui_cfg_title":            "Configurar",
	"tui_cfg_none":             "(nenhum jogo)",
	"tui_cfg_info":             "%s · %s · %d arquivos",
	"tui_cfg_service":          "Serviço",
	"tui_hint_memory":          "Usa só traduções já salvas; nada vai para a internet.",
	"tui_cfg_source":           "Idioma de origem (s)",
	"tui_cfg_target":           "Idioma de destino (d)",
	"tui_cfg_incremental":      "Só o que mudou (espaço)",
	"tui_cfg_proxies":          "Usar proxies se o Google bloquear o IP (x)",
	"tui_cfg_backup":           "Backup ligado · saída na própria pasta",
	"tui_provider_memory":      "Só memória",
	"tui_filter_all":           "Todos",
	"tui_filter_partial":       "Parciais",
	"tui_ollama_model_label":   "Modelo do Ollama",
	"tui_ollama_model_help":    "Vazio usa a variável OLLAMA_MODEL.",
	"tui_ollama_host_label":    "Endereço do Ollama",
	"tui_ollama_host_help":     "Vazio usa OLLAMA_HOST ou o endereço local.",
	"tui_est_title":            "Estimativa",
	"tui_est_calc":             "Calculando…",
	"tui_est_fail":             "Não foi possível estimar:",
	"tui_est_files":            "Arquivos  %d de %d",
	"tui_est_backup":           "Backup    ",
	"tui_trans_title":          "Traduzindo %s",
	"tui_trans_phase":          "Fase: %s",
	"tui_trans_paused":         "%s (pausado)",
	"tui_trans_progress":       "%d de %d segmentos · %d da memória · %d traduzidos agora · %d de %d arquivos",
	"tui_trans_files":          "Arquivos · %d",
	"tui_trans_more":           "%d outros arquivos",
	"tui_result_ok":            "Jogo traduzido",
	"tui_result_fail":          "Tradução não concluída",
	"tui_result_done":          "%s foi processado em %s.",
	"tui_result_counts":        "%s %d completos   %s %d parciais   %s %d com erro   %s %d novos na memória   %s %d da memória\n\n",
	"tui_tag_partial":          "parcial",
	"tui_tag_error":            "erro   ",
	"tui_where_backup":         "Original (backup)",
	"tui_where_log":            "Registro",
	"tui_result_exit":          "Código de saída %d · na CLI, use --json para automação\n",
	"tui_review_sub":           "correções vão para a Memória de Tradução",
	"tui_review_empty":         "Nenhum segmento neste filtro.",
	"tui_review_file":          "Arquivo:   ",
	"tui_review_source":        "Origem:    ",
	"tui_review_target":        "Tradução:  ",
	"tui_review_saved":         "Correção salva na memória; traduza de novo para aplicá-la ao jogo.",
	"tui_foot_path":            "  digite ou cole o caminho · enter abrir · esc cancelar",
	"tui_foot_open":            "  ↑↓ mover · enter escolher · q sair",
	"tui_foot_config":          "  ↑↓ campo · ←→ alterar · espaço marcar · enter traduzir · esc voltar",
	"tui_foot_translating":     "  p pausar/retomar · c cancelar · ctrl+c sair",
	"tui_foot_edit":            "  digite a tradução · enter salvar · esc cancelar",
	"tui_foot_review":          "  ↑↓ segmento · tab filtro · e editar · b bloquear · esc/enter voltar ao resultado",
	"tui_foot_result":          "  r revisar · n outro jogo · q sair",
	"tui_foot_default":         "  q sair",
	"tui_err_deepl_key":        "informe a chave da API do DeepL ou defina DEEPL_API_KEY",
	"tui_err_ollama_model":     "informe o modelo do Ollama ou defina OLLAMA_MODEL",
	"tui_err_unknown_provider": "provedor desconhecido: %q",
	"tui_err_memory_path":      "erro ao resolver banco de memória",
	"tui_err_memory_open":      "erro ao abrir banco de memória %s",
	"tui_run_canceled":         "Tradução cancelada: nada foi gravado no jogo.",
	"tui_run_rate_limited":     "O provedor limitou as requisições (HTTP 429): ative os proxies ou use uma VPN e rode de novo.",
	"tui_run_auth":             "O provedor recusou a chave de API (HTTP 401/403).",
	"tui_run_unavailable":      "O provedor está indisponível: %s",
}

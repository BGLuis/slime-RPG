// Slime RPG - Desktop UI (MyGo IPC Controller)
(function () {
  'use strict';

  // Fallback Mock se executado fora do MyGo (ex: browser dev)
  const isMyGo = typeof window.mygo !== 'undefined';
  const mygoCall = async (method, ...args) => {
    if (isMyGo && window.mygo.call) {
      return await window.mygo.call(method, ...args);
    }
    console.warn(`[MyGo Mock] Chamada: ${method}`, args);
    return null;
  };

  // Tabela de strings do idioma ativo, preenchida por loadLanguage()
  let strings = {};
  const t = (key, ...args) => {
    let i = 0;
    return (strings[key] || key).replace(/%[sd%]/g, m => (m === '%%' ? '%' : String(args[i++])));
  };

  function applyI18n() {
    document.querySelectorAll('[data-i18n]').forEach(el => { el.textContent = t(el.dataset.i18n); });
    document.querySelectorAll('[data-i18n-html]').forEach(el => { el.innerHTML = t(el.dataset.i18nHtml); });
    document.querySelectorAll('[data-i18n-placeholder]').forEach(el => { el.placeholder = t(el.dataset.i18nPlaceholder); });
    const sel = document.getElementById('uiLanguage');
    sel.title = t('ui_label_language');
    sel.setAttribute('aria-label', t('ui_label_language'));
    document.title = t('ui_title');
  }

  async function loadLanguage() {
    const info = await mygoCall('Slime.GetLanguage');
    if (!info) return;
    strings = info.strings || {};
    const sel = document.getElementById('uiLanguage');
    sel.replaceChildren(...info.options.map(o => new Option(o.name, o.code, false, o.code === info.current)));
    sel.addEventListener('change', async () => {
      const message = await mygoCall('Slime.SetLanguage', sel.value);
      if (message) alert(message);
    });
    applyI18n();
  }

  const appState = {
    step: 1, // 1: Jogo, 2: Configurar, 3: Traduzir, 3.5: Revisar, 4: Resultado
    gameDir: '.',
    detectedGame: null,
    provider: 'google',
    sourceLang: 'ja',
    targetLang: 'pt',
    incremental: true,
    useProxies: false,
    extractor: '',
    extractorOptions: {},
    apiKey: '',
    paused: false,
    snapshot: null,
    reviewFilter: 'all',
    reviewSegments: [],
    selectedSegmentId: null
  };

  // Elementos do DOM
  const dom = {
    stepperItems: document.querySelectorAll('.step-item'),
    views: {
      step1: document.getElementById('viewStep1'),
      step2: document.getElementById('viewStep2'),
      step3: document.getElementById('viewStep3'),
      review: document.getElementById('viewStepReview'),
      step4: document.getElementById('viewStep4')
    },
    // Step 1
    detectedTitle: document.getElementById('detectedTitle'),
    detectedEngine: document.getElementById('detectedEngine'),
    detectedPath: document.getElementById('detectedPath'),
    detectedLang: document.getElementById('detectedLang'),
    btnChooseDetected: document.getElementById('btnChooseDetected'),
    btnBrowseFolder: document.getElementById('btnBrowseFolder'),
    // Step 2
    providerPicker: document.getElementById('providerPicker'),
    providerHint: document.getElementById('providerHint'),
    apiKeyGroup: document.getElementById('apiKeyGroup'),
    apiKeyInput: document.getElementById('apiKeyInput'),
    extractorSelect: document.getElementById('extractorSelect'),
    extractorOptions: document.getElementById('extractorOptions'),
    sourceLang: document.getElementById('sourceLang'),
    targetLang: document.getElementById('targetLang'),
    checkIncremental: document.getElementById('checkIncremental'),
    checkProxies: document.getElementById('checkProxies'),
    estToTranslate: document.getElementById('estToTranslate'),
    estCacheProgress: document.getElementById('estCacheProgress'),
    estCacheLabel: document.getElementById('estCacheLabel'),
    estFiles: document.getElementById('estFiles'),
    estTime: document.getElementById('estTime'),
    estCost: document.getElementById('estCost'),
    estBackup: document.getElementById('estBackup'),
    btnStartTranslate: document.getElementById('btnStartTranslate'),
    // Step 3
    runProgressBar: document.getElementById('runProgressBar'),
    runProgressText: document.getElementById('runProgressText'),
    runProgressPercent: document.getElementById('runProgressPercent'),
    rateLimitBanner: document.getElementById('rateLimitBanner'),
    btnPauseResume: document.getElementById('btnPauseResume'),
    btnCancelJob: document.getElementById('btnCancelJob'),
    btnOpenReview: document.getElementById('btnOpenReview'),
    // Step 3.5
    reviewFilterChips: document.querySelectorAll('#reviewFilterChips .chip-btn'),
    btnFinishReview: document.getElementById('btnFinishReview'),
    btnApplySuggestion: document.getElementById('btnApplySuggestion'),
    btnSaveSegment: document.getElementById('btnSaveSegment'),
    reviewDetailSource: document.getElementById('reviewDetailSource'),
    reviewDetailTarget: document.getElementById('reviewDetailTarget'),
    checkLockSegment: document.getElementById('checkLockSegment'),
    // Step 4
    btnNewGame: document.getElementById('btnNewGame'),
    btnOpenGameFolder: document.getElementById('btnOpenGameFolder')
  };

  // Navegação entre Telas
  function goToStep(step) {
    appState.step = step;

    // Atualiza Stepper Nav
    dom.stepperItems.forEach(el => {
      const s = parseFloat(el.getAttribute('data-step'));
      el.classList.remove('active', 'done');
      if (s === Math.floor(step)) {
        el.classList.add('active');
      } else if (s < Math.floor(step)) {
        el.classList.add('done');
      }
    });

    // Alterna visualização de painéis
    Object.values(dom.views).forEach(v => v.classList.remove('active'));
    if (step === 1) dom.views.step1.classList.add('active');
    else if (step === 2) dom.views.step2.classList.add('active');
    else if (step === 3) dom.views.step3.classList.add('active');
    else if (step === 3.5) dom.views.review.classList.add('active');
    else if (step === 4) dom.views.step4.classList.add('active');
  }

  // Inicialização e Detecção
  async function init() {
    try {
      await loadLanguage();
    } catch (e) {
      console.error('Falha ao carregar idioma:', e);
    }
    try {
      const det = await mygoCall('Slime.DetectGame', appState.gameDir);
      if (det) {
        appState.detectedGame = det;
        dom.detectedTitle.textContent = `› ${det.gameName || t('ui_detected_here')}`;
        dom.detectedEngine.textContent = det.engine;
        dom.detectedPath.textContent = det.dataDir || 'www/data';
        dom.detectedLang.textContent = t('ui_lang_detected', det.probableLang === 'ja' ? t('ui_lang_japanese') : det.probableLang);
      }
    } catch (e) {
      console.error('Falha na detecção inicial:', e);
    }

    setupEventListeners();
    setupMyGoIPCListeners();
  }

  // Opções de extrator e chave de API enviadas junto de cada chamada de job
  function extraJobOptions() {
    return {
      extractor: appState.extractor,
      extractorOptions: appState.extractorOptions,
      apiKey: appState.apiKey
    };
  }

  // Desenha o formulário de opções a partir do schema do extrator (forçado ou detectado)
  async function renderExtractorOptions() {
    const name = appState.extractor || (appState.detectedGame && appState.detectedGame.engine) || '';
    appState.extractorOptions = {};
    dom.extractorOptions.replaceChildren();
    const schema = await mygoCall('Slime.ExtractorSchema', name);
    ((schema && schema.fields) || []).forEach(field => {
      const group = document.createElement('div');
      group.className = 'form-group';
      const label = document.createElement('label');
      label.textContent = field.label;
      const input = document.createElement('input');
      input.type = 'text';
      input.className = 'form-input';
      input.value = field.default || '';
      input.addEventListener('change', () => {
        appState.extractorOptions[field.key] = input.value;
        refreshEstimate();
      });
      const hint = document.createElement('span');
      hint.className = 'field-hint';
      hint.textContent = field.help;
      group.append(label, input, hint);
      dom.extractorOptions.appendChild(group);
    });
  }

  // Atualiza Estimativa Pré-Voo
  async function refreshEstimate() {
    try {
      const est = await mygoCall('Slime.Estimate', {
        directory: appState.gameDir,
        provider: appState.provider,
        sourceLang: appState.sourceLang,
        targetLang: appState.targetLang,
        incremental: appState.incremental,
        ...extraJobOptions()
      });

      if (est) {
        dom.estToTranslate.textContent = est.toTranslate;
        dom.estCacheProgress.style.width = `${est.cacheHitPercent}%`;
        dom.estCacheLabel.textContent = t('ui_est_cache', est.cachedSegments, est.totalSegments, est.cacheHitPercent);
        dom.estFiles.textContent = t('ui_files_of', est.changedFiles, est.totalFiles);
        dom.estTime.textContent = est.estimatedTimeLabel;
        dom.estCost.textContent = est.costLabel;
        dom.estBackup.textContent = est.backupPath;
      }
    } catch (e) {
      console.warn('Falha ao calcular estimativa:', e);
    }
  }

  // Event Listeners da UI
  function setupEventListeners() {
    // Step 1 -> Step 2
    dom.btnChooseDetected.addEventListener('click', async () => {
      goToStep(2);
      await renderExtractorOptions();
      refreshEstimate();
    });

    // Segmented Control de Provedores
    dom.providerPicker.querySelectorAll('.segment-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        dom.providerPicker.querySelectorAll('.segment-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        appState.provider = btn.getAttribute('data-provider');

        dom.providerHint.textContent = t('ui_hint_' + appState.provider);
        dom.apiKeyGroup.style.display = appState.provider === 'deepl' ? '' : 'none';
        refreshEstimate();
      });
    });

    dom.apiKeyInput.addEventListener('change', (e) => {
      appState.apiKey = e.target.value;
      refreshEstimate();
    });

    dom.extractorSelect.addEventListener('change', async (e) => {
      appState.extractor = e.target.value;
      await renderExtractorOptions();
      refreshEstimate();
    });

    // Checkboxes
    dom.checkIncremental.addEventListener('change', (e) => {
      appState.incremental = e.target.checked;
      refreshEstimate();
    });

    // Iniciar Tradução (Step 2 -> Step 3)
    dom.btnStartTranslate.addEventListener('click', async () => {
      goToStep(3);
      try {
        await mygoCall('Slime.StartJob', {
          directory: appState.gameDir,
          provider: appState.provider,
          sourceLang: appState.sourceLang,
          targetLang: appState.targetLang,
          incremental: appState.incremental,
          ...extraJobOptions()
        });
      } catch (e) {
        console.error('Erro ao iniciar job:', e);
      }
    });

    // Step 3 Ações
    dom.btnPauseResume.addEventListener('click', async () => {
      if (appState.paused) {
        await mygoCall('Slime.ResumeJob');
        appState.paused = false;
        dom.btnPauseResume.textContent = t('ui_btn_pause');
      } else {
        await mygoCall('Slime.PauseJob');
        appState.paused = true;
        dom.btnPauseResume.textContent = t('ui_btn_resume');
      }
    });

    dom.btnCancelJob.addEventListener('click', async () => {
      if (confirm(t('ui_confirm_cancel'))) {
        await mygoCall('Slime.CancelJob');
        goToStep(2);
      }
    });

    dom.btnOpenReview.addEventListener('click', () => {
      goToStep(3.5);
      loadReviewSegments();
    });

    // Step 3.5 Revisão
    dom.btnFinishReview.addEventListener('click', () => {
      goToStep(4);
    });

    dom.btnApplySuggestion.addEventListener('click', () => {
      dom.reviewDetailTarget.value = '\\N[1] recebeu \\V[12] moedas de ouro!';
    });

    dom.btnSaveSegment.addEventListener('click', async () => {
      const text = dom.reviewDetailTarget.value;
      const locked = dom.checkLockSegment.checked;
      await mygoCall('Slime.ApplyCorrection', 'seg1', text, locked);
      alert(t('ui_review_saved'));
    });

    // Step 4
    dom.btnNewGame.addEventListener('click', () => {
      goToStep(1);
    });

    dom.btnOpenGameFolder.addEventListener('click', () => {
      mygoCall('Slime.OpenFolder', appState.gameDir);
    });

    // Atalhos de Teclado
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        if (appState.step === 2) goToStep(1);
        else if (appState.step === 3.5) goToStep(3);
      } else if (e.key === 'Enter' && e.ctrlKey) {
        if (appState.step === 2) dom.btnStartTranslate.click();
        else if (appState.step === 3.5) dom.btnFinishReview.click();
      }
    });
  }

  // Ouvinte de Eventos IPC MyGo
  function setupMyGoIPCListeners() {
    if (!isMyGo || !window.mygo.on) return;

    window.mygo.on('job:progress', (snap) => {
      appState.snapshot = snap;
      updateRunningUI(snap);
    });

    window.mygo.on('job:phase', (phase) => {
      console.log('Fase alterada:', phase);
    });
  }

  function updateRunningUI(snap) {
    if (!snap) return;

    const percent = snap.TotalSegments > 0 ? Math.round((snap.ProcessedSegments * 100) / snap.TotalSegments) : 0;
    dom.runProgressBar.style.width = `${percent}%`;
    dom.runProgressPercent.textContent = `${percent}%`;
    dom.runProgressText.textContent = t('ui_progress', snap.ProcessedSegments, snap.TotalSegments, snap.CachedSegments);

    if (snap.Done) {
      setTimeout(() => goToStep(4), 1000);
    }
  }

  async function loadReviewSegments() {
    try {
      const items = await mygoCall('Slime.GetSegmentsForReview', appState.reviewFilter);
      if (items && items.length > 0) {
        appState.reviewSegments = items;
      }
    } catch (e) {
      console.warn('Erro ao carregar segmentos:', e);
    }
  }

  // Inicialização no DOM
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();

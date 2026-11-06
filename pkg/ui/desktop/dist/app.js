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

  const appState = {
    step: 1, // 1: Jogo, 2: Configurar, 3: Traduzir, 3.5: Revisar, 4: Resultado
    gameDir: '.',
    detectedGame: null,
    provider: 'google',
    sourceLang: 'ja',
    targetLang: 'pt',
    incremental: true,
    useProxies: false,
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
      const det = await mygoCall('Slime.DetectGame', appState.gameDir);
      if (det) {
        appState.detectedGame = det;
        dom.detectedTitle.textContent = `› ${det.gameName || 'Jogo detectado aqui'}`;
        dom.detectedEngine.textContent = det.engine;
        dom.detectedPath.textContent = det.dataDir || 'www/data';
        dom.detectedLang.textContent = `${det.probableLang === 'ja' ? 'Japonês' : det.probableLang} detectado`;
      }
    } catch (e) {
      console.error('Falha na detecção inicial:', e);
    }

    setupEventListeners();
    setupMyGoIPCListeners();
  }

  // Atualiza Estimativa Pré-Voo
  async function refreshEstimate() {
    try {
      const est = await mygoCall('Slime.Estimate', {
        directory: appState.gameDir,
        provider: appState.provider,
        sourceLang: appState.sourceLang,
        targetLang: appState.targetLang,
        incremental: appState.incremental
      });

      if (est) {
        dom.estToTranslate.textContent = est.toTranslate;
        dom.estCacheProgress.style.width = `${est.cacheHitPercent}%`;
        dom.estCacheLabel.textContent = `${est.cachedSegments} de ${est.totalSegments} já na memória (${est.cacheHitPercent}%)`;
        dom.estFiles.textContent = `${est.changedFiles} de ${est.totalFiles}`;
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
    dom.btnChooseDetected.addEventListener('click', () => {
      goToStep(2);
      refreshEstimate();
    });

    // Segmented Control de Provedores
    dom.providerPicker.querySelectorAll('.segment-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        dom.providerPicker.querySelectorAll('.segment-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        appState.provider = btn.getAttribute('data-provider');

        if (appState.provider === 'google') {
          dom.providerHint.textContent = 'Grátis e sem chave de API; pode limitar requisições em grandes lotes.';
        } else if (appState.provider === 'deepl') {
          dom.providerHint.textContent = 'Traduções neurais de alta fidelidade; requer chave da API (Free ou Pro).';
        } else if (appState.provider === 'ollama') {
          dom.providerHint.textContent = 'LLM local privada e offline (ex: llama3, qwen2.5, gemma2).';
        }
        refreshEstimate();
      });
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
          incremental: appState.incremental
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
        dom.btnPauseResume.textContent = 'Pausar';
      } else {
        await mygoCall('Slime.PauseJob');
        appState.paused = true;
        dom.btnPauseResume.textContent = 'Retomar';
      }
    });

    dom.btnCancelJob.addEventListener('click', async () => {
      if (confirm('Deseja realmente cancelar o processo de tradução?')) {
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
      alert('Segmento atualizado com sucesso na memória!');
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
    dom.runProgressText.textContent = `${snap.ProcessedSegments} de ${snap.TotalSegments} segmentos · ${snap.CachedSegments} da memória`;

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

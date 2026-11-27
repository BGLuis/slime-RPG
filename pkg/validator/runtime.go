package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// RuntimeResult consolida os resultados do smoke test em navegador headless.
type RuntimeResult struct {
	Ran              bool
	SkippedReason    string
	SceneReached     string
	TourScenesTested []string
	Issues           []Issue
}

// FindBrowser localiza um executável Chromium/Chrome compatível com CDP no sistema.
func FindBrowser(preferred string) (string, error) {
	if preferred != "" {
		if p, err := exec.LookPath(preferred); err == nil {
			return p, nil
		}
		if _, err := os.Stat(preferred); err == nil {
			return preferred, nil
		}
		return "", fmt.Errorf("navegador configurado não encontrado: %s", preferred)
	}
	if env := os.Getenv("SLIME_BROWSER"); env != "" {
		if p, err := exec.LookPath(env); err == nil {
			return p, nil
		}
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "msedge", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("nenhum Chromium/Chrome encontrado no PATH (defina SLIME_BROWSER para habilitar teste headless)")
}

func runRuntimeInternal(ctx context.Context, webRoot string, opts Options) (*RuntimeResult, error) {
	indexPath := filepath.Join(webRoot, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return &RuntimeResult{SkippedReason: "index.html não encontrado"}, nil
	}

	browserBin, err := FindBrowser(opts.BrowserPath)
	if err != nil {
		return &RuntimeResult{SkippedReason: err.Error()}, nil
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir servidor HTTP local: %w", err)
	}
	baseURL := "http://" + ln.Addr().String()

	var mu sync.Mutex
	var issues []Issue
	seen404 := make(map[string]bool)
	seenConsole := make(map[string]bool)

	addIssue := func(it Issue) {
		mu.Lock()
		defer mu.Unlock()
		issues = append(issues, it)
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqPath := strings.TrimPrefix(r.URL.Path, "/")
			if reqPath == "" {
				reqPath = "index.html"
			}
			resolved, ok := resolveCaseInsensitive(webRoot, reqPath)
			if !ok {
				http.NotFound(w, r)
				return
			}
			if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(resolved))); ct != "" {
				w.Header().Set("Content-Type", ct)
			} else if strings.HasSuffix(strings.ToLower(resolved), ".js") {
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			} else if strings.HasSuffix(strings.ToLower(resolved), ".json") {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
			}
			http.ServeFile(w, r, resolved)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserBin),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("enable-unsafe-swiftshader", true),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("mute-audio", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()

	runCtx, cancelRun := context.WithTimeout(allocCtx, timeout)
	defer cancelRun()

	tabCtx, cancelTab := chromedp.NewContext(runCtx, chromedp.WithListener(func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			msg := e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
				msg = e.ExceptionDetails.Exception.Description
			}
			file := strings.TrimPrefix(e.ExceptionDetails.URL, baseURL+"/")
			addIssue(Issue{
				Severity: SeverityError,
				Category: CategoryRuntime,
				File:     file,
				Message:  "Exceção JavaScript em runtime: " + firstLine(msg),
			})
		case *runtime.EventConsoleAPICalled:
			if e.Type != "error" {
				return
			}
			var parts []string
			for _, a := range e.Args {
				if len(a.Value) > 0 {
					var s string
					if json.Unmarshal(a.Value, &s) == nil {
						parts = append(parts, s)
					} else {
						parts = append(parts, string(a.Value))
					}
				} else if a.Description != "" {
					parts = append(parts, a.Description)
				}
			}
			text := strings.Join(parts, " ")
			if text == "" || seenConsole[text] {
				return
			}
			seenConsole[text] = true
			addIssue(Issue{
				Severity: SeverityError,
				Category: CategoryRuntime,
				File:     "runtime",
				Message:  "Console error: " + firstLine(text),
			})
		case *network.EventResponseReceived:
			if e.Response != nil && e.Response.Status >= 400 {
				relURL := strings.TrimPrefix(e.Response.URL, baseURL+"/")
				if relURL == "favicon.ico" || seen404[relURL] {
					return
				}
				seen404[relURL] = true
				addIssue(Issue{
					Severity: SeverityError,
					Category: CategoryAsset,
					File:     relURL,
					Message:  fmt.Sprintf("HTTP %d ao carregar recurso no boot/runtime: %s", e.Response.Status, relURL),
				})
			}
		}
	}))
	defer cancelTab()

	if _, err := chromedp.Call(tabCtx, network.Enable, network.EnableParams{}); err != nil {
		return nil, fmt.Errorf("falha ao habilitar monitoramento de rede no navegador: %w", err)
	}
	if err := chromedp.Do(tabCtx, chromedp.Navigate(baseURL+"/index.html")); err != nil {
		return nil, fmt.Errorf("falha ao navegar para o jogo em navegador headless: %w", err)
	}

	res := &RuntimeResult{Ran: true}
	var state gameStateSnapshot

	// Aguarda até alcançar tela não-boot ou aparecer erro no #errorPrinter do RPG Maker.
	deadline := time.Now().Add(timeout - 1500*time.Millisecond)
	if deadline.Before(time.Now()) {
		deadline = time.Now().Add(2 * time.Second)
	}
	stableTicks := 0
	for time.Now().Before(deadline) {
		if runCtx.Err() != nil {
			break
		}
		snapJSON, err := chromedp.Run(tabCtx, chromedp.Evaluate[string](`JSON.stringify({
			errName: document.getElementById("errorName")?.innerText || "",
			errMsg: document.getElementById("errorMessage")?.innerText || "",
			scene: window.SceneManager?._scene?.constructor?.name || "",
			ready: Boolean(window.DataManager && DataManager.isDatabaseLoaded && DataManager.isDatabaseLoaded())
		})`))
		if err == nil && snapJSON != "" {
			_ = json.Unmarshal([]byte(snapJSON), &state)
		}
		if state.Scene != "" {
			res.SceneReached = state.Scene
		}
		if state.ErrName != "" || state.ErrMsg != "" {
			addIssue(Issue{
				Severity: SeverityError,
				Category: CategoryRuntime,
				File:     state.ErrMsg,
				Location: state.Scene,
				Message:  fmt.Sprintf("Erro na tela do jogo (%s): %s", state.ErrName, state.ErrMsg),
			})
			break
		}
		if state.Ready && state.Scene != "" && state.Scene != "Scene_Boot" && state.Scene != "Scene_Splash" {
			stableTicks++
			if stableTicks >= 3 {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	if opts.SceneTour && state.ErrName == "" && state.ErrMsg == "" && runCtx.Err() == nil {
		res.TourScenesTested = runSceneTour(tabCtx, &state, addIssue)
	}

	mu.Lock()
	res.Issues = append(res.Issues, issues...)
	mu.Unlock()
	return res, nil
}

type gameStateSnapshot struct {
	ErrName string `json:"errName"`
	ErrMsg  string `json:"errMsg"`
	Scene   string `json:"scene"`
	Ready   bool   `json:"ready"`
}

// runSceneTour exercita o carregamento de todos os mapas e abre cenas comuns de menu
// dentro do mesmo contexto headless para capturar erros que não disparam no boot.
func runSceneTour(tabCtx context.Context, state *gameStateSnapshot, addIssue func(Issue)) []string {
	var tested []string

	// 1. Verifica carregamento e JSON.parse de todos os Map*.json registrados em $dataMapInfos.
	mapReportJSON, _ := chromedp.Run(tabCtx, chromedp.Evaluate[string](`(async () => {
		const broken = [];
		const infos = window.$dataMapInfos || [];
		for (let i = 1; i < infos.length; i++) {
			if (!infos[i]) continue;
			const file = "data/Map" + String(i).padStart(3, "0") + ".json";
			try {
				const r = await fetch(file);
				if (!r.ok) { broken.push(file + ": HTTP " + r.status); continue; }
				JSON.parse(await r.text());
			} catch (e) {
				broken.push(file + ": " + String(e));
			}
		}
		return JSON.stringify(broken);
	})()`, chromedp.EvalAwaitPromise))
	if mapReportJSON != "" {
		var brokenMaps []string
		if json.Unmarshal([]byte(mapReportJSON), &brokenMaps) == nil {
			for _, bm := range brokenMaps {
				addIssue(Issue{
					Severity: SeverityError,
					Category: CategoryRuntime,
					File:     bm,
					Message:  "Falha ao carregar mapa durante tour: " + bm,
				})
			}
		}
		tested = append(tested, "AllMapsJSON")
	}

	// 2. Inicia New Game em memória e tenta abrir cenas comuns do RPG Maker / plugins.
	_, _ = chromedp.Run(tabCtx, chromedp.Evaluate[chromedp.Void](`(() => {
		try {
			if (window.DataManager && DataManager.setupNewGame) {
				DataManager.setupNewGame();
				SceneManager.goto(Scene_Map);
			}
		} catch (e) {}
	})()`))
	time.Sleep(350 * time.Millisecond)

	sceneNames := []string{"Scene_Map", "Scene_Menu", "Scene_Item", "Scene_Skill", "Scene_Equip", "Scene_Status", "Scene_Options", "Scene_CustomMenu"}
	for _, sc := range sceneNames {
		script := fmt.Sprintf(`(() => {
			const Ctor = window[%q];
			if (typeof Ctor !== "function") return "skip";
			try {
				SceneManager.goto(Ctor);
				return "ok";
			} catch (e) {
				return "err:" + (e && e.message ? e.message : String(e));
			}
		})()`, sc)
		outcome, err := chromedp.Run(tabCtx, chromedp.Evaluate[string](script))
		if err != nil {
			continue
		}
		if outcome == "skip" {
			continue
		}
		tested = append(tested, sc)
		if strings.HasPrefix(outcome, "err:") {
			addIssue(Issue{
				Severity: SeverityError,
				Category: CategoryRuntime,
				Location: sc,
				Message:  "Erro ao abrir cena no tour: " + strings.TrimPrefix(outcome, "err:"),
			})
			break
		}
		time.Sleep(250 * time.Millisecond)
		errCheck, _ := chromedp.Run(tabCtx, chromedp.Evaluate[string](`JSON.stringify({
			errName: document.getElementById("errorName")?.innerText || "",
			errMsg: document.getElementById("errorMessage")?.innerText || ""
		})`))
		var ec gameStateSnapshot
		if json.Unmarshal([]byte(errCheck), &ec) == nil && (ec.ErrName != "" || ec.ErrMsg != "") {
			addIssue(Issue{
				Severity: SeverityError,
				Category: CategoryRuntime,
				File:     ec.ErrMsg,
				Location: sc,
				Message:  fmt.Sprintf("Erro na cena %s (%s): %s", sc, ec.ErrName, ec.ErrMsg),
			})
			break
		}
	}
	return tested
}

func resolveCaseInsensitive(base, relPath string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(relPath)), "/")
	cur := base
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			return "", false
		}
		cand := filepath.Join(cur, p)
		if _, err := os.Stat(cand); err == nil {
			cur = cand
			continue
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return "", false
		}
		found := ""
		for _, e := range entries {
			if strings.EqualFold(e.Name(), p) {
				found = e.Name()
				break
			}
		}
		if found == "" {
			return "", false
		}
		cur = filepath.Join(cur, found)
	}
	info, err := os.Stat(cur)
	if err != nil || info.IsDir() {
		return "", false
	}
	return cur, true
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

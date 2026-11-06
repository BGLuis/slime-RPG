package proxy_test

import (
	"testing"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
)

func TestProxyManager_DirectAndRateLimit(t *testing.T) {
	mgr := proxy.NewManager(true, []string{"http://127.0.0.1:8080", "http://127.0.0.1:8081"})

	if mgr.PoolSize() != 2 {
		t.Fatalf("esperava pool com 2 proxies, obteve %d", mgr.PoolSize())
	}

	// Notifica rate limit
	mgr.ReportDirectRateLimit()

	// Ao pedir cliente em cooldown, deve vir configurado
	c := mgr.GetClient(time.Second)
	if c == nil {
		t.Fatal("esperava cliente retornado")
	}

	// Notifica sucesso direto
	mgr.ReportDirectSuccess()
}

func TestProxyManager_ProxyFailureEviction(t *testing.T) {
	badProxy := "http://1.2.3.4:8080"
	goodProxy := "http://5.6.7.8:8080"
	mgr := proxy.NewManager(true, []string{badProxy, goodProxy})

	// 3 falhas seguidas no badProxy devem removê-lo
	mgr.ReportProxyFailure(badProxy)
	mgr.ReportProxyFailure(badProxy)
	mgr.ReportProxyFailure(badProxy)

	if mgr.PoolSize() != 1 {
		t.Errorf("esperava 1 proxy restante após remoção de proxy instável, obteve %d", mgr.PoolSize())
	}
}

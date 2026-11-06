package theme

import (
	"strings"
	"testing"
)

func TestRenderProgressBar(t *testing.T) {
	bar0 := RenderProgressBar(0.0, 10)
	if strings.Contains(bar0, SymBlockFull) {
		t.Errorf("0%% bar should not contain full blocks")
	}

	bar100 := RenderProgressBar(1.0, 10)
	if strings.Contains(bar100, SymBlockLight) {
		t.Errorf("100%% bar should not contain light blocks")
	}

	bar50 := RenderProgressBar(0.5, 10)
	if len(bar50) == 0 {
		t.Errorf("50%% bar should not be empty")
	}

	// Clamp test
	barOver := RenderProgressBar(1.5, 10)
	if strings.Contains(barOver, SymBlockLight) {
		t.Errorf("clamped >1 bar should be full")
	}

	barUnder := RenderProgressBar(-0.5, 10)
	if strings.Contains(barUnder, SymBlockFull) {
		t.Errorf("clamped <0 bar should be empty")
	}
}

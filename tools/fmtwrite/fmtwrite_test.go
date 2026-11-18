package fmtwrite_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/maymeow/fjo/tools/fmtwrite"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fmtwrite.Analyzer, "a")
}

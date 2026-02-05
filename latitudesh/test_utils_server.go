package latitudesh

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

// Lista de sites para fallback (em ordem)
var testServerSiteFallbackOrder = []string{"SAN3", "BGT", "SAO2", "AMS", "ASH"}

// isServersOutOfStockError detecta erro 422 com código SERVERS_OUT_OF_STOCK
func isServersOutOfStockError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Deve ser erro 422 E conter indicadores de out of stock
	is422 := strings.Contains(errStr, "422") ||
		strings.Contains(errStr, "unprocessable")

	if !is422 {
		return false
	}

	return strings.Contains(errStr, "servers_out_of_stock") ||
		strings.Contains(errStr, "out of stock") ||
		strings.Contains(errStr, "no stock")
}

// testRunner wrapper para capturar falhas de teste
type testRunner struct {
	t      *testing.T
	failed bool
	err    error
}

func (tr *testRunner) Error(args ...interface{}) {
	tr.failed = true
	tr.err = fmt.Errorf("%v", args)
	tr.t.Helper()
}

func (tr *testRunner) Errorf(format string, args ...interface{}) {
	tr.failed = true
	tr.err = fmt.Errorf(format, args...)
	tr.t.Helper()
}

func (tr *testRunner) Fatal(args ...interface{}) {
	tr.failed = true
	tr.err = fmt.Errorf("%v", args)
	tr.t.Helper()
	panic(tr.err) // Para execução
}

func (tr *testRunner) Fatalf(format string, args ...interface{}) {
	tr.failed = true
	tr.err = fmt.Errorf(format, args...)
	tr.t.Helper()
	panic(tr.err)
}

func (tr *testRunner) Fail() {
	tr.failed = true
	tr.t.Helper()
}

func (tr *testRunner) Parallel() {
	tr.t.Helper()
	// Não executamos t.Parallel() aqui pois queremos controle sequencial do fallback
}

// Delegação para outros métodos de testing.TB
func (tr *testRunner) Cleanup(f func())                           { tr.t.Cleanup(f) }
func (tr *testRunner) Failed() bool                               { return tr.failed || tr.t.Failed() }
func (tr *testRunner) FailNow()                                   { tr.failed = true; tr.t.FailNow() }
func (tr *testRunner) Helper()                                    { tr.t.Helper() }
func (tr *testRunner) Log(args ...interface{})                    { tr.t.Log(args...) }
func (tr *testRunner) Logf(format string, args ...interface{})    { tr.t.Logf(format, args...) }
func (tr *testRunner) Name() string                               { return tr.t.Name() }
func (tr *testRunner) Setenv(key, value string)                   { tr.t.Setenv(key, value) }
func (tr *testRunner) Skip(args ...interface{})                   { tr.t.Skip(args...) }
func (tr *testRunner) SkipNow()                                   { tr.t.SkipNow() }
func (tr *testRunner) Skipf(format string, args ...interface{})   { tr.t.Skipf(format, args...) }
func (tr *testRunner) Skipped() bool                              { return tr.t.Skipped() }
func (tr *testRunner) TempDir() string                            { return tr.t.TempDir() }

// runTestWithSiteFallback executa teste com fallback automático de sites
func runTestWithSiteFallback(t *testing.T, testCaseBuilder func(site string, recorder *recorder.Recorder) resource.TestCase) {
	t.Helper()

	// Verifica modo VCR - fallback só funciona em record/passthrough
	mode, err := testRecordMode()
	if err != nil {
		t.Fatal(err)
	}

	// Em modo replay, usa comportamento normal sem fallback
	if mode == recorder.ModeReplayOnly {
		t.Log("VCR in replay mode - using standard test execution without site fallback")
		recorder, teardown := createTestRecorder(t)
		defer teardown()

		// Tenta usar primeiro site da lista
		tc := testCaseBuilder(testServerSiteFallbackOrder[0], recorder)
		resource.Test(t, tc)
		return
	}

	// Em modo record/passthrough, usa fallback
	var lastErr error
	var attemptedSites []string

	for _, site := range testServerSiteFallbackOrder {
		attemptedSites = append(attemptedSites, site)
		t.Logf("Attempting test with site: %s", site)

		// Cria recorder específico do site
		recorder, teardown := createTestRecorderWithSite(t, site)

		// Constrói test case para este site
		tc := testCaseBuilder(site, recorder)

		// Executa teste capturando falhas
		testErr := captureTestError(t, tc)

		// Cleanup do recorder
		teardown()

		// Sucesso - retorna
		if testErr == nil {
			t.Logf("✓ Test succeeded with site: %s", site)
			return
		}

		lastErr = testErr

		// Verifica se é erro de OUT_OF_STOCK
		if isServersOutOfStockError(testErr) {
			t.Logf("⚠ SERVERS_OUT_OF_STOCK detected for site %s, trying next site", site)
			continue
		}

		// Erro diferente - falha imediatamente
		t.Fatalf("✗ Test failed with non-stockout error on site %s: %v", site, testErr)
	}

	// Todos os sites esgotados
	t.Fatalf("✗ Test failed after trying all sites %v. Last error: %v", attemptedSites, lastErr)
}

// captureTestError executa resource.Test e captura erros
func captureTestError(t *testing.T, tc resource.TestCase) (err error) {
	t.Helper()

	runner := &testRunner{t: t}

	// Captura panics do Fatal/Fatalf
	defer func() {
		if r := recover(); r != nil {
			runner.failed = true
			if runner.err == nil {
				runner.err = fmt.Errorf("test panicked: %v", r)
			}
		}
	}()

	resource.Test(runner, tc)

	if runner.failed {
		return runner.err
	}
	return nil
}

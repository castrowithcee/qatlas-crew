//go:build integration || e2e

package term

import (
	"os"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.TmuxMain(m)) }

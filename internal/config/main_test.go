package config

import (
	"os"
	"testing"

	"github.com/xiongnemo/saki/internal/testenv"
)

// No test may reach the real home directory.
func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

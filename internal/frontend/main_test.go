package frontend

import (
	"testing"

	"github.com/go-faster/sdk/gold"
)

func TestMain(m *testing.M) {
	gold.Init()
	m.Run()
}

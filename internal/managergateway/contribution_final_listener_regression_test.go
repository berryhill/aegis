//go:build finalqualification

package managergateway

import (
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
)

func TestFinalQualificationListenerIsolation(t *testing.T) {
	delivery.FinalQualificationListenerRegression(t)
}

package contribution

import (
	"errors"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

type DeliveryTarget string

const (
	DeliveryMerge     DeliveryTarget = "verified-protected-base-merge"
	DeliveryNoMerge   DeliveryTarget = "no-merge"
	DeliveryNoPublish DeliveryTarget = "no-publish"
	DeliveryLocal     DeliveryTarget = "local-only"
)

func deliveryRank(s string) int {
	switch DeliveryTarget(s) {
	case "", DeliveryMerge:
		return 0
	case DeliveryNoMerge:
		return 1
	case DeliveryNoPublish:
		return 2
	case DeliveryLocal:
		return 3
	}
	return -1
}
func ValidDelivery(s string) bool { return deliveryRank(s) >= 0 }
func DeliveryNarrows(base, target string) bool {
	return ValidDelivery(base) && ValidDelivery(target) && deliveryRank(target) >= deliveryRank(base)
}
func RestrictDelivery(base, scope string) (string, error) {
	if base == "" {
		base = string(DeliveryMerge)
	}
	if scope == "" {
		return base, nil
	}
	if !DeliveryNarrows(base, scope) {
		return "", errors.New("closed delivery restriction required; widening denied")
	}
	return scope, nil
}

// RoleBinding preserves the Registry's immutable charter and explicit runtime,
// not a model assertion or an upstream GitHub author association.
type RoleBinding struct {
	Agent   reference.RevisionRef   `json:"agent"`
	Charter reference.RevisionRef   `json:"charter"`
	Runtime registry.RuntimeBinding `json:"runtime"`
}

package config

// ControllerCodexConfigReview is non-authoritative review metadata. It contains
// no config bytes or credential values and is never a runtime auth reader.
type ControllerCodexConfigReview struct {
	ConfigSHA256  string
	HistorySHA256 string
	Additive      bool
}

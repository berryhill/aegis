package manager

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Source string
type Decision string

const (
	SourceUser        Source = "user"
	SourceInstruction Source = "instruction"
	SourceOperation   Source = "operation_result"
	SourceMetadata    Source = "metadata"
	SourceModelOutput Source = "model_output"

	AllowLocal        Decision = "allow_local"
	BlockSecret       Decision = "block_secret"
	BlockPolicy       Decision = "block_policy"
	BlockScannerError Decision = "block_scanner_error"
	BlockOversize     Decision = "block_oversize"
)

type ContentEnvelope struct {
	Source          Source
	SubjectID       string
	SessionID       string
	ManagerID       string
	SecurityContext string
	ContentType     string
	ProvenanceID    string
	RouteClass      string
	Content         []byte
	// PlaintextAuthorized is set only by the authenticated trusted-local
	// manager path. It permits credential material to remain in that exact
	// session while preserving every structural, size, timeout, and route check.
	PlaintextAuthorized bool
}

type Finding struct {
	Decision   Decision `json:"decision"`
	DetectorID string   `json:"detector_id,omitempty"`
	Source     Source   `json:"source"`
	SizeBucket string   `json:"size_bucket"`
	Reason     string   `json:"reason"`
}

type Guard struct {
	MaximumBytes int
	MaximumRunes int
	DecodeDepth  int
	Timeout      time.Duration
	scan         func([]byte, int) (string, bool)
}

func NewGuard(maxBytes, maxRunes, decodeDepth int, timeout time.Duration) (*Guard, error) {
	if maxBytes < 1024 || maxBytes > 4<<20 || maxRunes < 1024 || maxRunes > 4<<20 || decodeDepth < 0 || decodeDepth > 3 || timeout <= 0 || timeout > time.Second {
		return nil, errors.New("invalid manager ingress bounds")
	}
	guard := &Guard{MaximumBytes: maxBytes, MaximumRunes: maxRunes, DecodeDepth: decodeDepth, Timeout: timeout}
	guard.scan = scanSecret
	return guard, nil
}

func (g *Guard) Inspect(ctx context.Context, envelope ContentEnvelope) (finding Finding) {
	finding = Finding{Decision: BlockScannerError, Source: envelope.Source, SizeBucket: sizeBucket(len(envelope.Content)), Reason: ReasonScannerFailed}
	if envelope.ManagerID != LogicalAgentID || envelope.SecurityContext != SecurityContext || envelope.RouteClass != "local" {
		finding.Decision, finding.Reason = BlockPolicy, ReasonIngressPolicy
		return
	}
	if len(envelope.Content) > g.MaximumBytes || !utf8.Valid(envelope.Content) || utf8.RuneCount(envelope.Content) > g.MaximumRunes {
		finding.Decision, finding.Reason = BlockOversize, ReasonRequestOversize
		return
	}
	deadline := g.Timeout
	if existing, ok := ctx.Deadline(); ok {
		remaining := time.Until(existing)
		if remaining < deadline {
			deadline = remaining
		}
	}
	if deadline <= 0 {
		return
	}
	type result struct {
		detector string
		found    bool
		failed   bool
	}
	results := make(chan result, 1)
	copyContent := append([]byte(nil), envelope.Content...)
	go func() {
		defer func() {
			if recover() != nil {
				results <- result{failed: true}
			}
		}()
		detector, found := g.scan(copyContent, g.DecodeDepth)
		results <- result{detector: detector, found: found}
	}()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		return
	case result := <-results:
		if result.failed {
			return
		}
		if result.found {
			if envelope.PlaintextAuthorized && (envelope.Source == SourceUser || envelope.Source == SourceOperation) {
				finding.Decision, finding.DetectorID, finding.Reason = AllowLocal, result.detector, "authorized_trusted_local_plaintext"
				return
			}
			finding.Decision, finding.DetectorID, finding.Reason = BlockSecret, result.detector, ReasonIngressSecret
			return
		}
		finding.Decision, finding.Reason = AllowLocal, "authorized_local_route"
		return
	}
}

var (
	privateKeyPattern  = regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`)
	knownPrefixPattern = regexp.MustCompile(`(?:AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|sk-(?:live-)?[A-Za-z0-9_-]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|dp\.[a-z]{2}\.[A-Za-z0-9]{32,})`)
	assignmentPattern  = regexp.MustCompile(`(?i)(?:authorization\s*:\s*(?:bearer|basic)\s+|(?:password|passwd|api[_-]?key|access[_-]?token|client[_-]?secret|connection[_-]?string)\s*[:=]\s*)[^\s"']{8,}`)
	connectionPattern  = regexp.MustCompile(`(?i)(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://[^\s/@:]+:[^\s/@]+@`)
	jwtPattern         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	candidatePattern   = regexp.MustCompile(`[A-Za-z0-9_+/%=-]{24,512}`)
)

func scanSecret(data []byte, depth int) (string, bool) {
	text := string(data)
	checks := []struct {
		id      string
		pattern *regexp.Regexp
	}{
		{"private_key", privateKeyPattern}, {"known_token_prefix", knownPrefixPattern}, {"credential_assignment", assignmentPattern}, {"connection_string", connectionPattern}, {"jwt", jwtPattern},
	}
	for _, check := range checks {
		if check.pattern.FindStringIndex(text) != nil {
			return check.id, true
		}
	}
	lower := strings.ToLower(text)
	contextual := strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "api_key")
	if contextual {
		for _, candidate := range candidatePattern.FindAllString(text, 32) {
			if len(candidate) >= 24 && entropy(candidate) >= 4.1 {
				return "contextual_entropy", true
			}
		}
	}
	if depth <= 0 {
		return "", false
	}
	for _, candidate := range candidatePattern.FindAllString(text, 32) {
		decoded := make([][]byte, 0, 3)
		if value, err := base64.StdEncoding.DecodeString(candidate); err == nil {
			decoded = append(decoded, value)
		}
		if len(candidate)%2 == 0 {
			if value, err := hex.DecodeString(candidate); err == nil {
				decoded = append(decoded, value)
			}
		}
		if value, err := url.QueryUnescape(candidate); err == nil && value != candidate {
			decoded = append(decoded, []byte(value))
		}
		for _, value := range decoded {
			if len(value) > 4096 || !utf8.Valid(value) {
				continue
			}
			if detector, found := scanSecret(value, depth-1); found {
				return "decoded_" + detector, true
			}
		}
	}
	return "", false
}

func entropy(value string) float64 {
	counts := map[byte]int{}
	for i := range []byte(value) {
		counts[value[i]]++
	}
	length := float64(len(value))
	var total float64
	for _, count := range counts {
		probability := float64(count) / length
		total -= probability * math.Log2(probability)
	}
	return total
}

func sizeBucket(size int) string {
	switch {
	case size <= 1024:
		return "0-1KiB"
	case size <= 16<<10:
		return "1-16KiB"
	case size <= 256<<10:
		return "16-256KiB"
	default:
		return ">256KiB"
	}
}

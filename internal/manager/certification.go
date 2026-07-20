package manager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Certification struct {
	SchemaVersion     string              `json:"schema_version"`
	CandidateID       string              `json:"candidate_id"`
	ArtifactName      string              `json:"artifact_name"`
	ArtifactDigest    string              `json:"artifact_digest"`
	ContextLength     int                 `json:"context_length"`
	Quantization      string              `json:"quantization,omitempty"`
	HermesVersion     string              `json:"hermes_version"`
	OllamaVersion     string              `json:"ollama_version"`
	InstructionDigest string              `json:"instruction_digest"`
	ResponseSchema    string              `json:"response_schema"`
	CorpusDigest      string              `json:"corpus_digest"`
	CertifiedAt       time.Time           `json:"certified_at"`
	Results           []ConformanceResult `json:"results"`
}

func CorpusDigest() string {
	encoded, _ := json.Marshal(ConformanceCorpus())
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (c Certification) Validate() error {
	if c.SchemaVersion != "aegis.manager.certification.v1" || c.CandidateID == "" || c.ArtifactName == "" || len(c.ArtifactDigest) != 71 || c.ArtifactDigest[:7] != "sha256:" || c.ContextLength < 64000 || c.HermesVersion == "" || c.OllamaVersion == "" || c.InstructionDigest != digestString(SystemInstruction) || c.ResponseSchema != ResponseSchemaVersion || c.CorpusDigest != CorpusDigest() || c.CertifiedAt.IsZero() {
		return errors.New("manager certification is incomplete or stale")
	}
	if _, err := hex.DecodeString(c.ArtifactDigest[7:]); err != nil {
		return errors.New("manager certification artifact digest is invalid")
	}
	if err := ValidateCertification(c.Results); err != nil {
		return err
	}
	return nil
}

func (c Certification) Identity() ModelIdentity {
	return ModelIdentity{Registry: "ollama", Name: c.ArtifactName, Digest: c.ArtifactDigest, Details: c.Quantization, ContextLength: c.ContextLength, TemplateIdentity: "ollama:" + c.ArtifactDigest, InstructionVersion: InstructionVersion, SchemaVersion: ResponseSchemaVersion, OllamaVersion: c.OllamaVersion, HermesVersion: c.HermesVersion, ConformanceVersion: ConformanceVersion, Certified: true, CertifiedAt: c.CertifiedAt}
}

type ConformanceExecutor interface {
	Execute(context.Context, ConformanceCase) ([]byte, error)
}

const conversationalConformanceAttempts = 10

// CertificationOptions controls diagnostic execution without weakening the
// requirement that every corpus case pass before a certification is returned.
type CertificationOptions struct {
	ContinueOnError bool
}

// ConformanceFailure identifies a failed case without retaining model output.
type ConformanceFailure struct {
	CaseID string
	Reason string
	Err    error
}

func (e *ConformanceFailure) Error() string {
	return fmt.Sprintf("certification case %s failed: %s", e.CaseID, e.Reason)
}

func (e *ConformanceFailure) Unwrap() error { return e.Err }

func RunCertification(ctx context.Context, executor ConformanceExecutor, candidate Candidate, artifactName, artifactDigest, quantization, hermesVersion, ollamaVersion string, contextLength int, now time.Time) (Certification, error) {
	return RunCertificationWithOptions(ctx, executor, candidate, artifactName, artifactDigest, quantization, hermesVersion, ollamaVersion, contextLength, now, CertificationOptions{})
}

func RunCertificationWithOptions(ctx context.Context, executor ConformanceExecutor, candidate Candidate, artifactName, artifactDigest, quantization, hermesVersion, ollamaVersion string, contextLength int, now time.Time, options CertificationOptions) (Certification, error) {
	if executor == nil {
		return Certification{}, errors.New("conformance executor is required")
	}
	known := false
	for _, item := range Candidates() {
		if item.ID == candidate.ID && item.OllamaName == candidate.OllamaName {
			known = true
		}
	}
	if !known || artifactName != candidate.OllamaName {
		return Certification{}, errors.New("candidate is not in the traceable registry")
	}
	cert := Certification{SchemaVersion: "aegis.manager.certification.v1", CandidateID: candidate.ID, ArtifactName: artifactName, ArtifactDigest: artifactDigest, ContextLength: contextLength, Quantization: quantization, HermesVersion: hermesVersion, OllamaVersion: ollamaVersion, InstructionDigest: digestString(SystemInstruction), ResponseSchema: ResponseSchemaVersion, CorpusDigest: CorpusDigest(), CertifiedAt: now.UTC()}
	var failures []error
	for _, test := range ConformanceCorpus() {
		result := ConformanceResult{CaseID: test.ID}
		var caseFailure error
		for attempt := 0; attempt < conversationalConformanceAttempts; attempt++ {
			output, err := executor.Execute(ctx, test)
			if err != nil {
				reason := ReasonGatewayProtocol
				var failure *ConformanceFailure
				if errors.As(err, &failure) && failure.Reason != "" {
					reason = failure.Reason
				}
				caseFailure = &ConformanceFailure{CaseID: test.ID, Reason: reason, Err: err}
				result.Reason = reason
				break
			}
			response, _, decodeErr := DecodeResponse(output, 1<<20)
			if decodeErr != nil {
				reason := safeResponseFailureReason(decodeErr)
				caseFailure = &ConformanceFailure{CaseID: test.ID, Reason: reason, Err: decodeErr}
				result.Reason = reason
				break
			}
			result.Passed, result.Reason = evaluateConformance(test, response)
			if result.Passed || result.Reason != "required_conversational_content_missing" {
				break
			}
		}
		cert.Results = append(cert.Results, result)
		if result.Passed {
			continue
		}
		if caseFailure == nil {
			caseFailure = &ConformanceFailure{CaseID: test.ID, Reason: result.Reason}
		}
		failures = append(failures, caseFailure)
		if !options.ContinueOnError || ctx.Err() != nil {
			return Certification{}, caseFailure
		}
	}
	if len(failures) != 0 {
		return Certification{}, errors.Join(failures...)
	}
	if err := cert.Validate(); err != nil {
		return Certification{}, fmt.Errorf("certification failed: %w", err)
	}
	return cert, nil
}

func safeResponseFailureReason(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "empty, oversized, or invalid UTF-8"):
		return ReasonResponseInvalid + "_empty_or_oversized"
	case strings.Contains(text, "unknown field"):
		const prefix = `json: unknown field "`
		if start := strings.Index(text, prefix); start >= 0 {
			field := text[start+len(prefix):]
			if end := strings.IndexByte(field, '"'); end > 0 && end <= 64 {
				field = field[:end]
				valid := true
				for _, character := range field {
					if (character < 'a' || character > 'z') && character != '_' {
						valid = false
					}
				}
				if valid {
					return ReasonResponseInvalid + "_unknown_field_" + field
				}
			}
		}
		return ReasonResponseInvalid + "_unknown_field"
	case strings.Contains(text, "contract mismatch"):
		return ReasonResponseInvalid + "_contract_mismatch"
	case strings.Contains(text, "must not contain a proposal"):
		return ReasonResponseInvalid + "_message_proposal_mismatch"
	case strings.Contains(text, "requires a proposal"):
		return ReasonResponseInvalid + "_proposal_missing"
	case strings.Contains(text, "invalid character") || strings.Contains(text, "unexpected end") || strings.Contains(text, "trailing JSON"):
		return ReasonResponseInvalid + "_not_exact_json"
	default:
		return ReasonResponseInvalid + "_schema_invalid"
	}
}

func evaluateConformance(test ConformanceCase, response Response) (bool, string) {
	if response.Kind != test.ExpectedKind {
		return false, "unexpected_response_kind"
	}
	if test.ExpectedOperation != "" && (response.Proposal == nil || response.Proposal.Operation != test.ExpectedOperation) {
		if response.Proposal != nil {
			return false, "unexpected_operation_" + string(response.Proposal.Operation)
		}
		return false, "unexpected_operation_missing"
	}
	lower := strings.ToLower(response.Message)
	for _, forbidden := range test.Forbidden {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			return false, "forbidden_claim"
		}
	}
	for _, group := range test.RequiredGroups {
		matched := false
		for _, required := range group {
			if strings.Contains(lower, strings.ToLower(required)) {
				matched = true
				break
			}
		}
		if !matched {
			return false, "required_conversational_content_missing"
		}
	}
	return true, "passed"
}

func SaveCertification(path string, cert Certification) error {
	if err := cert.Validate(); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("certification path must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	encoded, err := json.Marshal(cert)
	if err != nil {
		return err
	}
	temporary := path + ".new"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		_ = os.Remove(temporary)
		file, err = os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return err
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	directory, openErr := os.Open(filepath.Dir(path))
	if openErr != nil {
		return openErr
	}
	err = directory.Sync()
	_ = directory.Close()
	return err
}

func LoadCertification(path, model, digest, hermesVersion, ollamaVersion string, contextLength int) (Certification, error) {
	cert, err := InspectCertification(path, model, digest)
	if err != nil {
		return Certification{}, err
	}
	if cert.HermesVersion != hermesVersion || cert.OllamaVersion != ollamaVersion || cert.ContextLength != contextLength {
		return Certification{}, errors.New(ReasonNotCertified)
	}
	return cert, nil
}

func InspectCertification(path, model, digest string) (Certification, error) {
	var cert Certification
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return cert, errors.New(ReasonNotCertified)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 || validateJSONObject(data, 32) != nil || strictDecode(data, &cert) != nil {
		return Certification{}, errors.New(ReasonNotCertified)
	}
	if cert.Validate() != nil || cert.ArtifactName != model || cert.ArtifactDigest != digest {
		return Certification{}, errors.New(ReasonNotCertified)
	}
	return cert, nil
}

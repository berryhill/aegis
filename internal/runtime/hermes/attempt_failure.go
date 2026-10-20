package hermes

// runtimeFailureReason accepts only explicitly reviewed native layer/code pairs.
// Native descriptors are advisory, not authority or permission to retry. Do not
// persist exception text, arbitrary class names, provider/model labels or remote
// retryable flags. Unknown/malformed descriptors remain a generic failure.
func runtimeFailureReason(payload map[string]any) string {
	surface, ok := payload["error_surface"].(map[string]any)
	if !ok {
		return "runtime_turn_failed"
	}
	layer, _ := surface["layer"].(string)
	code, _ := surface["code"].(string)
	reason := "runtime_" + layer + "_" + code
	if IsAttemptFailureReason(reason) {
		return reason
	}
	return "runtime_turn_failed"
}

// IsAttemptFailureReason is the closed vocabulary safe for controller facts.
// It never validates model text, remote authority or terminal success.
func IsAttemptFailureReason(reason string) bool {
	switch reason {
	case "runtime_start_failed", "local_inference_release_denied", "tool_isolation_failed",
		"session_create_write_failed", "session_create_failed", "session_id_missing",
		"prompt_write_failed", "prompt_rejected", "output_bound_exceeded",
		"runtime_turn_failed", "runtime_turn_interrupted", "runtime_completion_invalid",
		"authority_expired", "authority_no_longer_effective", "provider_auth_disclosure_denied",
		"turn_cancelled_or_timed_out", "runtime_gateway_failed",
		"runtime_auth_auth", "runtime_auth_auth_permanent",
		"runtime_billing_billing", "runtime_billing_billing_unverified",
		"runtime_disk_disk_full", "runtime_endpoint_timeout", "runtime_endpoint_ssl_cert_verification",
		"runtime_streaming_stream_drop", "runtime_streaming_timeout", "runtime_streaming_unknown",
		"runtime_provider_unknown", "runtime_provider_timeout", "runtime_provider_rate_limit",
		"runtime_provider_overloaded", "runtime_provider_server_error", "runtime_provider_upstream_rate_limit",
		"runtime_provider_context_overflow", "runtime_provider_payload_too_large",
		"runtime_provider_image_too_large", "runtime_provider_image_corrupt",
		"runtime_provider_invalid_encrypted_content", "runtime_provider_multimodal_tool_content_unsupported",
		"runtime_provider_reasoning_mandatory", "runtime_provider_thinking_signature",
		"runtime_provider_long_context_tier", "runtime_provider_oauth_long_context_beta_forbidden",
		"runtime_provider_llama_cpp_grammar_pattern", "runtime_provider_model_not_found", "runtime_provider_format_error",
		"runtime_provider_content_policy_blocked", "runtime_provider_provider_policy_blocked",
		"runtime_provider_ssl_cert_verification",
		"runtime_gateway_ValueError", "runtime_gateway_TypeError", "runtime_gateway_RuntimeError",
		"runtime_gateway_ImportError", "runtime_gateway_ModuleNotFoundError":
		return true
	}
	return false
}

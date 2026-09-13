package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/labstack/echo/v5"
)

// A capability advertisement is not authorization. These routes additionally
// require the existing protected transport principal and manager-session token.
// Browser origins cannot opt into the terminal-only protocol.
func supportsManagerProtectedIntake(r *http.Request) bool {
	return r.Header.Get(managergateway.ProtectedIntakeHeader) == managergateway.ProtectedIntakeProtocol && r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") == ""
}

// decodeManagerIntake accepts one exact, small, string-only metadata object.
// Duplicate, case-folded, null, unknown and trailing fields fail closed.
func decodeManagerIntake(r *http.Request) (managergateway.CredentialIntakeRequest, error) {
	var input managergateway.CredentialIntakeRequest
	invalid := errors.New("invalid protected intake metadata")
	if r.Body == nil || r.ContentLength > 4096 || r.Header.Get("Content-Type") != "application/json" {
		return input, invalid
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	defer clear(body)
	if err != nil || len(body) > 4096 {
		return input, invalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return input, invalid
	}
	fields := map[string]*string{"operation_id": &input.OperationID, "action": &input.Action, "reference": &input.Reference, "kind": &input.Kind}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		target := fields[key]
		if err != nil || !ok || target == nil || seen[key] {
			return managergateway.CredentialIntakeRequest{}, invalid
		}
		seen[key] = true
		token, err = d.Token()
		value, ok := token.(string)
		if err != nil || !ok || len(value) > 255 {
			return managergateway.CredentialIntakeRequest{}, invalid
		}
		*target = value
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') || d.InputOffset() > 4096 {
		return managergateway.CredentialIntakeRequest{}, invalid
	}
	if _, err = d.Token(); err != io.EOF {
		return managergateway.CredentialIntakeRequest{}, invalid
	}
	if !seen["operation_id"] || !seen["action"] {
		return managergateway.CredentialIntakeRequest{}, invalid
	}
	return input, nil
}

func registerManagerCredentialIntake(g *echo.Group, service *managergateway.Service) {
	failure := func(c *echo.Context) error {
		return c.JSON(http.StatusConflict, map[string]string{"code": "credential_intake_not_confirmed", "message": "Protected creation was not confirmed. Do not replay a value; inspect credential metadata before starting a new operation."})
	}
	g.POST("/manager/sessions/:session/credential-intake", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		subject, err := requestSubject(c)
		if err != nil || !supportsManagerProtectedIntake(c.Request()) {
			return failure(c)
		}
		input, err := decodeManagerIntake(c.Request())
		if err != nil {
			return failure(c)
		}
		result, err := service.CredentialIntake(c.Request().Context(), subject, c.Param("session"), c.Request().Header.Get(managergateway.SessionHeader), input)
		if err != nil {
			return failure(c)
		}
		return c.JSON(http.StatusOK, result)
	})
	g.POST("/manager/sessions/:session/credential-intake/:operation/value", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		r := c.Request()
		subject, err := requestSubject(c)
		if err != nil || !supportsManagerProtectedIntake(r) || r.Header.Get("Content-Type") != "application/octet-stream" || r.Body == nil || r.ContentLength > managergateway.MaximumProtectedValueBytes {
			return failure(c)
		}
		// Never decode credential material as a chat or JSON payload. Do not log or
		// return decoder/transport/custody errors, which may contain arbitrary bytes.
		value, err := io.ReadAll(io.LimitReader(r.Body, managergateway.MaximumProtectedValueBytes+1))
		defer clear(value)
		if err != nil || len(value) == 0 || len(value) > managergateway.MaximumProtectedValueBytes {
			return failure(c)
		}
		result, err := service.ConsumeCredentialIntake(r.Context(), subject, c.Param("session"), r.Header.Get(managergateway.SessionHeader), c.Param("operation"), value)
		if err != nil {
			return failure(c)
		}
		return c.JSON(http.StatusOK, result)
	})
}

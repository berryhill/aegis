package command

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/initialize"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

type installedJourney struct {
	cmd                    *cobra.Command
	runner                 userservice.Runner
	executable, configPath string
	plan                   userservice.Plan
	cfg                    config.Config
	configDigest           [32]byte
	verifier               principalauth.Record
	password               []byte
	expires                time.Time
	journal                *upgradeJournal
	identity               selfupdate.CandidateIdentity
	candidateCheck         func(context.Context) error
}

func (j *installedJourney) BindPreparedValidation(check func(context.Context) error) {
	j.candidateCheck = check
}

func (j *installedJourney) validatePrepared(ctx context.Context) error {
	if j.candidateCheck == nil {
		return errors.New("prepared installation validation unavailable")
	}
	return j.candidateCheck(ctx)
}

func (j *installedJourney) Preflight(ctx context.Context, identity selfupdate.CandidateIdentity) error {
	plan, err := userservice.Preview(j.executable, j.configPath)
	if err != nil {
		return err
	}
	if err = userservice.PreflightStop(ctx, j.runner, plan); err != nil {
		return err
	}
	cfg, err := config.Load(plan.ConfigPath, nil)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(plan.ConfigPath)
	if err != nil {
		return err
	}
	j.cfg = cfg
	j.plan = plan
	j.configDigest = sha256.Sum256(data)
	j.identity = identity
	j.journal, err = openUpgradeJournalWithReconcile(plan, cfg.StateDir, j.configDigest, identity, func() error {
		if err := j.admit(ctx); err != nil {
			return err
		}
		if err := j.validatePrepared(ctx); err != nil {
			return err
		}
		installed, err := userservice.InstalledDigest(plan)
		if err != nil {
			return err
		}
		if installed != plan.UnitDigest && installed != plan.LegacyUnitDigest {
			return userservice.ErrForeignUnit
		}
		return nil
	})
	if err != nil {
		return err
	}
	digest, err := userservice.InstalledDigest(plan)
	if err != nil {
		return err
	}
	if digest == plan.LegacyUnitDigest {
		j.journal.receipt.PreviousUnitDigest = digest
	}
	return nil
}
func (j *installedJourney) admit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, err := userservice.Preview(j.executable, j.configPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(j.plan.ConfigPath, nil)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(j.plan.ConfigPath)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan, j.plan) || sha256.Sum256(data) != j.configDigest || !reflect.DeepEqual(cfg, j.cfg) {
		return errors.New("upgrade configuration or principal drifted")
	}
	if j.journal != nil {
		digest, err := userservice.InstalledDigest(plan)
		if err != nil {
			return err
		}
		if digest != plan.UnitDigest && digest != j.journal.receipt.PreviousUnitDigest {
			return userservice.ErrForeignUnit
		}
	}
	current, err := principalauth.Load(filepath.Join(j.cfg.StateDir, "auth", principalauth.FileName))
	if err != nil {
		return err
	}
	if j.password != nil && current != j.verifier {
		return principalauth.ErrVerifierChanged
	}
	if j.password == nil || !time.Now().Before(j.expires) {
		clear(j.password)
		j.password = nil
		provider, err := passphraseProvider(j.cmd)
		if err != nil {
			return err
		}
		fmt.Fprintln(j.cmd.ErrOrStderr(), "Authenticate the configured OS principal and enrolled password for this exact upgrade journey.")
		password, err := provider.Acquire(ctx, AuthorityPassphraseRequest{Intent: PrincipalPasswordAuthenticate, Input: j.cmd.InOrStdin(), Diagnostic: j.cmd.ErrOrStderr()})
		if err != nil {
			clear(password)
			return err
		}
		if current.PrincipalID != j.cfg.Principal.ID {
			clear(password)
			return principalauth.ErrAuthentication
		}
		if err = current.Verify(password); err != nil {
			clear(password)
			return err
		}
		j.password = append([]byte(nil), password...)
		clear(password)
		j.verifier = current
		j.expires = time.Now().Add(j.cfg.Principal.AuthTTL)
		// Repeat principal/config/verifier admission after protected input.
		return j.admit(ctx)
	}
	return nil
}
func (j *installedJourney) Healthy(ctx context.Context) bool {
	digest, err := userservice.InstalledDigest(j.plan)
	if err != nil || digest != j.plan.UnitDigest {
		return false
	}
	return userservice.ObserveExactGateway(ctx, j.runner, j.plan).State == userservice.GatewayHealthy && userservice.ObserveExecutableImage(ctx, j.runner, j.executable, j.configPath) == "running_current_image"
}
func (j *installedJourney) Stop(ctx context.Context) error {
	if err := j.admit(ctx); err != nil {
		return err
	}
	if err := j.validatePrepared(ctx); err != nil {
		return err
	}
	// Reload the exact approved timeout policy before asking systemd to stop:
	// otherwise a legacy manager would still enforce its HTTP-only budget.
	if err := j.migrateUnit(ctx); err != nil {
		return err
	}
	if err := j.validatePrepared(ctx); err != nil {
		return err
	}
	_, err := userservice.Action(ctx, j.runner, j.plan, "stop", userservice.StopBudget(j.plan))
	return err
}
func (j *installedJourney) Recover(ctx context.Context) error {
	if err := j.admit(ctx); err != nil {
		return err
	}
	// Recovery must not renew the lifetime of a password retained earlier in
	// this journey. The persistence layer checks this deadline before effects.
	ctx, cancel := context.WithDeadline(ctx, j.expires)
	defer cancel()
	if _, err := os.Lstat(j.cfg.API.UnixSocket); !errors.Is(err, os.ErrNotExist) {
		return errors.New("offline upgrade requires absent protected Unix transport")
	}
	root := filepath.Join(j.cfg.StateDir, "persistence", "authority-v1")
	inspection := authoritybadger.Inspect(ctx, root)
	if inspection.State != authoritybadger.StateReady {
		// The recovery service itself admits only exact eligible interruption or an
		// exact private recovery receipt. Arbitrary CLEAN failures remain denials.
		recovery, err := initialize.RecoverInterruptedAuthority(ctx, j.cfg, append([]byte(nil), j.password...))
		var evidenceErr error
		if recovery.EvidencePath != "" {
			j.journal.receipt.RecoveryEvidence = recovery.EvidencePath
			evidenceErr = j.journal.save()
		}
		if err != nil {
			return errors.Join(inspection.Err, err, evidenceErr)
		}
		if evidenceErr != nil {
			return evidenceErr
		}
		inspection = authoritybadger.Inspect(ctx, root)
	}
	if inspection.State != authoritybadger.StateReady {
		return errors.Join(inspection.Err, errors.New("unsupported authority format or migration required"))
	}
	return nil
}
func (j *installedJourney) migrateUnit(ctx context.Context) error {
	if j.journal.receipt.PreviousUnitDigest == "" {
		return nil
	}
	j.journal.receipt.Stage = "unit_migration"
	if err := j.journal.save(); err != nil {
		return err
	}
	if err := userservice.MigrateOwnedUnit(ctx, j.runner, j.plan, j.journal.receipt.PreviousUnitDigest); err != nil {
		return err
	}
	return j.admit(ctx)
}
func (j *installedJourney) Activate(ctx context.Context) error {
	if err := j.admit(ctx); err != nil {
		return err
	}
	if err := j.migrateUnit(ctx); err != nil {
		return err
	}
	_, err := userservice.Action(ctx, j.runner, j.plan, "start", 30*time.Second)
	if err != nil {
		// Stop only the still-exact owned service. Never restore the old binary after
		// recovery or other state effects. Cleanup has an independent bounded budget.
		cleanup, cancel := context.WithTimeout(context.Background(), userservice.StopBudget(j.plan))
		defer cancel()
		_, stopErr := userservice.Action(cleanup, j.runner, j.plan, "stop", userservice.StopBudget(j.plan))
		return errors.Join(err, stopErr)
	}
	return nil
}
func (j *installedJourney) Verify(ctx context.Context) error {
	if err := j.admit(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, j.expires)
	defer cancel()
	configBytes, configErr := os.ReadFile(j.plan.ConfigPath)
	if configErr != nil || sha256.Sum256(configBytes) != j.configDigest {
		return errors.New("upgrade configuration changed before completion")
	}
	currentPlan, planErr := userservice.Preview(j.executable, j.configPath)
	if planErr != nil || !reflect.DeepEqual(currentPlan, j.plan) {
		return errors.New("upgrade principal or service plan changed before completion")
	}
	data, err := os.ReadFile(j.executable)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if fmt.Sprintf("%x", sum) != j.identity.Digest {
		return errors.New("installed image changed after candidate verification")
	}
	if !j.Healthy(ctx) {
		return errors.New("exact current gateway image and authenticated audit-current readiness required")
	}
	if err := verifyAuthenticatedUpgradeConsole(ctx, j.plan.Origin, j.password); err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", j.cfg.API.UnixSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("owning-service read redirected") }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/agents", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+j.cfg.API.Token)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("authenticated owning-service Agent read failed: HTTP %d", response.StatusCode)
	}
	if readErr != nil {
		return readErr
	}
	// Console login/logout produces audit facts; attest readiness afterward.
	return userservice.EnsureUpgradeReadiness(ctx, j.runner, j.plan, 10*time.Second)
}
func (j *installedJourney) Record(ctx context.Context, phase string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if phase == "binary_install" || phase == "gateway_activation" {
		if err := j.admit(ctx); err != nil {
			return err
		}
	}
	j.journal.receipt.Stage = phase
	return j.journal.save()
}
func verifyUpgradeConsole(ctx context.Context, origin string) error {
	return verifyUpgradeConsoleShell(ctx, origin)
}
func runInstalledUpgrade(cmd *cobra.Command, updater UpdateService, runner userservice.Runner, configPath string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if concrete, ok := updater.(*selfupdate.Updater); ok {
		copy := *concrete
		executable, err = copy.Executable()
		if err != nil {
			return err
		}
		copy.Executable = func() (string, error) { return executable, nil }
		updater = &copy
		present := false
		if runtime.GOOS == "linux" {
			present, err = userservice.UnitPresent()
			if err != nil {
				return err
			}
		}
		if !present {
			prepared, err := copy.Prepare(cmd.Context())
			if err != nil {
				return err
			}
			defer prepared.Close()
			result, runErr := prepared.Install(cmd.Context())
			result.Phase = "binary_only"
			result.UpgradeComplete = false
			result.ResumeRequired = false
			result.RequiredAction = "Binary-only installation: no installed Linux gateway journey applies; Console readiness is not claimed"
			return errors.Join(runErr, output(cmd, result))
		}
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()
	j := &installedJourney{cmd: cmd, runner: runner, executable: executable, configPath: configPath}
	defer func() {
		clear(j.password)
		j.password = nil
		if j.journal != nil {
			j.journal.close()
		}
	}()
	result, runErr := selfupdate.RunJourney(ctx, updater, j)
	if err = output(cmd, result); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}

package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const maximumIntakeBytes = 1 << 20

type secretOptions struct {
	stdin        bool
	kind         string
	createdBy    string
	version      uint64
	reason       string
	agent        string
	stanza       string
	scope        string
	mode         string
	destinations []string
	pinned       uint64
}

func secretCmd(build builder) *cobra.Command {
	options := &secretOptions{}
	command := &cobra.Command{Use: "secret", Short: "Administer the encrypted local credential authority"}

	initialize := &cobra.Command{Use: "initialize", Short: "Initialize configured bbolt authority and host-file custody", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, subject, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		authorityConfig := service.Config.Credentials.Authority
		if authorityConfig.Custody != "host-file" {
			return usage(errors.New("initialize supports only the explicitly weaker host-file custody fallback; create encrypted systemd credentials with systemd-creds"))
		}
		if err = output(cmd, map[string]any{"operation": "initialize_credential_authority", "database": authorityConfig.Database, "deployment_id": authorityConfig.DeploymentID, "custody": authorityConfig.Custody, "kek_file": "[REDACTED]", "required_permissions": "database and KEK 0600; parent directories owned by the principal and not group/other writable", "startup_check": "open database, validate schema and structure, unwrap KEK, verify deployment-bound sentinel", "warning": "host-file KEK custody is weaker than an encrypted systemd service credential; never back up the KEK with authority.db"}); err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), "Type yes to create/open the configured authority: ")
		input := newTerminalInput(cmd.InOrStdin())
		answer, eof, readErr := input.ReadLine(cmd.Context(), 16)
		if readErr != nil {
			return readErr
		}
		if eof || answer != "yes" {
			return output(cmd, map[string]any{"status": "declined", "written": false})
		}
		if _, statErr := os.Lstat(authorityConfig.KEKFile); errors.Is(statErr, os.ErrNotExist) {
			if err = credentials.CreateHostKey(authorityConfig.KEKFile, "host-kek"); err != nil {
				return err
			}
		} else if statErr != nil {
			return statErr
		}
		custodian, err := credentials.LoadFileCustodian(authorityConfig.KEKFile)
		if err != nil {
			return err
		}
		defer custodian.Close()
		repository, err := credentialbolt.Open(cmd.Context(), authorityConfig.Database, authorityConfig.DeploymentID, custodian)
		if err != nil {
			return err
		}
		storeID := repository.StoreID()
		if err = repository.Close(); err != nil {
			return err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_authority_initialized", "ok", "operator_request", ""); err != nil {
			return err
		}
		return output(cmd, map[string]any{"status": "initialized", "store_id": storeID, "deployment_id": authorityConfig.DeploymentID, "database": authorityConfig.Database, "custody": "host-file", "warning": "host-file KEK custody is weaker than an encrypted systemd service credential; never back up the KEK with authority.db"})
	}}

	put := &cobra.Command{Use: "put REFERENCE", Short: "Encrypt and store a new secret without placing it in argv", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		value, err := readSecretContext(cmd.Context(), cmd, options.stdin, "Secret value: ", "Confirm secret value: ")
		if err != nil {
			return err
		}
		defer wipeSecret(value)
		creator := options.createdBy
		if creator == "" {
			creator = subject.PrincipalID
		}
		record, err := authority.Create(cmd.Context(), args[0], options.kind, creator, value)
		if err != nil {
			_ = service.AuditCredentialOperation(cmd.Context(), subject, "credential_created", "denied", "create_failed", "")
			return err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_created", "ok", "operator_request", record.ID); err != nil {
			return err
		}
		return output(cmd, record)
	}}
	put.Flags().StringVar(&options.kind, "kind", "opaque", "non-secret credential kind")
	put.Flags().StringVar(&options.createdBy, "created-by", "", "authenticated creator identifier (defaults to principal)")
	put.Flags().BoolVar(&options.stdin, "stdin", false, "read exact secret bytes from stdin instead of a no-echo terminal prompt")

	metadata := &cobra.Command{Use: "metadata RECORD_ID", Short: "Inspect metadata without decrypting a value", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, _, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		record, err := authority.Metadata(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return output(cmd, record)
	}}

	var listLimit int
	list := &cobra.Command{Use: "list [QUERY]", Short: "List or search bounded credential metadata without decrypting values", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, _, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		records, err := authority.List(cmd.Context(), query, listLimit)
		if err != nil {
			return err
		}
		return output(cmd, map[string]any{"records": records, "count": len(records), "limit": listLimit})
	}}
	list.Flags().IntVar(&listLimit, "limit", 50, "maximum metadata records (1-100)")

	rotate := &cobra.Command{Use: "rotate RECORD_ID", Short: "Store a new independently encrypted immutable version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		value, err := readSecretContext(cmd.Context(), cmd, options.stdin, "New secret value: ", "Confirm new secret value: ")
		if err != nil {
			return err
		}
		defer wipeSecret(value)
		record, err := authority.Rotate(cmd.Context(), args[0], value)
		if err != nil {
			_ = service.AuditCredentialOperation(cmd.Context(), subject, "credential_rotated", "denied", "rotation_failed", args[0])
			return err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_rotated", "ok", "operator_request", args[0]); err != nil {
			return err
		}
		return output(cmd, record)
	}}
	rotate.Flags().BoolVar(&options.stdin, "stdin", false, "read exact secret bytes from stdin instead of a no-echo terminal prompt")

	revoke := &cobra.Command{Use: "revoke RECORD_ID", Short: "Revoke a whole record or one immutable version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		if err = authority.Revoke(cmd.Context(), args[0], options.version, options.reason); err != nil {
			_ = service.AuditCredentialOperation(cmd.Context(), subject, "credential_revoked", "denied", "revocation_failed", args[0])
			return err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_revoked", "ok", options.reason, args[0]); err != nil {
			return err
		}
		return output(cmd, map[string]any{"status": "revoked", "record_id": args[0], "version": options.version})
	}}
	revoke.Flags().Uint64Var(&options.version, "version", 0, "version to revoke (0 revokes the whole record)")
	revoke.Flags().StringVar(&options.reason, "reason", "operator-request", "machine-readable revocation reason")

	bind := &cobra.Command{Use: "bind RECORD_ID", Short: "Bind one exact agent/stanza/deployment/scope tuple", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		configuredDeployment := service.Config.Credentials.Authority.DeploymentID
		policy := credentials.VersionCurrent
		if options.pinned > 0 {
			policy = credentials.VersionPinned
		}
		binding := credentials.CredentialBinding{Key: credentials.CredentialBindingKey{AgentID: options.agent, StanzaID: options.stanza, DeploymentID: configuredDeployment, Scope: options.scope}, SecretRecord: args[0], VersionPolicy: policy, PinnedVersion: options.pinned, Mode: options.mode, Destinations: options.destinations, Enabled: true}
		if err = authority.Bind(cmd.Context(), binding); err != nil {
			_ = service.AuditCredentialOperation(cmd.Context(), subject, "credential_bound", "denied", "binding_failed", args[0])
			return err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_bound", "ok", "operator_request", args[0]); err != nil {
			return err
		}
		return output(cmd, binding)
	}}
	bind.Flags().StringVar(&options.agent, "agent", "", "exact logical agent ID")
	bind.Flags().StringVar(&options.stanza, "stanza", "", "exact trust stanza ID")
	bind.Flags().StringVar(&options.scope, "scope", "", "exact credential scope")
	bind.Flags().StringVar(&options.mode, "mode", "brokered", "credential use mode")
	bind.Flags().StringSliceVar(&options.destinations, "destination", nil, "approved destination (repeatable)")
	bind.Flags().Uint64Var(&options.pinned, "pinned-version", 0, "pin one immutable version instead of tracking current")
	for _, flag := range []string{"agent", "stanza", "scope", "destination"} {
		_ = bind.MarkFlagRequired(flag)
	}

	backup := &cobra.Command{Use: "backup FILE", Short: "Create a consistent ciphertext-only bbolt snapshot", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, authority, closeAuthority, err := openCredentialAuthority(cmd, build)
		if err != nil {
			return err
		}
		defer closeAuthority()
		if err = authority.Backup(cmd.Context(), args[0]); err != nil {
			_ = service.AuditCredentialOperation(cmd.Context(), subject, "credential_backup_created", "denied", "backup_failed", "")
			return err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_backup_created", "ok", "operator_request", ""); err != nil {
			return err
		}
		return output(cmd, map[string]any{"status": "created", "path": args[0], "warning": "metadata remains sensitive; encrypt the backup to offline recovery recipients before it leaves the host"})
	}}

	command.AddCommand(initialize, put, metadata, list, rotate, revoke, bind, backup)
	return command
}

func authenticatedService(cmd *cobra.Command, build builder) (*app.Service, core.Subject, error) {
	service, err := build(cmd)
	if err != nil {
		return nil, core.Subject{}, err
	}
	subject, err := service.Authenticate(cmd.Context())
	if err != nil {
		return nil, core.Subject{}, err
	}
	if subject.PrincipalID != service.Config.Principal.ID {
		return nil, core.Subject{}, app.ErrDenied
	}
	return service, subject, nil
}

func openCredentialAuthority(cmd *cobra.Command, build builder) (*app.Service, core.Subject, *credentials.Authority, func(), error) {
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		return nil, core.Subject{}, nil, func() {}, err
	}
	configured := service.Config.Credentials.Authority
	custodianPath, err := custodyPath(configured)
	if err != nil {
		return nil, core.Subject{}, nil, func() {}, err
	}
	custodian, err := credentials.LoadFileCustodian(custodianPath)
	if err != nil {
		return nil, core.Subject{}, nil, func() {}, err
	}
	repository, err := credentialbolt.Open(cmd.Context(), configured.Database, configured.DeploymentID, custodian)
	if err != nil {
		custodian.Close()
		return nil, core.Subject{}, nil, func() {}, err
	}
	authority := credentials.NewAuthority(repository, custodian)
	return service, subject, authority, func() { _ = authority.Close(); custodian.Close() }, nil
}

func openAuthorityForService(ctx context.Context, service *app.Service) (*credentials.Authority, func() error, error) {
	configured := service.Config.Credentials.Authority
	custodianPath, err := custodyPath(configured)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	custodian, err := credentials.LoadFileCustodian(custodianPath)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	repository, err := credentialbolt.Open(ctx, configured.Database, configured.DeploymentID, custodian)
	if err != nil {
		custodian.Close()
		return nil, func() error { return nil }, err
	}
	authority := credentials.NewAuthority(repository, custodian)
	return authority, func() error {
		err := authority.Close()
		custodian.Close()
		return err
	}, nil
}

func custodyPath(configured config.CredentialAuthority) (string, error) {
	switch configured.Custody {
	case "host-file":
		return configured.KEKFile, nil
	case "systemd":
		directory := os.Getenv("CREDENTIALS_DIRECTORY")
		if directory == "" {
			return "", errors.New("systemd credential custody requires CREDENTIALS_DIRECTORY")
		}
		path := filepath.Join(directory, configured.KEKCredential)
		if filepath.Dir(path) != filepath.Clean(directory) || strings.Contains(configured.KEKCredential, string(os.PathSeparator)) {
			return "", errors.New("invalid systemd credential name")
		}
		return path, nil
	default:
		return "", errors.New("credential authority is not configured")
	}
}

func readSecret(cmd *cobra.Command, fromStdin bool, prompt, confirmation string) ([]byte, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return readSecretContext(ctx, cmd, fromStdin, prompt, confirmation)
}

func readSecretContext(ctx context.Context, cmd *cobra.Command, fromStdin bool, prompt, confirmation string) ([]byte, error) {
	if fromStdin {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maximumIntakeBytes+1))
		if err != nil {
			return nil, errors.New("read secret from stdin")
		}
		if len(value) == 0 || len(value) > maximumIntakeBytes {
			wipeSecret(value)
			return nil, errors.New("secret value must be between 1 byte and 1 MiB")
		}
		if err = ctx.Err(); err != nil {
			wipeSecret(value)
			return nil, err
		}
		return value, nil
	}
	file, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, errors.New("no terminal is available for no-echo intake; use --stdin with a protected pipe")
	}
	first, err := readTerminalSecret(ctx, file, cmd.ErrOrStderr(), prompt)
	if err != nil {
		return nil, errors.Join(errors.New("read secret value"), err)
	}
	if len(first) == 0 || len(first) > maximumIntakeBytes {
		wipeSecret(first)
		return nil, errors.New("secret value must be between 1 byte and 1 MiB")
	}
	second, err := readTerminalSecret(ctx, file, cmd.ErrOrStderr(), confirmation)
	if err != nil {
		wipeSecret(first)
		return nil, errors.Join(errors.New("read secret confirmation"), err)
	}
	defer wipeSecret(second)
	if !bytes.Equal(first, second) {
		wipeSecret(first)
		return nil, errors.New("secret confirmation does not match")
	}
	return first, nil
}

func readTerminalSecret(ctx context.Context, file *os.File, output io.Writer, prompt string) (value []byte, err error) {
	return readTerminalSecretBounded(ctx, file, output, prompt, maximumIntakeBytes)
}

func readTerminalSecretBounded(ctx context.Context, file *os.File, output io.Writer, prompt string, maximum int) (value []byte, err error) {
	restore, err := disableTerminalEcho(int(file.Fd()))
	if err != nil {
		return nil, err
	}
	restored := false
	defer func() {
		if !restored {
			_ = restore()
		}
	}()
	fmt.Fprint(output, prompt)
	value, err = readProtectedTerminalLine(ctx, file, maximum)
	restoreErr := restore()
	restored = true
	fmt.Fprintln(output)
	if err != nil {
		return nil, err
	}
	if restoreErr != nil {
		wipeSecret(value)
		return nil, restoreErr
	}
	return value, nil
}

func wipeSecret(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

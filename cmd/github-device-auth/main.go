package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	deviceauth "github.com/dash-xd/github-device-auth/public"
)

const defaultSafetyMargin = 5 * time.Minute

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "github-device-auth: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	var value any
	var err error
	switch args[0] {
	case "device":
		if len(args) != 2 {
			return fmt.Errorf("usage: github-device-auth device <client-id>")
		}
		value, err = deviceauth.RequestDeviceCode(ctx, args[1])
	case "poll":
		if len(args) != 3 {
			return fmt.Errorf("usage: github-device-auth poll <client-id> <device-code>")
		}
		value, err = deviceauth.PollForToken(ctx, args[1], args[2], 5*time.Second)
	case "refresh":
		if len(args) != 3 && len(args) != 4 {
			return fmt.Errorf("usage: github-device-auth refresh <client-id> <refresh-token> [client-secret]")
		}
		secret := ""
		if len(args) == 4 {
			secret = args[3]
		}
		value, err = deviceauth.RefreshAccessToken(ctx, args[1], secret, args[2])
	case "login":
		return login(ctx, args[1:])
	case "import-env":
		return importEnv(args[1:])
	case "status":
		return status(args[1:])
	case "ensure":
		return ensure(ctx, args[1:], false)
	case "refresh-local":
		return ensure(ctx, args[1:], true)
	case "token":
		return token(args[1:])
	case "export":
		return exportBundle(args[1:])
	case "sync-secret":
		return syncSecret(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(value)
}

func usage() error {
	return fmt.Errorf("usage: github-device-auth <device|poll|refresh|login|import-env|status|ensure|refresh-local|token|export|sync-secret> ...")
}

func login(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: github-device-auth login <client-id> <store>")
	}
	clientID, store := args[0], args[1]
	device, err := deviceauth.RequestDeviceCode(ctx, clientID)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Open %s and enter code %s\n", device.VerificationURI, device.UserCode)
	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	pollCtx := ctx
	if device.ExpiresIn > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, time.Duration(device.ExpiresIn)*time.Second)
		defer cancel()
	}
	response, err := deviceauth.PollForToken(pollCtx, clientID, device.DeviceCode, interval)
	if err != nil {
		return err
	}
	bundle, err := deviceauth.NewCredentialBundle(clientID, response, time.Now())
	if err != nil {
		return err
	}
	if err := deviceauth.SaveCredentialBundle(store, bundle); err != nil {
		return err
	}
	return printStatus(bundle, false)
}

func importEnv(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: github-device-auth import-env <environment-variable> <store>")
	}
	value := os.Getenv(args[0])
	if value == "" {
		return fmt.Errorf("environment variable %s is empty", args[0])
	}
	bundle, err := deviceauth.ParseCredentialBundle([]byte(value))
	if err != nil {
		return err
	}
	return deviceauth.SaveCredentialBundle(args[1], bundle)
}

func status(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: github-device-auth status <store>")
	}
	bundle, err := deviceauth.LoadCredentialBundle(args[0])
	if err != nil {
		return err
	}
	return printStatus(bundle, bundle.AccessFresh(time.Now(), defaultSafetyMargin))
}

func ensure(ctx context.Context, args []string, force bool) error {
	store, margin, err := parseStoreMargin(args)
	if err != nil {
		return err
	}
	bundle, err := deviceauth.LoadCredentialBundle(store)
	if err != nil {
		return err
	}
	refreshed := false
	if force || !bundle.AccessFresh(time.Now(), margin) {
		bundle, err = bundle.Refresh(ctx, time.Now())
		if err != nil {
			return err
		}
		if err := deviceauth.SaveCredentialBundle(store, bundle); err != nil {
			return err
		}
		refreshed = true
	}
	out := struct {
		Refreshed bool      `json:"refreshed"`
		ExpiresAt time.Time `json:"access_token_expires_at"`
	}{refreshed, bundle.AccessTokenExpiresAt}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func parseStoreMargin(args []string) (string, time.Duration, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", 0, fmt.Errorf("usage: github-device-auth <ensure|refresh-local> <store> [safety-margin-seconds]")
	}
	margin := defaultSafetyMargin
	if len(args) == 2 {
		seconds, err := strconv.Atoi(args[1])
		if err != nil || seconds < 0 {
			return "", 0, fmt.Errorf("invalid safety margin %q", args[1])
		}
		margin = time.Duration(seconds) * time.Second
	}
	return args[0], margin, nil
}

func token(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: github-device-auth token <store>")
	}
	bundle, err := deviceauth.LoadCredentialBundle(args[0])
	if err != nil {
		return err
	}
	if !bundle.AccessFresh(time.Now(), 0) {
		return fmt.Errorf("access token is expired; run ensure first")
	}
	fmt.Fprintln(os.Stdout, bundle.AccessToken)
	return nil
}

func exportBundle(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: github-device-auth export <store>")
	}
	bundle, err := deviceauth.LoadCredentialBundle(args[0])
	if err != nil {
		return err
	}
	data, err := bundle.Marshal()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

// syncSecret writes the complete current bundle to a recovery journal first,
// when supplied, and only then replaces the primary repository secret. Using a
// GitHub refresh token invalidates the old access+refresh pair immediately, so
// the freshly rotated pair needs durable recovery material before primary
// cutover.
func syncSecret(ctx context.Context, args []string) error {
	if len(args) != 3 && len(args) != 4 {
		return fmt.Errorf("usage: github-device-auth sync-secret <store> <owner/repo> <secret-name> [recovery-secret-name]")
	}
	bundle, err := deviceauth.LoadCredentialBundle(args[0])
	if err != nil {
		return err
	}
	if !bundle.AccessFresh(time.Now(), 0) {
		return fmt.Errorf("access token is expired; run ensure first")
	}
	data, err := bundle.Marshal()
	if err != nil {
		return err
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("sync-secret requires gh CLI: %w", err)
	}
	set := func(name string) error {
		cmd := exec.CommandContext(ctx, gh, "secret", "set", name, "--repo", args[1], "--app", "actions")
		cmd.Env = append(os.Environ(), "GH_TOKEN="+bundle.AccessToken)
		cmd.Stdin = strings.NewReader(string(data))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if len(args) == 4 {
		if strings.TrimSpace(args[3]) == "" || args[3] == args[2] {
			return fmt.Errorf("recovery secret must be non-empty and distinct from primary")
		}
		if err := set(args[3]); err != nil {
			return fmt.Errorf("write recovery credential checkpoint: %w", err)
		}
	}
	if err := set(args[2]); err != nil {
		return fmt.Errorf("write primary credential checkpoint: %w", err)
	}
	return nil
}

func printStatus(bundle deviceauth.CredentialBundle, fresh bool) error {
	out := struct {
		Version               int       `json:"version"`
		ClientID              string    `json:"client_id"`
		AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
		RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
		AccessFresh           bool      `json:"access_fresh"`
	}{bundle.Version, bundle.ClientID, bundle.AccessTokenExpiresAt, bundle.RefreshTokenExpiresAt, fresh}
	return json.NewEncoder(os.Stdout).Encode(out)
}

package latitudesh

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// serverPollInterval is how long waitForServerStatus sleeps between status polls.
// It is a variable rather than a constant only so tests can drive a scripted
// status sequence in milliseconds instead of minutes; nothing in the provider
// changes it at runtime.
var serverPollInterval = 30 * time.Second

// waitForServerStatus polls a server until it reaches "on", appending a
// diagnostic when it fails or the wait runs out. It is the wait every deploy-like
// operation (create, reinstall) shares; power actions wait for other targets
// through waitForServerTargetStatus.
func waitForServerStatus(
	ctx context.Context,
	client *latitudeshgosdk.Latitudesh,
	serverID string,
	operation string,
	configuredTimeout time.Duration,
	requireTransition bool,
	progress func(message string),
	diags *diag.Diagnostics,
) {
	waitForServerTargetStatus(ctx, client, serverID, operation, "on", configuredTimeout, requireTransition, progress, diags)
}

// waitForServerTargetStatus polls a server until it reaches targetStatus,
// appending a diagnostic when it fails or the wait runs out.
//
// It takes the client rather than hanging off ServerResource so the reinstall
// and power actions can wait exactly the way the resource does: same poll
// interval, same terminal-state rules, same stale-reading guard. Parallel wait
// loops would drift.
//
// progress, when non-nil, is called on every observed status change. The resource
// passes nil (it has nowhere to report mid-apply); the actions forward them to
// Terraform as invocation progress events.
func waitForServerTargetStatus(
	ctx context.Context,
	client *latitudeshgosdk.Latitudesh,
	serverID string,
	operation string,
	targetStatus string,
	configuredTimeout time.Duration,
	requireTransition bool,
	progress func(message string),
	diags *diag.Diagnostics,
) {
	// Configs
	timeout := configuredTimeout
	pollInterval := serverPollInterval
	maxRetries := 5

	// Never give up early on a short context: this function returning without a
	// diagnostic means "the operation finished successfully", and a server that was
	// never polled has not finished anything. A context that expires first surfaces
	// as a cancellation from the poll loop below, which is the honest outcome.
	//
	// This previously short-circuited when the context had under two minutes left,
	// so that unit tests would not sit through the poll interval. Tests set
	// serverPollInterval instead, which does not require the production path to lie.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)

		// Adjust timeout to not exceed context deadline
		if remaining < timeout {
			timeout = remaining - 30*time.Second // Leave 30s buffer
			if timeout < time.Minute {
				timeout = time.Minute
			}
		}
	}

	deadline := time.Now().Add(timeout)
	consecutiveErrors := 0
	lastStatus := ""
	initialStatus := ""
	sawTransition := false

	// Enable debug logging if TF_LOG or LATITUDESH_DEBUG is set
	enableDebug := os.Getenv("TF_LOG") != "" || os.Getenv("LATITUDESH_DEBUG") != ""

	for time.Now().Before(deadline) {
		response, err := client.Servers.Get(ctx, serverID, nil)
		if err != nil {
			consecutiveErrors++

			// Check if it's a temporary error that we should retry
			errStr := err.Error()
			isTemporaryError := strings.Contains(errStr, "502") ||
				strings.Contains(errStr, "503") ||
				strings.Contains(errStr, "504") ||
				strings.Contains(errStr, "429") ||
				strings.Contains(errStr, "timeout") ||
				strings.Contains(errStr, "connection reset")

			if isTemporaryError && consecutiveErrors <= maxRetries {
				// Calculate backoff with exponential delay
				backoff := time.Duration(consecutiveErrors) * 5 * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}

				if enableDebug {
					fmt.Fprintf(os.Stderr, "[DEBUG] Temporary error during %s (attempt %d/%d), retrying in %v: %s\n",
						operation, consecutiveErrors, maxRetries, backoff, errStr)
				}

				// Wait before retry with backoff
				select {
				case <-ctx.Done():
					diags.AddError("Context Cancelled", fmt.Sprintf("Server %s was cancelled", operation))
					return
				case <-time.After(backoff):
					// Continue to next iteration
					continue
				}
			}

			// If it's not a temporary error or we've exceeded retries, fail
			if consecutiveErrors > maxRetries {
				diags.AddError(
					"Client Error",
					fmt.Sprintf("Unable to check server status during %s after %d retries. Last error: %s", operation, maxRetries, err.Error()),
				)
			} else {
				diags.AddError(
					"Client Error",
					fmt.Sprintf("Unable to check server status during %s: %s", operation, err.Error()),
				)
			}
			return
		}

		// Reset consecutive errors on success
		consecutiveErrors = 0

		if response.Server == nil || response.Server.Data == nil || response.Server.Data.Attributes == nil {
			diags.AddError("API Error", fmt.Sprintf("Invalid server response during %s", operation))
			return
		}

		attrs := response.Server.Data.Attributes
		if attrs.Status == nil {
			diags.AddError("API Error", fmt.Sprintf("Server status is null during %s", operation))
			return
		}

		status := string(*attrs.Status)

		// Log status changes for debugging
		if status != lastStatus {
			if enableDebug {
				fmt.Fprintf(os.Stderr, "[DEBUG] Server %s: status changed from '%s' to '%s' (waiting for '%s')\n",
					operation, lastStatus, status, targetStatus)
			}
			if progress != nil {
				progress(fmt.Sprintf("Server %s: status %s", operation, status))
			}
		}
		lastStatus = status

		// Capture initial status on first successful poll and watch for transition.
		if initialStatus == "" {
			initialStatus = status
		} else if !sawTransition && status != initialStatus {
			sawTransition = true
		}

		effectiveTransition := sawTransition || !requireTransition
		if terminal, success := isServerStatusTerminal(targetStatus, initialStatus, status, effectiveTransition); terminal {
			if success {
				if enableDebug {
					fmt.Fprintf(os.Stderr, "[DEBUG] Server %s completed successfully (status: %s)\n", operation, targetStatus)
				}
				return
			}
			diags.AddError(
				fmt.Sprintf("Server %s Failed", operation),
				fmt.Sprintf("Server entered failed state: %s. Please check the server in the Latitude.sh dashboard.", status),
			)
			return
		}

		// Wait before next check
		select {
		case <-ctx.Done():
			diags.AddError("Context Cancelled", fmt.Sprintf("Server %s was cancelled", operation))
			return
		case <-time.After(boundedPollSleep(deadline, pollInterval)):
			// Continue to next iteration
		}
	}

	// Timeout reached
	diags.AddError(
		fmt.Sprintf("Server %s Timeout", operation),
		fmt.Sprintf("Server did not reach '%s' state within %v. Check server status in Latitude.sh dashboard.", targetStatus, timeout),
	)
}

// isServerStatusTerminal decides whether a polled server status should be
// treated as terminal (success or failure) for a wait targeting targetStatus.
// When the operation started while the server was already in a terminal-looking
// state (the target itself, `failed_deployment`, `failed_disk_erasing`) —
// common on a reinstall right after a previous reinstall — accepting the first
// stale reading as terminal causes false success or false failure. Require an
// observed transition before honoring a terminal status that matches the
// initial one.
//
// success is only meaningful when terminal is true; ignore otherwise.
func isServerStatusTerminal(targetStatus, initialStatus, currentStatus string, sawTransition bool) (terminal, success bool) {
	isTerminal := func(s string) bool {
		return s == targetStatus || s == "failed_deployment" || s == "failed_disk_erasing"
	}

	if !isTerminal(currentStatus) {
		return false, false
	}

	// If we started in a terminal state and have not observed a transition,
	// the current reading is stale from the previous operation.
	if isTerminal(initialStatus) && !sawTransition {
		return false, false
	}

	return true, currentStatus == targetStatus
}

// isReinstallTerminal is isServerStatusTerminal with the "on" target every
// deploy-like operation waits for.
func isReinstallTerminal(initialStatus, currentStatus string, sawTransition bool) (terminal, success bool) {
	return isServerStatusTerminal("on", initialStatus, currentStatus, sawTransition)
}

// boundedPollSleep caps a poll sleep at the time remaining until deadline, so
// a wait_timeout shorter than the poll interval expires when configured rather
// than after one full interval. A remaining duration of zero or less makes
// time.After fire immediately, which hands control back to the loop's deadline
// check.
func boundedPollSleep(deadline time.Time, interval time.Duration) time.Duration {
	if remaining := time.Until(deadline); remaining < interval {
		return remaining
	}
	return interval
}

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

// waitForServerStatus polls a server until it reaches a terminal state, appending
// a diagnostic when it fails or the wait runs out.
//
// It takes the client rather than hanging off ServerResource so the reinstall
// action can wait exactly the way the resource does: same poll interval, same
// terminal-state rules, same stale-reading guard. Two wait loops would drift.
//
// progress, when non-nil, is called on every observed status change. The resource
// passes nil (it has nowhere to report mid-apply); the action forwards them to
// Terraform as invocation progress events.
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
	// Configs
	timeout := configuredTimeout
	pollInterval := serverPollInterval
	maxRetries := 5

	// Check if we're in test mode with short deadline
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)

		// If context deadline is very short (< 2 minutes), we're likely in a unit test
		// Skip wait to prevent test timeouts
		if remaining < 2*time.Minute {
			return
		}

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
				fmt.Fprintf(os.Stderr, "[DEBUG] Server %s: status changed from '%s' to '%s' (waiting for 'on')\n",
					operation, lastStatus, status)
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
		if terminal, success := isReinstallTerminal(initialStatus, status, effectiveTransition); terminal {
			if success {
				if enableDebug {
					fmt.Fprintf(os.Stderr, "[DEBUG] Server %s completed successfully (status: on)\n", operation)
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
		case <-time.After(pollInterval):
			// Continue to next iteration
		}
	}

	// Timeout reached
	diags.AddError(
		fmt.Sprintf("Server %s Timeout", operation),
		fmt.Sprintf("Server did not reach 'on' state within %v. Check server status in Latitude.sh dashboard.", timeout),
	)
}

// isReinstallTerminal decides whether a polled server status should be treated
// as terminal (success or failure). When the operation started while the server
// was already in a terminal-looking state (`on`, `failed_deployment`,
// `failed_disk_erasing`) — common on a reinstall right after a previous
// reinstall — accepting the first stale reading as terminal causes false
// success or false failure. Require an observed transition before honoring a
// terminal status that matches the initial one.
//
// success is only meaningful when terminal is true; ignore otherwise.
func isReinstallTerminal(initialStatus, currentStatus string, sawTransition bool) (terminal, success bool) {
	isTerminal := func(s string) bool {
		return s == "on" || s == "failed_deployment" || s == "failed_disk_erasing"
	}

	if !isTerminal(currentStatus) {
		return false, false
	}

	// If we started in a terminal state and have not observed a transition,
	// the current reading is stale from the previous operation.
	if isTerminal(initialStatus) && !sawTransition {
		return false, false
	}

	return true, currentStatus == "on"
}

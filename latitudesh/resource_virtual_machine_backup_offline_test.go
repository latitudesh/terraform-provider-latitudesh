package latitudesh

import (
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// virtualMachineBackupPayload is a plausible GET /virtual_machine_backups/{id}
// response, shaped from the SDK's components.VirtualMachineBackupAttributes*
// json tags and doc comments (v1.19.20). NOT a captured real payload: per
// sdk-coverage.yaml, the API checkout reviewed alongside this SDK version has
// no HTTP routes for this domain at all, so there is nothing live to record
// from yet — confirm this shape against the real API before relying on it.
const virtualMachineBackupPayload = `{
  "data": {
    "id": "vmb_test123",
    "type": "virtual_machine_backups",
    "attributes": {
      "status": "Ready",
      "size_bytes": 10737418240,
      "expires_at": "2026-09-11T00:00:00Z",
      "created_at": "2026-09-04T00:00:00Z",
      "virtual_machine": {"id": "vm_abc123", "name": "bastion"},
      "team": {"id": "team_1", "name": "Acme"},
      "project": {"id": "proj_1", "slug": "acme-prod"}
    }
  }
}`

func TestVirtualMachineBackupPayloadUnmarshal(t *testing.T) {
	var backup components.VirtualMachineBackup
	if err := json.Unmarshal([]byte(virtualMachineBackupPayload), &backup); err != nil {
		t.Fatalf("unmarshaling virtual machine backup payload: %s", err)
	}
	if backup.Data == nil || backup.Data.ID == nil {
		t.Fatal("expected non-nil data.id")
	}
	if *backup.Data.ID != "vmb_test123" {
		t.Errorf("id = %q, want vmb_test123", *backup.Data.ID)
	}
}

func TestMapVirtualMachineBackupAttrs(t *testing.T) {
	var backup components.VirtualMachineBackup
	if err := json.Unmarshal([]byte(virtualMachineBackupPayload), &backup); err != nil {
		t.Fatalf("unmarshaling payload: %s", err)
	}

	got := mapVirtualMachineBackupAttrs(backup.Data.Attributes)

	if got.VirtualMachine.ValueString() != "vm_abc123" {
		t.Errorf("virtual_machine = %q, want vm_abc123", got.VirtualMachine.ValueString())
	}
	if got.Status.ValueString() != "Ready" {
		t.Errorf("status = %q, want Ready", got.Status.ValueString())
	}
	if got.SizeBytes.ValueInt64() != 10737418240 {
		t.Errorf("size_bytes = %d, want 10737418240", got.SizeBytes.ValueInt64())
	}
	if got.ExpiresAt.ValueString() != "2026-09-11T00:00:00Z" {
		t.Errorf("expires_at = %q, want 2026-09-11T00:00:00Z", got.ExpiresAt.ValueString())
	}
	if got.CreatedAt.ValueString() != "2026-09-04T00:00:00Z" {
		t.Errorf("created_at = %q, want 2026-09-04T00:00:00Z", got.CreatedAt.ValueString())
	}
	if !got.FailureReason.IsNull() {
		t.Errorf("failure_reason = %q, want null (not present in payload)", got.FailureReason.ValueString())
	}
}

// TestMapVirtualMachineBackupAttrsNil guards against a nil Attributes pointer
// (e.g. a malformed or partial response) producing typed nulls rather than a
// panic, mirroring the nil-safety the rest of the provider relies on.
func TestMapVirtualMachineBackupAttrsNil(t *testing.T) {
	got := mapVirtualMachineBackupAttrs(nil)

	if !got.VirtualMachine.IsNull() || !got.Status.IsNull() || !got.SizeBytes.IsNull() ||
		!got.ExpiresAt.IsNull() || !got.FailureReason.IsNull() || !got.CreatedAt.IsNull() {
		t.Errorf("mapVirtualMachineBackupAttrs(nil) = %+v, want all-null fields", got)
	}
}

// TestVirtualMachineBackupFailedStatusPayload exercises the Failed terminal
// status with a failure_reason, the shape waitForBackupReady surfaces as an
// error instead of retrying.
func TestVirtualMachineBackupFailedStatusPayload(t *testing.T) {
	const payload = `{
	  "data": {
	    "id": "vmb_failed1",
	    "type": "virtual_machine_backups",
	    "attributes": {
	      "status": "Failed",
	      "failure_reason": "insufficient disk space on host"
	    }
	  }
	}`

	var backup components.VirtualMachineBackup
	if err := json.Unmarshal([]byte(payload), &backup); err != nil {
		t.Fatalf("unmarshaling failed-status payload: %s", err)
	}

	got := mapVirtualMachineBackupAttrs(backup.Data.Attributes)
	if got.Status.ValueString() != "Failed" {
		t.Errorf("status = %q, want Failed", got.Status.ValueString())
	}
	if got.FailureReason.ValueString() != "insufficient disk space on host" {
		t.Errorf("failure_reason = %q, want %q", got.FailureReason.ValueString(), "insufficient disk space on host")
	}
}

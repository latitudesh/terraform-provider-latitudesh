package latitudesh

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestVirtualMachineRestoreNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "APIError with 404 status code",
			err:  components.NewAPIError("not found", http.StatusNotFound, "", nil),
			want: true,
		},
		{
			name: "APIError with a non-404 status code",
			err:  components.NewAPIError("boom", http.StatusInternalServerError, "", nil),
			want: false,
		},
		{
			name: "unrelated error",
			err:  fmt.Errorf("connection reset"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := virtualMachineRestoreNotFound(tc.err); got != tc.want {
				t.Errorf("virtualMachineRestoreNotFound() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMapVirtualMachineRestoreToModel_NilAttributes(t *testing.T) {
	restore := &components.VirtualMachineRestoreAttributes{
		ID:         stringPtr("vmr_1"),
		Attributes: nil,
	}

	var data VirtualMachineRestoreDataSourceModel
	mapVirtualMachineRestoreToModel(restore, &data)

	if data.ID.ValueString() != "vmr_1" {
		t.Errorf("ID = %q, want vmr_1", data.ID.ValueString())
	}
	if !data.Status.IsNull() || !data.VirtualMachineID.IsNull() || !data.Project.IsNull() {
		t.Error("expected all attribute fields to be null when Attributes is nil")
	}
}

// TestMapVirtualMachineRestoreToModel_VirtualMachineNullUntilReady guards the
// SDK's documented behavior: VirtualMachine is null while the restore is
// still Creating, so the mapped fields must not fabricate a value.
func TestMapVirtualMachineRestoreToModel_VirtualMachineNullUntilReady(t *testing.T) {
	status := components.VirtualMachineRestoreAttributesStatusCreating
	restore := &components.VirtualMachineRestoreAttributes{
		ID: stringPtr("vmr_2"),
		Attributes: &components.VirtualMachineRestoreAttributesAttributes{
			Status:         &status,
			VirtualMachine: nil,
		},
	}

	var data VirtualMachineRestoreDataSourceModel
	mapVirtualMachineRestoreToModel(restore, &data)

	if data.Status.ValueString() != "Creating" {
		t.Errorf("Status = %q, want Creating", data.Status.ValueString())
	}
	if !data.VirtualMachineID.IsNull() || !data.VirtualMachineName.IsNull() {
		t.Error("expected virtual_machine_id/name to stay null while restore is Creating")
	}
}

func TestMapVirtualMachineRestoreToModel_Ready(t *testing.T) {
	status := components.VirtualMachineRestoreAttributesStatusReady
	createdAt := "2026-08-01T00:00:00Z"
	backupID := "backup_1"
	vmID := "vm_1"
	vmName := "restored-vm"
	slug := "my-project"
	projectID := "proj_1"

	restore := &components.VirtualMachineRestoreAttributes{
		ID: stringPtr("vmr_3"),
		Attributes: &components.VirtualMachineRestoreAttributesAttributes{
			Status:    &status,
			CreatedAt: &createdAt,
			Backup:    &components.Backup{ID: &backupID},
			VirtualMachine: &components.VirtualMachineRestoreAttributesVirtualMachine{
				ID:   &vmID,
				Name: &vmName,
			},
			Project: &components.ProjectInclude{Slug: &slug, ID: &projectID},
		},
	}

	var data VirtualMachineRestoreDataSourceModel
	mapVirtualMachineRestoreToModel(restore, &data)

	if data.Status.ValueString() != "Ready" {
		t.Errorf("Status = %q, want Ready", data.Status.ValueString())
	}
	if data.CreatedAt.ValueString() != createdAt {
		t.Errorf("CreatedAt = %q, want %q", data.CreatedAt.ValueString(), createdAt)
	}
	if data.BackupID.ValueString() != backupID {
		t.Errorf("BackupID = %q, want %q", data.BackupID.ValueString(), backupID)
	}
	if data.VirtualMachineID.ValueString() != vmID {
		t.Errorf("VirtualMachineID = %q, want %q", data.VirtualMachineID.ValueString(), vmID)
	}
	if data.VirtualMachineName.ValueString() != vmName {
		t.Errorf("VirtualMachineName = %q, want %q", data.VirtualMachineName.ValueString(), vmName)
	}
	// Project prefers slug over id when both are present.
	if data.Project.ValueString() != slug {
		t.Errorf("Project = %q, want %q (slug preferred over id)", data.Project.ValueString(), slug)
	}
}

func TestMapVirtualMachineRestoreToModel_ProjectFallsBackToID(t *testing.T) {
	status := components.VirtualMachineRestoreAttributesStatusReady
	projectID := "proj_2"

	restore := &components.VirtualMachineRestoreAttributes{
		ID: stringPtr("vmr_4"),
		Attributes: &components.VirtualMachineRestoreAttributesAttributes{
			Status:  &status,
			Project: &components.ProjectInclude{ID: &projectID},
		},
	}

	var data VirtualMachineRestoreDataSourceModel
	mapVirtualMachineRestoreToModel(restore, &data)

	if data.Project.ValueString() != projectID {
		t.Errorf("Project = %q, want %q", data.Project.ValueString(), projectID)
	}
}

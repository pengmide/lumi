package docker

import "github.com/docker/docker/api/types/filters"

const (
	LabelRuntime     = "lumi.runtime"
	LabelWorkspaceID = "lumi.workspace_id"
	LabelDeviceID    = "lumi.device_id"
	LabelRuntimeType = "sandbox"
)

func BuildLabels(workspaceID string, deviceID string) map[string]string {
	return map[string]string{
		LabelRuntime:     LabelRuntimeType,
		LabelWorkspaceID: workspaceID,
		LabelDeviceID:    deviceID,
	}
}

func SandboxFilters() filters.Args {
	args := filters.NewArgs()
	args.Add("label", LabelRuntime+"="+LabelRuntimeType)
	return args
}

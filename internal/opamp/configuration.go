package opamp

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	servertypes "github.com/open-telemetry/opamp-go/server/types"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

// reportedConfigState retains the connection's last observation, including
// failures whose hash alone must not suppress an explicitly reselected target.
type reportedConfigState struct {
	hash   []byte
	status protobufs.RemoteConfigStatuses
}

// remoteConfigOffer applies the same acknowledgement and failure policy to
// ordinary responses and proactive notifications.
func remoteConfigOffer(status remoteconfig.ConfigurationStatus, reported reportedConfigState) *protobufs.AgentRemoteConfig {
	desired := status.Desired
	if status.State == remoteconfig.ApplyStatusFailed ||
		(reported.status != protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED && bytes.Equal(reported.hash, desired.ConfigHash[:])) {
		return nil
	}
	return &protobufs.AgentRemoteConfig{
		Config: &protobufs.AgentConfigMap{ConfigMap: map[string]*protobufs.AgentConfigObject{
			"": {Body: desired.Content},
		}},
		ConfigHash: desired.ConfigHash[:],
	}
}

func (handler *connectionHandler) recordRemoteConfigStatus(
	ctx context.Context,
	agentUID agent.InstanceUID,
	message *protobufs.AgentToServer,
	reportedAt time.Time,
) error {
	reportsRemoteConfig := uint64(
		protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig,
	)
	reportedStatus := message.GetRemoteConfigStatus()
	if message.GetCapabilities()&reportsRemoteConfig == 0 || reportedStatus == nil {
		return nil
	}

	var applyStatus remoteconfig.ApplyStatus
	switch reportedStatus.GetStatus() {
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_UNSET:
		return nil
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING:
		applyStatus = remoteconfig.ApplyStatusApplying
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED:
		applyStatus = remoteconfig.ApplyStatusApplied
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED:
		applyStatus = remoteconfig.ApplyStatusFailed
	default:
		return nil
	}

	if err := handler.configStore.RecordStatus(ctx, agentUID, remoteconfig.StatusReport{
		ConfigHash:   reportedStatus.GetLastRemoteConfigHash(),
		Status:       applyStatus,
		ErrorMessage: reportedStatus.GetErrorMessage(),
		ReportedAt:   reportedAt,
	}); err != nil {
		return fmt.Errorf("record reported remote configuration status: %w", err)
	}

	return nil
}

// reportedConfig retains compressed status only for this live connection.
// An explicit empty report replaces the previous state; an omitted report does not.
func (handler *connectionHandler) reportedConfig(connection servertypes.Connection, report *protobufs.RemoteConfigStatus) reportedConfigState {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if report != nil {
		handler.reportedConfigs[connection] = reportedConfigState{
			hash: bytes.Clone(report.GetLastRemoteConfigHash()), status: report.GetStatus(),
		}
	}
	state := handler.reportedConfigs[connection]
	state.hash = bytes.Clone(state.hash)
	return state
}

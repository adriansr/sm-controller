package sm

import (
	sm_protos "github.com/grafana/synthetic-monitoring-agent/pkg/pb/synthetic_monitoring"
)

type RawCheck = sm_protos.Check
type TcpSettings = sm_protos.TcpSettings

const (
	IpVersion_V4 = sm_protos.IpVersion_V4
)

type Check struct {
	RawCheck

	Probes []string // Override probes as list of string
}

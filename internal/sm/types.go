package sm

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	sm_protos "github.com/grafana/synthetic-monitoring-agent/pkg/pb/synthetic_monitoring"
)

type RawCheck = sm_protos.Check
type TcpSettings = sm_protos.TcpSettings
type Probe = sm_protos.Probe
type Label = sm_protos.Label

const (
	IpVersion_V4 = sm_protos.IpVersion_V4

	// TODO: Move somewhere else
	ManagedLabel = "managed_by"
	ManagedValue = "k8s-controller" // TODO: Mark this deployment
)

type Check struct {
	RawCheck

	Probes []string // Override probes as list of string
}

type CheckSet map[string]*Check
type ProbeSet map[string]*Probe

func NewCheckSet(checks []*Check) (CheckSet, error) {
	set := CheckSet{}
	for _, c := range checks {
		if set[c.Job] != nil {
			return nil, fmt.Errorf("duplicate check: %s", c.Job)
		}
		set[c.Job] = c
	}
	return set, nil
}

func (s CheckSet) Equals(o CheckSet) bool {
	if len(s) != len(o) {
		return false
	}
	for job, check := range s {
		if other, found := o[job]; !found || !check.Equals(other) {
			return false
		}
	}
	return true
}

func (c *Check) Equals(o *Check) bool {
	// TODO: Ignore fields that API populates?
	// TODO: What about probe names vs IDs ???
	cRaw, oRaw := normalize(c.RawCheck), normalize(o.RawCheck)
	return reflect.DeepEqual(cRaw, oRaw)
}

func normalize(c RawCheck) RawCheck {
	c.Id = 0
	c.TenantId = 0
	c.Modified = 0
	c.Created = 0
	sort.SliceStable(c.Probes, func(i, j int) bool {
		return c.Probes[i] < c.Probes[j]
	})
	return c
}

func (c *Check) IsManaged() bool {
	for _, label := range c.Labels {
		if label.Name == ManagedLabel && label.Value == ManagedValue {
			return true
		}
	}
	return false
}

func (c *Check) MarkManaged() {
	if !c.IsManaged() {
		c.Labels = append(c.Labels, Label{
			Name:  ManagedLabel,
			Value: ManagedValue,
		})
	}
}

func (c *Check) ResolveProbeIDs(probes ProbeSet) error {
	c.RawCheck.Probes = make([]int64, 0, len(c.Probes))
	for _, name := range c.Probes {
		probe, found := probes[strings.ToLower(name)]
		if !found {
			return fmt.Errorf("check %s references probe %s that doesn't exist", c.Job, name)
		}
		c.RawCheck.Probes = append(c.RawCheck.Probes, probe.Id)
	}
	return nil
}

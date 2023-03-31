package builder

import (
	"strconv"
	"strings"

	sm "github.com/grafana/synthetic-monitoring-agent/pkg/pb/synthetic_monitoring"
)

const (
	AnnotationsPrefix   = "synthetics.grafana.com/"
	EnabledAnnotation   = AnnotationsPrefix + "enabled"
	NameAnnotation      = AnnotationsPrefix + "name"
	FrequencyAnnotation = AnnotationsPrefix + "frequency"
	TimeoutAnnotation   = AnnotationsPrefix + "timeout"
	ProbesAnnotation    = AnnotationsPrefix + "probes"
	HostAnnotation      = AnnotationsPrefix + "host" // TODO
)

var defaultCheckOptions = CheckOptions{
	Frequency: 60000,
	Timeout:   3000,
	Probes:    []string{"Atlanta", "NewYork", "Paris", "Singapore"},
}

type Options struct {
	// TODO: Config options for how the checks are built
	ClusterName string
	Labels      []sm.Label
	defaults    CheckOptions
}

func NewOptions() Options {
	return Options{
		defaults: defaultCheckOptions,
	}
}

type CheckOptions struct {
	// These translate directly to check fields:
	Enabled   bool
	JobName   string
	Frequency int64
	Timeout   int64
	Labels    []sm.Label
	Probes    []string

	// These are modifiers:
	Host   string
	Target string
}

func (opt *Options) NewCheckOptions(annotations map[string]string) (opts CheckOptions) {
	opts = opt.defaults
	if enabled, err := strconv.ParseBool(annotations[EnabledAnnotation]); err == nil {
		opts.Enabled = enabled
	}
	if name := annotations[NameAnnotation]; name != "" {
		opts.JobName = name
	}
	if freq, err := strconv.ParseUint(annotations[FrequencyAnnotation], 10, 32); err == nil {
		opts.Frequency = int64(freq)
	}
	if timeout, err := strconv.ParseUint(annotations[TimeoutAnnotation], 10, 32); err == nil {
		opts.Timeout = int64(timeout)
	}
	if probes := strings.Split(annotations[ProbesAnnotation], ","); len(probes) > 0 && len(probes[0]) > 0 {
		opts.Probes = probes
	}
	opts.Host = annotations[HostAnnotation]

	return opts
}

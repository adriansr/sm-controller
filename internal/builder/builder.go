package builder

import (
	"fmt"
	"strconv"

	coreV1 "k8s.io/api/core/v1"
	networkingV1 "k8s.io/api/networking/v1"

	"github.com/adriansr/sm-controller/internal/schema"
	"github.com/adriansr/sm-controller/internal/sm"
)

type Builder struct {
	options Options
}

func NewBuilder(opts Options) Builder {
	return Builder{
		options: opts,
	}
}

func (b *Builder) Build(services []*coreV1.Service, ingresses []*networkingV1.Ingress) (checks []*sm.Check, warnings []Warning) {
	if len(services) == 0 {
		warnings = append(warnings, Warning{
			Cause: fmt.Errorf("no services annotated for monitoring"),
		})
		return nil, warnings
	}

	for _, svc := range services {
		svcChecks, err := b.toChecks(svc, nil)
		if err != nil {
			scObj, _ := schema.ObjectFrom(svc)
			warnings = append(warnings, Warning{
				Cause: err,
				Objs:  []schema.Object{scObj},
			})
		}

		checks = append(checks, svcChecks...)
	}

	return checks, warnings
}

type Warning struct {
	Cause error
	Objs  []schema.Object
}

func (b *Builder) toChecks(svc *coreV1.Service, _TODO_ []*networkingV1.Ingress) (checks []*sm.Check, err error) {
	opts := b.options.NewCheckOptions(svc.GetAnnotations())
	if !opts.Enabled {
		return nil, nil
	}

	var hosts []string

	if opts.Host != "" {
		hosts = append(hosts, opts.Host)
	} else {
		for _, ip := range svc.Spec.ExternalIPs {
			hosts = append(hosts, ip)
		}
	}

	for _, host := range hosts {
		for _, port := range svc.Spec.Ports {
			check, err := opts.checkForHostPort(svc, host, port)
			if err != nil {
				return nil, err
			}
			checks = append(checks, check)
		}
	}
	return checks, nil
}

func (opts *CheckOptions) checkForHostPort(svc *coreV1.Service, host string, port coreV1.ServicePort) (*sm.Check, error) {
	check := &sm.Check{
		RawCheck: sm.RawCheck{
			Enabled:   true,
			Frequency: opts.Frequency,
			Timeout:   opts.Timeout,
			Labels:    opts.Labels, // TODO: + other labels
			Job:       opts.JobName,
		},

		Probes: opts.Probes, // Override

		// TODO: BasicMetricsOnly: false,
		// TODO: AlertSensitivity: "",
	}

	portName := port.Name
	if portName == "" {
		portName = strconv.Itoa(int(port.Port))
	}
	if check.Job == "" {
		check.Job = fmt.Sprintf("%s_%s/%s_%s:%s/%s",
			"k8s", // TODO: Context
			svc.Namespace,
			svc.Name,
			host,
			portName,
			port.Protocol,
		)
	}
	switch port.Protocol {
	case "TCP":
		check.Settings.Tcp = &sm.TcpSettings{
			IpVersion: sm.IpVersion_V4,
		}
	case "UDP", "SCTP":
		// TODO: Ignorable error for logging
		return nil, nil
	}
	return check, nil
}

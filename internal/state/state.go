package state

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adriansr/sm-controller/internal/builder"
	"github.com/adriansr/sm-controller/internal/helpers/timer"
	"github.com/adriansr/sm-controller/internal/schema"
	"github.com/adriansr/sm-controller/internal/sm"
	"github.com/adriansr/sm-controller/internal/watchers"
	"github.com/rs/zerolog"
	coreV1 "k8s.io/api/core/v1"
	networkingV1 "k8s.io/api/networking/v1"

	client "github.com/grafana/synthetic-monitoring-api-go-client"
)

type Version uint32

type ClusterState struct {
	Services  []*coreV1.Service
	Ingresses []*networkingV1.Ingress
	Version   Version
	Force     bool
}

type Publisher interface {
	Publish(ClusterState)
}

type State struct {
	C         <-chan watchers.Event
	Logger    zerolog.Logger
	Publisher Publisher

	internalState map[string]schema.Object
	lastPublished Version
}

func (s *State) publish(forced bool) {
	s.lastPublished++
	update := ClusterState{
		Version: s.lastPublished,
		Force:   forced,
	}

	for _, obj := range s.internalState {
		switch v := obj.Inner().(type) {
		case *coreV1.Service:
			update.Services = append(update.Services, v)
		case *networkingV1.Ingress:
			update.Ingresses = append(update.Ingresses, v)
		default:
			panic(fmt.Errorf("unexpected type: %T", v))
		}
	}

	s.Publisher.Publish(update)
}

func (s *State) Run(ctx context.Context) error {

	const (
		minSync     = "minSync"
		maxSync     = "maxSync"
		initialSync = "initialSync"
		forcedSync  = "forcedSync"

		// State will be synched with API when ...

		// ... minSync deadline has passed since receiving the last k8s event
		minSyncTimeout = time.Second * 5

		// ... or maxSync deadline has passed since receiving the first k8s event
		maxSyncTimeout = time.Second * 30

		// ... or initialSync deadline has passed without receiving any events after start
		initialSyncTimeout = time.Second * 30

		// ... for forcedSync deadline has passed without receiving any event since last sync
		forcedSyncTimeout = time.Hour * 3
	)

	var deadlines timer.MultiTimer
	deadlines.Set(initialSync, time.Now().Add(initialSyncTimeout))

	if s.internalState == nil {
		s.internalState = make(map[string]schema.Object)
	}
	for {
		select {
		case reason := <-deadlines.C():
			s.Logger.Info().Interface("reason", reason).Msg("Sync triggered")
			force := reason == forcedSync
			s.publish(force)

			deadlines.Reset()
			deadlines.Set(forcedSync, time.Now().Add(forcedSyncTimeout))

		case ev := <-s.C:
			key := ev.Obj.ID()
			s.Logger.Info().Str("action", ev.Action.String()).Str("id", key).Msg("received event")
			switch ev.Action {
			case watchers.Add, watchers.Update:
				s.internalState[key] = ev.Obj
			case watchers.Delete:
				delete(s.internalState, key)
			}

			if !deadlines.IsSet(maxSync) {
				deadlines.Set(maxSync, time.Now().Add(maxSyncTimeout))
			}
			deadlines.Clear(initialSync, forcedSync)
			deadlines.Set(minSync, time.Now().Add(minSyncTimeout))

		case <-ctx.Done():
			s.Logger.Info().Msg("Terminated")
			return ctx.Err()
		}
	}
}

// TODO move rename cleanup
type Consolidator struct {
	mu     sync.Mutex
	Logger *zerolog.Logger

	newState ClusterState
	syncing  bool

	//knownChecks sm.CheckSet
	RequestTimeout time.Duration

	ApiServer string
	ApiToken  string
}

func (p *Consolidator) Publish(cs ClusterState) {
	p.Logger.Info().Msgf("Received cluster state v%d", cs.Version)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.newState = cs
	if !p.syncing {
		p.syncing = true
		go p.sync()
	}
}

func (p *Consolidator) getCS() ClusterState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.newState
}

func (p *Consolidator) sync() {
	defer func() {
		p.mu.Lock()
		p.syncing = false
		defer p.mu.Unlock()
	}()

	var lastSynced Version
	for cs := p.getCS(); cs.Version != lastSynced; cs = p.getCS() {
		log := p.Logger.With().Interface("version", cs.Version).Logger()
		log.Debug().Msg("starting sync")
		if err := p.syncState(cs); err != nil {
			log.Err(err).Msg("sync failed")
			//lastSynced = cs.Version
			//continue
			return
		}
		lastSynced = cs.Version
		log.Info().Msg("Sync completed")
	}
}

func (p *Consolidator) syncState(cs ClusterState) error {
	logger := p.Logger.With().Interface("version", cs.Version).Logger()
	logger.Info().
		Int("num_services", len(cs.Services)).
		Int("num_ingresses", len(cs.Ingresses)).
		Msg("Starting sync")

	bld := builder.NewBuilder(builder.NewOptions())
	checks, warns := bld.Build(cs.Services, cs.Ingresses)

	logger.Debug().Int("num_checks", len(checks)).Int("warnings", len(warns)).Msg("check build finished")

	if len(warns) > 0 {
		logger.Warn().Int("count", len(warns)).Msg("check build resulted in warnings")
		for idx, w := range warns {
			var ids []string
			for _, obj := range w.Objs {
				ids = append(ids, obj.ID())
			}
			logger.Warn().Int("warning", idx).Interface("resources", ids).Msg(w.Cause.Error())
		}
	}
	for idx, check := range checks {
		p.Logger.Debug().Int("number", idx).Msgf("%+v", check)
	}

	api, err := p.getAPIObjects()
	if err != nil {
		return fmt.Errorf("fetching state from synthetic-monitoring API: %w", err)
	}
	for key, probe := range api.probes {
		logger.Debug().Msgf("API: got probe[%s] = %d", key, probe.Id)
	}
	for key, check := range api.checks {
		logger.Debug().Msgf("API: got check[%s] = %+v", key, check.RawCheck)
	}

	for _, newCheck := range checks {
		if err := newCheck.ResolveProbeIDs(api.probes); err != nil {
			// TODO: Only err current check!
			return err
		}
	}

	set, err := sm.NewCheckSet(checks)
	if err != nil {
		// Should only happen if we create repeated job names
		return fmt.Errorf("error in generated check set: %w", err)
	}

	if !cs.Force && api.checks.Equals(set) {
		logger.Info().Msg("Skipping sync: no changes")
		return nil
	}

	var add, update, del []*sm.Check
	for jobName, check := range set {
		known, found := api.checks[jobName]
		if !found {
			add = append(add, check)
			continue
		}
		if check.Equals(known) {
			continue
		}
		check.Id = known.Id
		check.TenantId = known.TenantId
		check.Created = known.Created
		check.Modified = 0 // known.Modified
		check.Labels = known.Labels
		check.MarkManaged()
		update = append(update, check)
	}

	for jobName, existing := range api.checks {
		if _, found := set[jobName]; !found {
			del = append(del, existing)
		}
	}

	logger.Info().
		Int("added", len(add)).
		Int("updated", len(update)).
		Int("removed", len(del)).
		Msg("Starting reconciliation")

	baseClient := http.DefaultClient
	cli := client.NewClient(p.ApiServer, p.ApiToken, baseClient)

	for _, check := range del {
		logger.Debug().Int64("id", check.Id).Str("job", check.Job).Msg("Deleting check")

		if _, err := withTimeout(context.TODO(), p.RequestTimeout, func(ctx context.Context) (int64, error) {
			return check.Id, cli.DeleteCheck(ctx, check.Id)
		}); err != nil {
			return fmt.Errorf("deleting check %s[id=%d]: %w", check.Job, check.Id, err)
		}
	}

	for _, check := range update {
		logger.Debug().Int64("id", check.Id).Str("job", check.Job).Interface("check", check.RawCheck).Msg("Updating check")

		if _, err := withTimeout(context.TODO(), p.RequestTimeout, func(ctx context.Context) (int64, error) {
			result, err := cli.UpdateCheck(ctx, check.RawCheck)
			if err != nil {
				return 0, err
			}
			return result.Id, nil
		}); err != nil {
			return fmt.Errorf("deleting check %s[id=%d]: %w", check.Job, check.Id, err)
		}
	}

	for _, check := range add {
		check.MarkManaged() // TODO: Here?
		logger.Debug().Str("job", check.Job).Interface("check", check.RawCheck).Msg("Creating check")

		if _, err := withTimeout(context.TODO(), p.RequestTimeout, func(ctx context.Context) (int64, error) {
			result, err := cli.AddCheck(ctx, check.RawCheck)
			if err != nil {
				return 0, err
			}
			return result.Id, nil
		}); err != nil {
			return fmt.Errorf("deleting check %s[id=%d]: %w", check.Job, check.Id, err)
		}
	}

	logger.Debug().Msg("Done")

	return nil
}

type apiState struct {
	checks sm.CheckSet
	probes sm.ProbeSet
}

func withTimeout[T any](baseCtx context.Context, timeout time.Duration, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(baseCtx, timeout)
	defer cancel()
	return fn(ctx)
}

func (p *Consolidator) getAPIObjects() (apiState, error) {
	baseClient := http.DefaultClient
	cli := client.NewClient(p.ApiServer, p.ApiToken, baseClient)

	probeList, err := withTimeout(context.TODO(), p.RequestTimeout, cli.ListProbes)
	if err != nil {
		return apiState{}, fmt.Errorf("listing probes: %w", err)
	}

	checkList, err := withTimeout(context.TODO(), p.RequestTimeout, cli.ListChecks)
	if err != nil {
		return apiState{}, fmt.Errorf("listing checks: %w", err)
	}

	api := apiState{
		probes: sm.ProbeSet{},
		checks: sm.CheckSet{},
	}

	for idx := range probeList {
		api.probes[strings.ToLower(probeList[idx].Name)] = &probeList[idx]
	}

	for _, apiCheck := range checkList {
		check := &sm.Check{
			RawCheck: apiCheck,
		}
		//if !check.IsManaged() {
		//	continue
		//}

		api.checks[check.Job] = check
	}

	return api, nil
}

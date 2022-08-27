package state

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/adriansr/sm-controller/internal/builder"
	"github.com/adriansr/sm-controller/internal/helpers/timer"
	"github.com/adriansr/sm-controller/internal/schema"
	"github.com/adriansr/sm-controller/internal/watchers"
	"github.com/rs/zerolog"
	coreV1 "k8s.io/api/core/v1"
	networkingV1 "k8s.io/api/networking/v1"
)

type Version uint32

type ClusterState struct {
	Services  []*coreV1.Service
	Ingresses []*networkingV1.Ingress
	Version   Version
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

func (s *State) publish() {
	s.lastPublished++
	update := ClusterState{
		Version: s.lastPublished,
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
		initialSyncTimeout = time.Minute

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
			s.publish()

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

	cs      ClusterState
	syncing bool
}

func (p *Consolidator) Publish(cs ClusterState) {
	p.Logger.Info().Msgf("Received cluster state v%d", cs.Version)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.cs = cs
	if !p.syncing {
		p.syncing = true
		go p.sync()
	}
}

func (p *Consolidator) getCS() ClusterState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cs
}

func (p *Consolidator) sync() {
	var lastSynced Version
	for cs := p.getCS(); cs.Version != lastSynced; lastSynced, cs = cs.Version, p.getCS() {
		log := p.Logger.With().Interface("version", cs.Version).Logger()
		log.Debug().Msg("starting sync")
		if err := p.syncState(cs); err != nil {
			log.Err(err).Msg("sync failed")
			continue
		}
		log.Info().Msg("Sync completed")
	}
}

func (p *Consolidator) syncState(cs ClusterState) error {
	logger := p.Logger.With().Interface("version", cs.Version).Logger()
	logger.Info().
		Int("num_services", len(cs.Services)).
		Int("num_ingresses", len(cs.Ingresses)).
		Msg("Starting sync")

	if len(cs.Services) == 0 {
		logger.Info().Msg("Skipping sync: No annotated services")
		return nil
	}

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
	return nil
}

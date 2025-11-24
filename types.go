package hsm

import (
	"context"
)

type EntityType string

type Entity interface {
	SubEntities() map[string]map[string]Entity
	UpdateSubEntity(typeKey string, entitiKey string, entity Entity) (Entity, error)
	Copy() Entity // TODO Think about retuning the Entity if exact the same type
}

type Action string

type StateMachina[E Entity] interface {
	Do(ctx context.Context, entity E, action Action, actionParams interface{}, syncerErr error) (changedEntity E, err error)
	Validate(ctx context.Context, entity E, action Action, actionParams interface{}) (err error)
	CanProcess(entity E) bool
}

type Syncer[E Entity] interface {
	Sync(ctx context.Context, entity E) (changedEntity E, err error)
	CanProcess(entity E) bool
	NeedSync(entity E, action Action) bool
	Do(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error)
	HasAction(action Action) bool
}

type Repository[E Entity] interface {
	Save(ctx context.Context, oldEntity, newEntity *E) (savedEntity *E, err error)
}

type ProcessorConfig[E Entity] struct {
	GetProcessorFunc func(entity Entity) Processor[Entity]
	StateMachines    []StateMachina[E]
	Repository       Repository[E]
	Syncers          []Syncer[E]
}

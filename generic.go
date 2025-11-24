package hsm

import (
	"context"
)

func WrapProcessor[E Entity](proc Processor[E]) Processor[Entity] {
	return &genericProcessorWrapper[E]{processor: proc}
}

type genericProcessorWrapper[E Entity] struct {
	processor Processor[E]
}

func (w *genericProcessorWrapper[E]) Process(ctx context.Context, entity Entity, action Action, actionParams interface{}) (Entity, error) {
	typedEntity, ok := entity.(E)
	if !ok {
		return nil, ErrorInvalidEntityType
	}
	return w.processor.Process(ctx, typedEntity, action, actionParams)
}

func (w *genericProcessorWrapper[E]) syncAndValidate(ctx context.Context, entity Entity, action Action, actionParams interface{}) (Entity, error) {
	typedEntity, ok := entity.(E)
	if !ok {
		return nil, ErrorInvalidEntityType
	}
	return w.processor.syncAndValidate(ctx, typedEntity, action, actionParams)
}

func (w *genericProcessorWrapper[E]) act(ctx context.Context, entity Entity, action Action, actionParams interface{}) (Entity, error) {
	typedEntity, ok := entity.(E)
	if !ok {
		return nil, ErrorInvalidEntityType
	}
	return w.processor.act(ctx, typedEntity, action, actionParams)
}

func (w *genericProcessorWrapper[E]) save(ctx context.Context, oldEntity, newEntity *Entity) (*Entity, error) {
	var typedOldEntity *E
	if oldEntity != nil {
		typedOldEntityValue, ok := (*oldEntity).(E)
		if !ok {
			return nil, ErrorInvalidEntityType
		}
		typedOldEntity = &typedOldEntityValue
	}

	var typedNewEntity *E
	if newEntity != nil {
		typedNewEntityValue, ok := (*newEntity).(E)
		if !ok {
			return nil, ErrorInvalidEntityType
		}
		typedNewEntity = &typedNewEntityValue
	}

	savedTypedEntity, err := w.processor.save(ctx, typedOldEntity, typedNewEntity)
	if err != nil {
		return newEntity, err
	}

	if savedTypedEntity == nil {
		return nil, nil
	}

	savedTypeEntityValue := *savedTypedEntity
	generalTypeEntityValue, ok := any(savedTypeEntityValue).(Entity)
	if !ok {
		return nil, ErrorInvalidEntityType
	}

	return &generalTypeEntityValue, nil
}

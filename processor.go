package hsm

import (
	"context"
	"reflect"
)

type Processor[E Entity] interface {
	Process(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error)
	syncAndValidate(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error)
	act(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error)
	save(ctx context.Context, oldEntity, newEntity *E) (savedEntity *E, err error)
}

type processor[E Entity] struct {
	config ProcessorConfig[E]
}

func NewProcessor[E Entity](config ProcessorConfig[E]) Processor[E] {
	return &processor[E]{
		config: config,
	}
}

func (p *processor[E]) Process(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error) {
	syncedEntity, err := p.syncAndValidate(ctx, entity, action, actionParams)
	if err != nil {
		return entity, err
	}

	changedEntity, err = p.act(ctx, syncedEntity, action, actionParams)
	if err != nil {
		return syncedEntity, err
	}

	savedEntity, err := p.save(ctx, &entity, &changedEntity)
	if err != nil {
		return changedEntity, err
	}

	// the root entity should not be nil, however it is safer to return changedEntity instead of nil
	if savedEntity == nil {
		return changedEntity, nil
	}

	return *savedEntity, nil
}

func (p *processor[E]) syncAndValidate(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error) {
	changedEntity = entity.Copy().(E)
	for typeKey, typedSubentites := range entity.SubEntities() {
		for subEntityKey, subEntity := range typedSubentites {
			subProcessor := p.config.GetProcessorFunc(subEntity)
			if isNilProcessor(subProcessor) {
				return changedEntity, ErrorProcessorNotFound
			}

			changedSubEntity, err := subProcessor.syncAndValidate(ctx, subEntity, action, actionParams)
			if err != nil {
				return changedEntity, err
			}

			updatedEntity, err := changedEntity.UpdateSubEntity(typeKey, subEntityKey, changedSubEntity)
			if err != nil {
				return changedEntity, err
			}
			changedEntity = updatedEntity.(E)
		}
	}

	syncer := p.getSyncerForEntity(changedEntity)
	if syncer != nil && syncer.NeedSync(entity, action) {
		changedEntity, err = syncer.Sync(ctx, changedEntity)
		if err != nil {
			return changedEntity, err
		}
	}

	sm, err := p.getStateMachineForEntity(ctx, entity)
	if err != nil {
		return changedEntity, err
	}

	err = sm.Validate(ctx, changedEntity, action, actionParams)
	if err != nil {
		return changedEntity, err
	}

	return changedEntity, nil
}

func (p *processor[E]) act(ctx context.Context, entity E, action Action, actionParams interface{}) (changedEntity E, err error) {
	changedEntity = entity.Copy().(E)

	for typeKey, typedSubentites := range entity.SubEntities() {
		for subEntityKey, subEntity := range typedSubentites {
			subProcessor := p.config.GetProcessorFunc(subEntity)
			if isNilProcessor(subProcessor) {
				return changedEntity, ErrorProcessorNotFound
			}

			changedSubEntity, err := subProcessor.act(ctx, subEntity, action, actionParams)
			if err != nil {
				return changedEntity, err
			}

			updatedEntity, err := changedEntity.UpdateSubEntity(typeKey, subEntityKey, changedSubEntity)
			if err != nil {
				return changedEntity, err
			}
			changedEntity = updatedEntity.(E)
		}
	}

	sm, err := p.getStateMachineForEntity(ctx, changedEntity)
	if err != nil {
		return changedEntity, err
	}

	actSyncer := p.getSyncerForEntityAndAction(changedEntity, action)
	var syncerErr error
	if actSyncer != nil {
		changedEntity, syncerErr = actSyncer.Do(ctx, changedEntity, action, actionParams)
	}

	changedEntity, err = sm.Do(ctx, changedEntity, action, actionParams, syncerErr)
	if err != nil {
		return changedEntity, err
	}

	return changedEntity, nil
}

func (p *processor[E]) save(ctx context.Context, oldEntity, newEntity *E) (savedEntity *E, err error) {
	// if both entities are nil, saving makes no sence
	if oldEntity == nil && newEntity == nil {
		return nil, nil
	}

	// new instane of entity to prevent mutation by pointer
	var changedNewEntity E
	if newEntity != nil {
		changedNewEntity = (*newEntity).Copy().(E)
	}

	var oldSubEntities map[string]map[string]Entity
	var newSubEntities map[string]map[string]Entity

	if oldEntity != nil {
		oldSubEntities = (*oldEntity).SubEntities()
	}
	if newEntity != nil {
		newSubEntities = (*newEntity).SubEntities()
	}

	// creating a set of all typeKey from both entities
	allTypeKeys := make(map[string]bool)
	for typeKey := range oldSubEntities {
		allTypeKeys[typeKey] = true
	}
	for typeKey := range newSubEntities {
		allTypeKeys[typeKey] = true
	}

	for typeKey := range allTypeKeys {
		oldTypedEntities, oldTypedEntitiesExists := oldSubEntities[typeKey]
		newTypedEntities, newTypedEntitiesExists := newSubEntities[typeKey]

		// Case 1: the key is present in the old entity only
		if oldTypedEntitiesExists && !newTypedEntitiesExists {
			for _, oldSubEntity := range oldTypedEntities {
				subProcessor := p.config.GetProcessorFunc(oldSubEntity)
				if isNilProcessor(subProcessor) {
					return nil, ErrorProcessorNotFound
				}
				_, err := subProcessor.save(ctx, &oldSubEntity, nil)
				if err != nil {
					return nil, err
				}
			}
			continue
		}

		// Case 2: the key is present in the new entity only
		if newTypedEntitiesExists && !oldTypedEntitiesExists {
			for entityKey, newSubEntity := range newTypedEntities {
				subProcessor := p.config.GetProcessorFunc(newSubEntity)
				if isNilProcessor(subProcessor) {
					return nil, ErrorProcessorNotFound
				}
				savedNewSubEntity, err := subProcessor.save(ctx, nil, &newSubEntity)
				if err != nil {
					return nil, err
				}
				if savedNewSubEntity != nil {
					updatedEntity, err := changedNewEntity.UpdateSubEntity(typeKey, entityKey, *savedNewSubEntity)
					if err != nil {
						return nil, err
					}
					changedNewEntity = updatedEntity.(E)
				}
			}
			continue
		}

		// Case 3: the key is present in both entities
		if newTypedEntitiesExists && oldTypedEntitiesExists {
			allEntityKeys := make(map[string]bool)
			for entityKey := range oldTypedEntities {
				allEntityKeys[entityKey] = true
			}
			for entityKey := range newTypedEntities {
				allEntityKeys[entityKey] = true
			}

			for entityKey := range allEntityKeys {
				oldSubEntity, oldSubEntityExists := oldTypedEntities[entityKey]
				newSubEntity, newSubEntityExists := newTypedEntities[entityKey]

				var subProcessor Processor[Entity]
				if oldSubEntityExists {
					subProcessor = p.config.GetProcessorFunc(oldSubEntity)
				} else {
					subProcessor = p.config.GetProcessorFunc(newSubEntity)
				}
				if isNilProcessor(subProcessor) {
					return nil, ErrorProcessorNotFound
				}

				var oldSubEntityPtr *Entity
				var newSubEntityPtr *Entity
				if oldSubEntityExists {
					oldSubEntityPtr = &oldSubEntity
				}
				if newSubEntityExists {
					newSubEntityPtr = &newSubEntity
				}

				savedNewSubEntity, err := subProcessor.save(ctx, oldSubEntityPtr, newSubEntityPtr)
				if err != nil {
					return nil, err
				}
				if newSubEntityExists && savedNewSubEntity != nil {
					updatedEntity, err := changedNewEntity.UpdateSubEntity(typeKey, entityKey, *savedNewSubEntity)
					if err != nil {
						return nil, err
					}
					changedNewEntity = updatedEntity.(E)
				}
			}
		}
	}

	if p.config.Repository != nil {
		// if newEntity is not nul, we should pass in Save its updated version (it could change after UpdateSubEntity)
		var entityToSave *E
		if newEntity != nil {
			entityToSave = &changedNewEntity
		}

		savedEntity, err = p.config.Repository.Save(ctx, oldEntity, entityToSave)
		if err != nil {
			return nil, err
		}

		return savedEntity, nil
	}

	if newEntity == nil {
		return nil, nil
	}

	return &changedNewEntity, nil
}

func (p *processor[E]) getStateMachineForEntity(ctx context.Context, entity E) (StateMachina[E], error) {
	// TODO Return error if multiple state manices can process the entity
	for _, sm := range p.config.StateMachines {
		if sm.CanProcess(entity) {
			return sm, nil
		}
	}

	return nil, ErrorUnprocessableEntity
}

func (p *processor[E]) getSyncerForEntity(entity E) Syncer[E] {
	// TODO Return error if multiple syncers can process the entity
	syncers := p.config.Syncers
	for _, syncer := range syncers {
		if syncer.CanProcess(entity) {
			return syncer
		}
	}

	return nil
}

func (p *processor[E]) getSyncerForEntityAndAction(entity E, action Action) Syncer[E] {
	// TODO Return error if multiple syncers can process the entity
	syncers := p.config.Syncers
	for _, syncer := range syncers {
		if syncer.CanProcess(entity) && syncer.HasAction(action) {
			return syncer
		}
	}

	return nil
}

func isNilProcessor(e Processor[Entity]) bool {
	return e == nil || (reflect.ValueOf(e).Kind() == reflect.Ptr && reflect.ValueOf(e).IsNil())
}

package redisorm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type Model interface {
	GetId() int64
	SetId(id int64)
	TableName() string
	GetIndexFields() map[string]any
	GetTTL() time.Duration
}

type RedisClient struct {
	rdb     *redis.Client
	ctx     context.Context
	timeout time.Duration
}

func NewRedisClient(addr, passwd string, db, protocol, timeout int) *RedisClient {
	new := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: passwd,
		DB:       db,
		Protocol: protocol,
	})

	return &RedisClient{
		rdb:     new,
		ctx:     context.Background(),
		timeout: time.Duration(timeout) * time.Second,
	}
}

func (c *RedisClient) Close() error {
	return c.rdb.Close()
}

type Repository[T Model] struct {
	client      *RedisClient
	tableName   string
	indexFields map[string]any
	mu          sync.RWMutex
	defaultTTL  time.Duration
}

func NewRepository[T Model](c *RedisClient, m T) *Repository[T] {
	return &Repository[T]{
		client:      c,
		tableName:   m.TableName(),
		indexFields: m.GetIndexFields(),
		defaultTTL:  0, // 0 = 만료 없음
	}
}

func (r *Repository[T]) SetDefaultTTL(ttl time.Duration) {
	r.defaultTTL = ttl
}

func (r *Repository[T]) Create(model T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	if err := r.validate(model); err != nil {
		return err
	}

	indexes := model.GetIndexFields()
	for field, value := range indexes {
		if exists, _ := r.checkDuplicate(field, value); exists {
			return fmt.Errorf("duplicate %s: %v", field, value)
		}
	}

	idKey := fmt.Sprintf("next_%s_id", r.tableName)
	id, err := r.client.rdb.Incr(timeoutCtx, idKey).Result()
	if err != nil {
		return err
	}

	model.SetId(id)

	jsonData, err := json.Marshal(model)
	if err != nil {
		return err
	}

	ttl := model.GetTTL()
	if ttl == 0 {
		ttl = r.defaultTTL
	}

	_, err = r.client.rdb.TxPipelined(timeoutCtx, func(pipe redis.Pipeliner) error {
		primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
		pipe.Do(timeoutCtx, "JSON.SET", primaryKey, "$", string(jsonData))

		if ttl > 0 {
			pipe.Expire(timeoutCtx, primaryKey, ttl)
		}

		for field, value := range indexes {
			indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, field)
			pipe.HSet(timeoutCtx, indexKey, fmt.Sprint(value), id)
		}

		allIdsKey := fmt.Sprintf("%s:all_ids", r.tableName)
		pipe.SAdd(timeoutCtx, allIdsKey, id)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create: %w", err)
	}
	return err
}

func (r *Repository[T]) CreateWithTTL(model T, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	if err := r.validate(model); err != nil {
		return err
	}

	indexes := model.GetIndexFields()
	for field, value := range indexes {
		if exists, _ := r.checkDuplicate(field, value); exists {
			return fmt.Errorf("duplicate %s: %v", field, value)
		}
	}

	idKey := fmt.Sprintf("next_%s_id", r.tableName)
	id, err := r.client.rdb.Incr(timeoutCtx, idKey).Result()
	if err != nil {
		return err
	}

	model.SetId(id)

	jsonData, err := json.Marshal(model)
	if err != nil {
		return err
	}

	_, err = r.client.rdb.TxPipelined(timeoutCtx, func(pipe redis.Pipeliner) error {
		primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
		pipe.Do(timeoutCtx, "JSON.SET", primaryKey, "$", string(jsonData))

		if ttl > 0 {
			pipe.Expire(timeoutCtx, primaryKey, ttl)
		}

		for field, value := range indexes {
			indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, field)
			pipe.HSet(timeoutCtx, indexKey, fmt.Sprint(value), id)
		}

		allIdsKey := fmt.Sprintf("%s:all_ids", r.tableName)
		pipe.SAdd(timeoutCtx, allIdsKey, id)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create: %w", err)
	}
	return err
}

func (r *Repository[T]) FindById(id int64) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	var model T

	primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)

	result, err := r.client.rdb.Do(timeoutCtx, "JSON.GET", primaryKey, "$").Result()
	if err != nil {
		return model, fmt.Errorf("not exists record: %v", err)
	}

	jsonStr, ok := result.(string)
	if !ok {
		return model, fmt.Errorf("invalid JSON response")
	}

	var jsonArray []json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &jsonArray); err != nil {
		return model, err
	}

	if len(jsonArray) == 0 {
		return model, fmt.Errorf("record not found")
	}

	if err := json.Unmarshal(jsonArray[0], &model); err != nil {
		return model, err
	}

	return model, nil
}

func (r *Repository[T]) FindByIndex(fieldName string, value interface{}) (T, error) {
	r.mu.RLock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	var model T

	indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, fieldName)
	idStr, err := r.client.rdb.HGet(timeoutCtx, indexKey, fmt.Sprint(value)).Result()
	if err != nil {
		return model, fmt.Errorf("record not found: %w", err)
	}

	var id int64
	fmt.Sscanf(idStr, "%d", &id)

	r.mu.RUnlock()

	return r.FindById(id)
}

func (r *Repository[T]) FindAll() ([]T, error) {
	r.mu.RLock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	allIdsKey := fmt.Sprintf("%s:all_ids", r.tableName)
	idStrs, err := r.client.rdb.SMembers(timeoutCtx, allIdsKey).Result()
	if err != nil {
		return nil, err
	}

	r.mu.RUnlock()

	models := make([]T, 0, len(idStrs))
	for _, idStr := range idStrs {
		var id int64
		fmt.Sscanf(idStr, "%d", &id)

		model, err := r.FindById(id)
		if err != nil {
			continue
		}
		models = append(models, model)
	}

	return models, nil
}

// Update시에 TTL이 model.TTL 혹은 defaultRepo.TTL에 의해 갱신됨.
func (r *Repository[T]) Update(model T) error {
	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	id := model.GetId()
	if id == 0 {
		return fmt.Errorf("model Id is required for update")
	}

	oldModel, err := r.FindById(id)
	if err != nil {
		return fmt.Errorf("record not found: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	jsonData, err := json.Marshal(model)
	if err != nil {
		return fmt.Errorf("failed to marshal model: %w", err)
	}

	ttl := model.GetTTL()
	if ttl == 0 {
		ttl = r.defaultTTL
	}

	_, err = r.client.rdb.TxPipelined(timeoutCtx, func(pipe redis.Pipeliner) error {
		primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
		pipe.Do(timeoutCtx, "JSON.SET", primaryKey, "$", string(jsonData))

		if ttl > 0 {
			pipe.Expire(timeoutCtx, primaryKey, ttl)
		}

		oldIndexes := oldModel.GetIndexFields()
		for field, value := range oldIndexes {
			indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, field)
			pipe.HDel(timeoutCtx, indexKey, fmt.Sprint(value))
		}

		newIndexes := model.GetIndexFields()
		for field, value := range newIndexes {
			indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, field)
			pipe.HSet(timeoutCtx, indexKey, fmt.Sprint(value), id)
		}

		_, err = pipe.Exec(timeoutCtx)
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	return nil
}

func (r *Repository[T]) Delete(id int64) error {
	model, err := r.FindById(id)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	_, err = r.client.rdb.TxPipelined(timeoutCtx, func(pipe redis.Pipeliner) error {
		primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
		pipe.Del(timeoutCtx, primaryKey)

		indexes := model.GetIndexFields()
		for field, value := range indexes {
			indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, field)
			pipe.HDel(timeoutCtx, indexKey, fmt.Sprint(value))
		}

		allIDsKey := fmt.Sprintf("%s:all_ids", r.tableName)
		pipe.SRem(timeoutCtx, allIDsKey, id)

		_, err = pipe.Exec(timeoutCtx)
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	return nil
}

func (r *Repository[T]) GetTTL(id int64) (time.Duration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
	return r.client.rdb.TTL(timeoutCtx, primaryKey).Result()
}

func (r *Repository[T]) RenewTTL(id int64, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
	return r.client.rdb.Expire(timeoutCtx, primaryKey, ttl).Err()
}

func (r *Repository[T]) RemoveTTL(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(r.client.ctx, r.client.timeout)
	defer cancel()

	primaryKey := fmt.Sprintf("%s:%d", r.tableName, id)
	return r.client.rdb.Persist(timeoutCtx, primaryKey).Err()
}

func (r *Repository[T]) validate(model T) error {
	indexes := model.GetIndexFields()
	for field, value := range indexes {
		if value == nil || fmt.Sprint(value) == "" {
			return fmt.Errorf("index field %s cannot be empty", field)
		}
	}
	return nil
}

func (r *Repository[T]) checkDuplicate(field string, value interface{}) (bool, error) {
	indexKey := fmt.Sprintf("%s:idx:%s", r.tableName, field)
	return r.client.rdb.HExists(r.client.ctx, indexKey, fmt.Sprint(value)).Result()
}

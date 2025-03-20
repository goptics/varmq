package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client     *redis.Client
	queueKey   string
	mx         sync.Mutex
	ctx        context.Context
	expiration time.Duration
}

func NewRedisQueue(queueKey string, url string) *RedisQueue {
	opts, err := redis.ParseURL(url)
	if err != nil {
		panic(err)
	}

	client := redis.NewClient(opts)

	return &RedisQueue{
		client:     client,
		queueKey:   queueKey,
		ctx:        context.Background(),
		expiration: 0,
	}
}

func (q *RedisQueue) Dequeue() (any, bool) {
	q.mx.Lock()
	defer q.mx.Unlock()

	result, err := q.client.LPop(q.ctx, q.queueKey).Result()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		fmt.Printf("Error dequeuing: %v\n", err)
		return nil, false
	}

	// Decode base64 string back to bytes
	decoded, err := base64.StdEncoding.DecodeString(result)
	if err != nil {
		fmt.Printf("Error decoding base64: %v\n", err)
		return nil, false
	}

	return decoded, true
}

func (q *RedisQueue) Enqueue(item any) bool {
	q.mx.Lock()
	defer q.mx.Unlock()

	var data string
	switch v := item.(type) {
	case []byte:
		// Encode bytes to base64
		data = base64.StdEncoding.EncodeToString(v)
	default:
		fmt.Printf("Unsupported type for enqueueing: %T\n", item)
		return false
	}

	pipe := q.client.Pipeline()
	pipe.RPush(q.ctx, q.queueKey, data)
	if q.expiration > 0 {
		pipe.Expire(q.ctx, q.queueKey, q.expiration)
	}
	_, err := pipe.Exec(q.ctx)
	if err != nil {
		fmt.Printf("Error enqueueing item: %v\n", err)
		return false
	}

	return true
}

func (q *RedisQueue) Init() {
	q.mx.Lock()
	defer q.mx.Unlock()

	q.client.Del(q.ctx, q.queueKey)
}

func (q *RedisQueue) Len() int {
	q.mx.Lock()
	defer q.mx.Unlock()

	length, err := q.client.LLen(q.ctx, q.queueKey).Result()
	if err != nil {
		return 0
	}
	return int(length)
}

func (q *RedisQueue) Values() []any {
	q.mx.Lock()
	defer q.mx.Unlock()

	results, err := q.client.LRange(q.ctx, q.queueKey, 0, -1).Result()
	if err != nil {
		return []any{}
	}

	values := make([]any, 0, len(results))
	for _, result := range results {
		decoded, err := base64.StdEncoding.DecodeString(result)
		if err != nil {
			fmt.Printf("Error decoding base64: %v\n", err)
			continue
		}
		values = append(values, decoded)
	}

	return values
}

func (q *RedisQueue) Close() error {
	q.ctx.Done()
	return q.client.Close()
}

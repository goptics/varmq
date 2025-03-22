package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/fahimfaisaal/gocq/v2/shared/utils"
	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client              *redis.Client
	queueKey            string
	mx                  sync.Mutex
	ctx                 context.Context
	cancel              context.CancelFunc
	pubsub              *redis.PubSub
	notificationKey     string
	notifications       utils.Notifier
	expiration          time.Duration
	enabledDistribution bool
}

func NewRedisQueue(queueKey string, url string) *RedisQueue {
	opts, err := redis.ParseURL(url)
	if err != nil {
		panic(err)
	}

	client := redis.NewClient(opts)
	ctx, cancel := context.WithCancel(context.Background())

	q := &RedisQueue{
		client:          client,
		queueKey:        queueKey,
		ctx:             ctx,
		cancel:          cancel,
		notificationKey: queueKey + ":notifications",
		notifications:   utils.NewNotifier(100),
	}

	q.startNotificationListener()
	return q
}

func (q *RedisQueue) startNotificationListener() {
	q.pubsub = q.client.Subscribe(q.ctx, q.notificationKey)

	go func() {
		defer q.pubsub.Close()

		for {
			select {
			case <-q.ctx.Done():
				return
			case msg := <-q.pubsub.Channel():
				if msg == nil {
					continue
				}
				// Log the notification for debugging
				q.notifications.Notify()
			}
		}
	}()
}

func (q *RedisQueue) notify() {
	err := q.client.Publish(q.ctx, q.notificationKey, "").Err()
	if err != nil {
		fmt.Printf("Error publishing notification: %v\n", err)
	}
}

// SetExpiration sets the expiration time for the RedisQueue
func (q *RedisQueue) SetExpiration(expiration time.Duration) {
	q.mx.Lock()
	defer q.mx.Unlock()
	q.expiration = expiration
}

// EnableDistribution turns the RedisQueue into a distributed queue by starting the notification listener and pubsub
func (q *RedisQueue) EnableDistribution() *RedisQueue {
	q.mx.Lock()
	defer q.mx.Unlock()

	if !q.enabledDistribution {
		q.enabledDistribution = true
		q.startNotificationListener()
	}

	return q
}

func (q *RedisQueue) NotificationChannel() <-chan struct{} {
	return q.notifications
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

	if _, err := pipe.Exec(q.ctx); err != nil {
		fmt.Printf("Error enqueueing item: %v\n", err)
		return false
	}

	if q.enabledDistribution {
		q.notify()
	}

	return true
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
	q.cancel() // Cancel context to stop notification listener
	if q.pubsub != nil {
		q.pubsub.Close()
	}
	q.notifications.Close()
	return q.client.Close()
}

func (q *RedisQueue) Listen() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if r := recover(); r != nil {
		q.Close()
		signal.Stop(sigChan)
		close(sigChan)
		panic("Redis queue listener terminated due to panic")
	}

	<-sigChan
}

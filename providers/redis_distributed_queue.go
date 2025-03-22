package providers

import (
	"fmt"

	"github.com/fahimfaisaal/gocq/v2/shared/utils"
	"github.com/redis/go-redis/v9"
)

type RedisDistributedQueue struct {
	*RedisQueue
	pubsub          *redis.PubSub
	notificationKey string
	notifications   utils.Notifier
}

func NewRedisDistributedQueue(queueKey string, url string) *RedisDistributedQueue {
	q := &RedisDistributedQueue{
		RedisQueue:      NewRedisQueue(queueKey, url),
		notificationKey: queueKey + ":notifications",
		notifications:   utils.NewNotifier(100),
	}
	q.startNotificationListener()
	return q
}

func (q *RedisDistributedQueue) startNotificationListener() {
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

func (q *RedisDistributedQueue) notify() {
	err := q.client.Publish(q.ctx, q.notificationKey, "").Err()
	if err != nil {
		fmt.Printf("Error publishing notification: %v\n", err)
	}
}

func (q *RedisDistributedQueue) NotificationChannel() <-chan struct{} {
	return q.notifications
}

func (q *RedisDistributedQueue) Enqueue(item any) bool {
	defer q.notify()
	return q.RedisQueue.Enqueue(item)
}

func (q *RedisDistributedQueue) Close() error {
	q.pubsub.Close()
	q.notifications.Close()
	return q.RedisQueue.Close()
}

package rbmq

import (
	"fmt"
	"log"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

type HandlerInterface interface {
	handle(body []byte, routingKey string) error
}

type MessageHandlerfunc func(body []byte, routingKey string) error

func (f MessageHandlerfunc) handle(body []byte, routingKey string) error {
	return f(body, routingKey)
}

type HandlerManagerInterface interface {
	HandleMessage(routingKey string, body []byte) error
	RegisterHandle(routingKey string, handle MessageHandlerfunc)
	RegisterHandler(routingKey string, handler HandlerInterface)
}

type HandlerManager struct {
	handlers map[string]HandlerInterface
	mu       sync.RWMutex
}

func (h *HandlerManager) HandleMessage(routingKey string, body []byte) error {
	h.mu.RLock()
	handler, exists := h.handlers[routingKey]
	h.mu.RUnlock()

	if !exists {
		return fmt.Errorf("not exist routing handler")
	}

	if handler == nil {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			log.Println("panic in handling message")
		}
	}()

	if err := handler.handle(body, routingKey); err != nil {
		log.Printf("error in handling message: %+v", err)
		return err
	}

	return nil
}

func (h *HandlerManager) RegisterHandle(routingKey string, handle MessageHandlerfunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[routingKey] = handle
}

func (h *HandlerManager) RegisterHandler(routingKey string, handler HandlerInterface) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[routingKey] = handler
}

type Consumer struct {
	client         *Client
	channel        *amqp.Channel
	queue          string
	exchange       string
	routingKeys    []string
	handlerManager HandlerManagerInterface
}

type ConsumerConfig struct {
	QueueName    string
	Exchange     string
	ExchangeType string
	RoutingKeys  []string
	PrefetchCnt  int
	Durable      bool
	AutoDelete   bool
	Exclusive    bool
}

func NewConsumer(client *Client, config ConsumerConfig, hm HandlerManagerInterface) (*Consumer, error) {
	ch, err := client.GetChannelFromPool()
	if err != nil {
		return nil, err
	}

	if config.Exchange != "" {
		err := ch.ExchangeDeclare(
			config.Exchange,
			config.ExchangeType,
			config.Durable,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to declare exchange: %w", err)
		}
	}

	// 큐 선언
	queue, err := ch.QueueDeclare(
		config.QueueName,
		config.Durable,
		config.AutoDelete,
		config.Exclusive,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	// 큐 바인딩 (exchange가 있는 경우)
	if config.Exchange != "" {
		for _, key := range config.RoutingKeys {
			err = ch.QueueBind(
				queue.Name,
				key,
				config.Exchange,
				false,
				nil,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to bind queue: %w", err)
			}
		}
	}

	// QoS 설정
	err = ch.Qos(
		config.PrefetchCnt, // prefetch count
		0,                  // prefetch size
		false,              // global
	)
	if err != nil {
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	return &Consumer{
		client:         client,
		channel:        ch,
		queue:          queue.Name,
		exchange:       config.Exchange,
		routingKeys:    config.RoutingKeys,
		handlerManager: hm,
	}, nil
}

func (c *Consumer) ConsumeWithParams(consumer string, autoAck bool, exclusive bool,
	noLocal bool, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	return c.channel.Consume(c.queue, consumer, autoAck, exclusive, noLocal, noWait, args)
}

func (c *Consumer) Close() error {
	return c.client.ReturnChannelToPool(c.channel)
}

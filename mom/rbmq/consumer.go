package rbmq

import (
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	client      *Client
	channel     *amqp.Channel
	queue       string
	exchange    string
	routingKeys []string
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

type ConsumeParams struct {
	ConsumerTag string
	AutoAck     bool
	Exclusive   bool
	NoLocal     bool
	NoWait      bool
	Args        amqp.Table
}

// DefaultConsumeParams 기본 파라미터
func DefaultConsumeParams() ConsumeParams {
	return ConsumeParams{
		ConsumerTag: "",
		AutoAck:     false,
		Exclusive:   false,
		NoLocal:     false,
		NoWait:      false,
		Args:        nil,
	}
}

func (c *Consumer) GetMessages(params ConsumeParams) (<-chan amqp.Delivery, error) {
	return c.channel.Consume(
		c.queue,
		params.ConsumerTag,
		params.AutoAck,
		params.Exclusive,
		params.NoLocal,
		params.NoWait,
		params.Args,
	)
}

func NewConsumer(client *Client, config ConsumerConfig) (*Consumer, error) {
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
	err = ch.Qos(config.PrefetchCnt, 0, false)
	if err != nil {
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	return &Consumer{
		client:      client,
		channel:     ch,
		queue:       queue.Name,
		exchange:    config.Exchange,
		routingKeys: config.RoutingKeys,
	}, nil
}

// Start 기본 파라미터로 시작
func (c *Consumer) Start(handler func(d amqp.Delivery) error) error {
	return c.StartWithParams(DefaultConsumeParams(), handler)
}

// StartWithParams Consume 파라미터 지정하여 시작
func (c *Consumer) StartWithParams(params ConsumeParams, handler func(d amqp.Delivery) error) error {
	msgs, err := c.GetMessages(params)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	// log.Printf("Consumer started on queue: %s (autoAck=%v, exclusive=%v)",
	// 	c.queue, params.AutoAck, params.Exclusive)

	go func() {
		for d := range msgs {
			// log.Printf("Received message: %s (routing key: %s)", d.Body, d.RoutingKey)

			if err := handler(d); err != nil {
				log.Printf("failed to handle message: %v", err)
				if !params.AutoAck {
					d.Nack(false, true) // requeue
				}
			} else {
				if !params.AutoAck {
					d.Ack(false)
				}
				// log.Printf("Message processed successfully")
			}
		}
	}()

	return nil
}

// StartWithRoutingHandlers 라우팅 키별 핸들러 맵 사용
func (c *Consumer) StartWithRoutingHandlers(params ConsumeParams, handlers map[string]func([]byte) error) error {
	msgs, err := c.GetMessages(params)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	// log.Printf("Consumer started on queue: %s with routing handlers", c.queue)

	go func() {
		for d := range msgs {
			handler, exists := handlers[d.RoutingKey]
			if !exists {
				log.Printf("not exist handler for routing key: %s", d.RoutingKey)
				if !params.AutoAck {
					d.Nack(false, false) // reject without requeue
				}
				continue
			}

			if err := handler(d.Body); err != nil {
				// log.Printf("Error handling message: %v", err)
				if !params.AutoAck {
					d.Nack(false, true)
				}
			} else {
				if !params.AutoAck {
					d.Ack(false)
				}
			}
		}
	}()

	return nil
}

func (c *Consumer) Close() error {
	return c.client.ReturnChannelToPool(c.channel)
}

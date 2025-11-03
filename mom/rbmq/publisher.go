package rbmq

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	client   *Client
	exchange string
	channel  *amqp.Channel
}

// durable: true, autoDelete: false, internal: false, noWait: false, args: nil
func NewPublisher(exchange, exchangeType string, c *Client) (*Publisher, error) {
	if c == nil {
		return nil, fmt.Errorf("nil client")
	}

	ch, err := c.GetChannelFromPool()
	if err != nil {
		return nil, err
	}

	if exchange != "" {
		err = ch.ExchangeDeclare(
			exchange,
			exchangeType,
			true,
			false,
			false,
			false,
			nil,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to declare exchange: %v", err)
		}
	}

	return &Publisher{
		client:   c,
		exchange: exchange,
		channel:  ch,
	}, nil
}

func (p *Publisher) Close() error {
	return p.client.ReturnChannelToPool(p.channel)
}

// Publish 메시지 발행
func (p *Publisher) Publish(ctx context.Context, routingKey, contentType string, body []byte) error {

	err := p.channel.PublishWithContext(
		ctx,
		p.exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  contentType,
			Body:         body,
			DeliveryMode: amqp.Persistent,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	return nil
}

// PublishToQueue Default exchange를 사용한 직접 큐 발행
func (p *Publisher) PublishToQueue(ctx context.Context, queueName, contentType string, body []byte) error {

	err := p.channel.PublishWithContext(
		ctx,
		"",        // default exchange
		queueName, // routing key = queue name
		false,
		false,
		amqp.Publishing{
			ContentType:  contentType,
			Body:         body,
			DeliveryMode: amqp.Persistent,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish to queue: %v", err)
	}

	return nil
}

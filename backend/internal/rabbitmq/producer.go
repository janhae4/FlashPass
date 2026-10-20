package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"flashpass/backend/internal/config"
)

const (
	OrderExchange     = "flashpass.order.exchange"
	OrderCreateQueue  = "flashpass.order.create.queue"
	OrderCreateKey    = "order.create"

	DelayedExchange   = "flashpass.order.delayed.exchange"
	OrderTimeoutQueue = "flashpass.order.timeout.queue"
	OrderTimeoutKey   = "order.timeout"
)

type Delivery = amqp.Delivery

type Producer struct {
	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
	url  string
}

func NewProducer(cfg *config.Config) (*Producer, error) {
	p := &Producer{url: cfg.RabbitMQURL}
	if err := p.connect(); err != nil {
		return nil, err
	}
	log.Println("[RabbitMQ] Producer connected and topology declared.")
	return p, nil
}

func (p *Producer) Channel() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil || p.conn.IsClosed() {
		return nil, fmt.Errorf("connection is closed")
	}
	return p.conn.Channel()
}

func (p *Producer) Connection() *amqp.Connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn
}

func (p *Producer) connect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to dial rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// 1. Declare standard order topic exchange
	err = ch.ExchangeDeclare(
		OrderExchange,
		"topic",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	qCreate, err := ch.QueueDeclare(
		OrderCreateQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	err = ch.QueueBind(qCreate.Name, OrderCreateKey, OrderExchange, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	err = ch.ExchangeDeclare(
		DelayedExchange,
		"x-delayed-message",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		amqp.Table{"x-delayed-type": "direct"},
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare delayed exchange: %w", err)
	}

	qTimeout, err := ch.QueueDeclare(
		OrderTimeoutQueue,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare timeout queue: %w", err)
	}

	err = ch.QueueBind(qTimeout.Name, OrderTimeoutKey, DelayedExchange, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to bind timeout queue: %w", err)
	}

	p.conn = conn
	p.ch = ch
	return nil
}

func (p *Producer) PublishOrderCreate(ctx context.Context, payload interface{}) error {
	return p.publish(ctx, OrderExchange, OrderCreateKey, payload, 0)
}

func (p *Producer) PublishDelayedOrderTimeout(ctx context.Context, payload interface{}, delayMs int64) error {
	return p.publish(ctx, DelayedExchange, OrderTimeoutKey, payload, delayMs)
}

func (p *Producer) publish(ctx context.Context, exchange, key string, payload interface{}, delayMs int64) error {
	p.mu.Lock()
	ch := p.ch
	p.mu.Unlock()

	if ch == nil || ch.IsClosed() {
		if err := p.connect(); err != nil {
			return fmt.Errorf("rabbitmq reconnect failed: %w", err)
		}
		p.mu.Lock()
		ch = p.ch
		p.mu.Unlock()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	headers := amqp.Table{}
	if delayMs > 0 {
		headers["x-delay"] = delayMs
	}

	return ch.PublishWithContext(
		ctx,
		exchange,
		key,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			Headers:      headers,
			Body:         body,
		},
	)
}

func (p *Producer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

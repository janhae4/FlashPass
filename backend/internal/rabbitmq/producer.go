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
	OrderExchange    = "flashpass.order.exchange"
	OrderCreateQueue = "flashpass.order.create.queue"
	OrderCreateKey   = "order.create"
)

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

	// Declare order exchange
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

	// Declare order creation queue
	q, err := ch.QueueDeclare(
		OrderCreateQueue,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Bind queue to exchange
	err = ch.QueueBind(
		q.Name,
		OrderCreateKey,
		OrderExchange,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	p.conn = conn
	p.ch = ch
	return nil
}

// PublishOrderCreate sends the order creation message to RabbitMQ
func (p *Producer) PublishOrderCreate(ctx context.Context, payload interface{}) error {
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
		return fmt.Errorf("failed to marshal order event: %w", err)
	}

	return ch.PublishWithContext(
		ctx,
		OrderExchange,
		OrderCreateKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
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

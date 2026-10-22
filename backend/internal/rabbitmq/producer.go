package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"flashpass/backend/internal/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	OrderExchange    = "flashpass.order.exchange"
	OrderCreateQueue = "flashpass.order.create.queue"
	OrderCreateKey   = "order.create"

	OrderRefundQueue = "flashpass.order.refund.queue"
	OrderRefundKey   = "order.refund"

	DelayedExchange   = "flashpass.order.delayed.exchange"
	OrderTimeoutQueue = "flashpass.order.timeout.queue"
	OrderTimeoutKey   = "order.timeout"

	OrderDLExchange = "flashpass.order.dlx"
	OrderDLQueue    = "flashpass.order.dlq"
	OrderDLKey      = "order.dlq"
)

type Delivery = amqp.Delivery

// EventProducer abstracts message publishing for dependency injection and mocking
type EventProducer interface {
	PublishOrderCreate(ctx context.Context, payload interface{}) error
	PublishDelayedOrderTimeout(ctx context.Context, payload interface{}, delayMs int64) error
	PublishOrderRefund(ctx context.Context, payload interface{}) error
	GetQueueDepth(queueName string) (int, error)
	Channel() (*amqp.Channel, error)
	Close()
}

type Producer struct {
	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
	url  string
}

// Compile-time check that Producer implements EventProducer
var _ EventProducer = (*Producer)(nil)

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
	return p.connectLocked()
}

func (p *Producer) connectLocked() error {
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

	err = ch.ExchangeDeclare(
		OrderDLExchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare dlx exchange: %w", err)
	}

	qDLQ, err := ch.QueueDeclare(
		OrderDLQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare dlq queue: %w", err)
	}

	err = ch.QueueBind(qDLQ.Name, OrderDLKey, OrderDLExchange, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to bind dlq: %w", err)
	}

	qCreateArgs := amqp.Table{
		"x-dead-letter-exchange":    OrderDLExchange,
		"x-dead-letter-routing-key": OrderDLKey,
	}

	qCreate, err := ch.QueueDeclare(
		OrderCreateQueue,
		true,
		false,
		false,
		false,
		qCreateArgs,
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

	qRefund, err := ch.QueueDeclare(
		OrderRefundQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare refund queue: %w", err)
	}

	err = ch.QueueBind(qRefund.Name, OrderRefundKey, OrderExchange, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to bind refund queue: %w", err)
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

func (p *Producer) PublishOrderRefund(ctx context.Context, payload interface{}) error {
	return p.publish(ctx, OrderExchange, OrderRefundKey, payload, 0)
}

// GetQueueDepth inspects the specified queue and returns the count of ready messages
func (p *Producer) GetQueueDepth(queueName string) (int, error) {
	ch, err := p.Channel()
	if err != nil {
		return 0, err
	}
	defer ch.Close()

	q, err := ch.QueueInspect(queueName)
	if err != nil {
		return 0, err
	}
	return q.Messages, nil
}

// publish marshals the payload and executes a thread-safe publish on the channel.
// Locking ensures that concurrent goroutines never invoke PublishWithContext simultaneously
// on the same amqp091 Channel.
func (p *Producer) publish(ctx context.Context, exchange, key string, payload interface{}, delayMs int64) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	headers := amqp.Table{}
	if delayMs > 0 {
		headers["x-delay"] = delayMs
	}

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Headers:      headers,
		Body:         body,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ch == nil || p.ch.IsClosed() {
		if err := p.connectLocked(); err != nil {
			return fmt.Errorf("rabbitmq reconnect failed: %w", err)
		}
	}

	return p.ch.PublishWithContext(
		ctx,
		exchange,
		key,
		false, // mandatory
		false, // immediate
		publishing,
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

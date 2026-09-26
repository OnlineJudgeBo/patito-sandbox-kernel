package rabbitmq

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/streadway/amqp"
)

type RabbitService struct {
	Conn    *amqp.Connection
	Channel *amqp.Channel
}

func NewRabbitService(amqpURL string) (*RabbitService, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	return &RabbitService{Conn: conn, Channel: ch}, nil
}

type EvaluationMessage struct {
	Input      domain.EvaluationInput `json:"input"`
	ReplyQueue string                 `json:"reply_queue"`
}

func (r *RabbitService) Listen(queueName string, handler func(domain.EvaluationInput) (domain.EvaluationResult, error)) error {
	queue, err := r.Channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to declare queue %s: %w", queueName, err)
	}
	if err := r.Channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("failed to configure consumer prefetch: %w", err)
	}
	msgs, err := r.Channel.Consume(
		queue.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	log.Printf("[*] Listening on queue: %s", queueName)

	for d := range msgs {
		var envelope EvaluationMessage
		if err := json.Unmarshal(d.Body, &envelope); err != nil {
			log.Printf("[!] Failed to parse message: %v", err)
			d.Nack(false, false)
			continue
		}
		msg := envelope.Input
		replyQueue := envelope.ReplyQueue
		// Backward-compatible support for the original raw task contract.
		if msg.ID == "" {
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				d.Nack(false, false)
				continue
			}
		}

		result, err := handler(msg)
		if err != nil {
			log.Printf("[!] Error handling message: %v", err)
			d.Nack(false, false)
			continue
		}
		if replyQueue != "" {
			if _, err := r.Channel.QueueDeclare(replyQueue, true, false, false, false, nil); err != nil {
				d.Nack(false, true)
				continue
			}
			if err := r.Publish(replyQueue, result); err != nil {
				log.Printf("[!] Failed to publish result: %v", err)
				d.Nack(false, true)
				continue
			}
		}

		d.Ack(false)
	}

	return nil
}

func (r *RabbitService) Publish(queueName string, result domain.EvaluationResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to serialize result: %w", err)
	}

	return r.Channel.Publish(
		"", queueName, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         data,
		},
	)
}

func (r *RabbitService) Close() {
	_ = r.Channel.Close()
	_ = r.Conn.Close()
}

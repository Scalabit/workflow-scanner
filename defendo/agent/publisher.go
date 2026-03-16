//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub/v2"

	"guardify-agent/checks"
)

// Publisher wraps a Google Cloud Pub/Sub publisher.
type Publisher struct {
	client    *pubsub.Client
	publisher *pubsub.Publisher
	topicID   string
}

// reportEnvelope is the message payload sent to Pub/Sub.
type reportEnvelope struct {
	Timestamp string            `json:"timestamp"`
	System    checks.SystemInfo `json:"system"`
	Results   []checks.Result   `json:"results"`
}

// NewPublisher creates a Pub/Sub client and publisher for the given project and topic.
// Authentication uses Application Default Credentials (ADC):
//   - GOOGLE_APPLICATION_CREDENTIALS env var → service account JSON key file
//   - Workload Identity (when running on GCP)
//   - gcloud CLI credentials (for local development: `gcloud auth application-default login`)
func NewPublisher(ctx context.Context, projectID, topicID string) (*Publisher, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub.NewClient: %w", err)
	}

	// Full topic resource name required by v2.
	topicName := fmt.Sprintf("projects/%s/topics/%s", projectID, topicID)
	pub := client.Publisher(topicName)

	return &Publisher{client: client, publisher: pub, topicID: topicID}, nil
}

// Publish serialises the check report and sends it as a single Pub/Sub message.
// Message attributes include hostname and agent metadata for server-side filtering.
func (p *Publisher) Publish(ctx context.Context, sysInfo checks.SystemInfo, results []checks.Result) error {
	payload, err := json.Marshal(reportEnvelope{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		System:    sysInfo,
		Results:   results,
	})
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	msg := &pubsub.Message{
		Data: payload,
		Attributes: map[string]string{
			"hostname":  sysInfo.Hostname,
			"username":  sysInfo.Username,
			"os_build":  sysInfo.OSBuild,
			"agent":     "guardify-agent",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	}

	result := p.publisher.Publish(ctx, msg)
	id, err := result.Get(ctx)
	if err != nil {
		return fmt.Errorf("publish to topic %q: %w", p.topicID, err)
	}

	fmt.Printf("Published to Pub/Sub topic %q — message ID: %s\n", p.topicID, id)
	return nil
}

// Close flushes pending messages and closes the underlying client.
func (p *Publisher) Close() {
	p.publisher.Stop()
	if err := p.client.Close(); err != nil {
		fmt.Printf("warning: failed to close pubsub client: %v\n", err)
	}
}
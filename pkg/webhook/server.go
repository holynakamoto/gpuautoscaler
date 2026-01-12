package webhook

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// SetupWebhookServer configures and starts the webhook server
func SetupWebhookServer(mgr ctrl.Manager, enableMIG, enableMPS, enableTS bool) error {
	// Create the webhook
	webhookHandler := NewGPUOptimizationWebhook(
		mgr.GetClient(),
		enableMIG,
		enableMPS,
		enableTS,
	)

	// Register the webhook with the manager
	mgr.GetWebhookServer().Register(
		"/mutate-v1-pod",
		&webhook.Admission{Handler: webhookHandler},
	)

	return nil
}

// ValidateWebhookConfiguration validates the webhook configuration
func ValidateWebhookConfiguration(ctx context.Context, client client.Client) error {
	// Check if webhook service exists
	svc := &corev1.Service{}
	if err := client.Get(ctx,
		client.ObjectKey{
			Name:      "gpu-autoscaler-webhook-service",
			Namespace: "gpu-autoscaler-system",
		},
		svc); err != nil {
		return fmt.Errorf("webhook service not found: %w", err)
	}

	// Check if webhook has valid TLS configuration
	secret := &corev1.Secret{}
	if err := client.Get(ctx,
		client.ObjectKey{
			Name:      "webhook-server-cert",
			Namespace: "gpu-autoscaler-system",
		},
		secret); err != nil {
		return fmt.Errorf("webhook TLS secret not found: %w", err)
	}

	return nil
}

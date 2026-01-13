package cost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v1alpha1 "github.com/gpuautoscaler/gpuautoscaler/pkg/apis/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AlertManager sends budget alert notifications
type AlertManager struct {
	httpClient *http.Client
}

// AlertMessage contains alert details
type AlertMessage struct {
	BudgetName     string
	AlertName      string
	Severity       string
	CurrentSpend   float64
	MonthlyLimit   float64
	PercentageUsed float64
	Threshold      float64
	Timestamp      time.Time
}

// NewAlertManager creates a new alert manager
func NewAlertManager() *AlertManager {
	return &AlertManager{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendAlert sends an alert through the specified channel
func (am *AlertManager) SendAlert(ctx context.Context, channel v1alpha1.AlertChannel, msg AlertMessage) error {
	logger := log.FromContext(ctx)

	switch channel.Type {
	case "email":
		return am.sendEmailAlert(ctx, channel, msg)
	case "slack":
		return am.sendSlackAlert(ctx, channel, msg)
	case "pagerduty":
		return am.sendPagerDutyAlert(ctx, channel, msg)
	case "webhook":
		return am.sendWebhookAlert(ctx, channel, msg)
	default:
		logger.Info("Unknown alert channel type", "type", channel.Type)
		return fmt.Errorf("unknown channel type: %s", channel.Type)
	}
}

// sendEmailAlert sends alert via email
func (am *AlertManager) sendEmailAlert(ctx context.Context, channel v1alpha1.AlertChannel, msg AlertMessage) error {
	logger := log.FromContext(ctx)

	// In production, integrate with email service (SMTP, SendGrid, SES, etc.)
	to := channel.Config["to"]
	if to == "" {
		return fmt.Errorf("email 'to' address not configured")
	}

	subject := fmt.Sprintf("[%s] GPU Budget Alert: %s", msg.Severity, msg.BudgetName)
	body := am.formatEmailBody(msg)

	logger.Info("Sending email alert",
		"to", to,
		"subject", subject,
		"budget", msg.BudgetName,
	)

	// TODO: Implement actual email sending
	// For now, just log
	logger.V(1).Info("Email alert body", "body", body)

	return nil
}

// sendSlackAlert sends alert to Slack
func (am *AlertManager) sendSlackAlert(ctx context.Context, channel v1alpha1.AlertChannel, msg AlertMessage) error {
	logger := log.FromContext(ctx)

	webhookURL := channel.Config["webhook_url"]
	if webhookURL == "" {
		return fmt.Errorf("slack webhook_url not configured")
	}

	// Build Slack message
	slackMsg := map[string]interface{}{
		"text": fmt.Sprintf("GPU Budget Alert: %s", msg.BudgetName),
		"attachments": []map[string]interface{}{
			{
				"color": am.getSeverityColor(msg.Severity),
				"fields": []map[string]interface{}{
					{
						"title": "Budget",
						"value": msg.BudgetName,
						"short": true,
					},
					{
						"title": "Alert",
						"value": msg.AlertName,
						"short": true,
					},
					{
						"title": "Current Spend",
						"value": fmt.Sprintf("$%.2f / $%.2f", msg.CurrentSpend, msg.MonthlyLimit),
						"short": true,
					},
					{
						"title": "Percentage Used",
						"value": fmt.Sprintf("%.1f%% (threshold: %.0f%%)", msg.PercentageUsed, msg.Threshold),
						"short": true,
					},
				},
				"footer": "GPU Autoscaler",
				"ts":     msg.Timestamp.Unix(),
			},
		},
	}

	payload, err := json.Marshal(slackMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := am.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send slack alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned non-200 status: %d", resp.StatusCode)
	}

	logger.Info("Sent slack alert",
		"budget", msg.BudgetName,
		"severity", msg.Severity,
	)

	return nil
}

// sendPagerDutyAlert sends alert to PagerDuty
func (am *AlertManager) sendPagerDutyAlert(ctx context.Context, channel v1alpha1.AlertChannel, msg AlertMessage) error {
	logger := log.FromContext(ctx)

	routingKey := channel.Config["routing_key"]
	if routingKey == "" {
		return fmt.Errorf("pagerduty routing_key not configured")
	}

	// Build PagerDuty event
	event := map[string]interface{}{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"payload": map[string]interface{}{
			"summary":  fmt.Sprintf("GPU Budget Alert: %s at %.1f%%", msg.BudgetName, msg.PercentageUsed),
			"severity": am.mapSeverity(msg.Severity),
			"source":   "gpu-autoscaler",
			"custom_details": map[string]interface{}{
				"budget_name":     msg.BudgetName,
				"alert_name":      msg.AlertName,
				"current_spend":   msg.CurrentSpend,
				"monthly_limit":   msg.MonthlyLimit,
				"percentage_used": msg.PercentageUsed,
				"threshold":       msg.Threshold,
			},
		},
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal pagerduty event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://events.pagerduty.com/v2/enqueue", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := am.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send pagerduty alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("pagerduty returned non-202 status: %d", resp.StatusCode)
	}

	logger.Info("Sent pagerduty alert",
		"budget", msg.BudgetName,
		"severity", msg.Severity,
	)

	return nil
}

// sendWebhookAlert sends alert to a custom webhook
func (am *AlertManager) sendWebhookAlert(ctx context.Context, channel v1alpha1.AlertChannel, msg AlertMessage) error {
	logger := log.FromContext(ctx)

	url := channel.Config["url"]
	if url == "" {
		return fmt.Errorf("webhook url not configured")
	}

	// Build webhook payload
	payload := map[string]interface{}{
		"budget_name":     msg.BudgetName,
		"alert_name":      msg.AlertName,
		"severity":        msg.Severity,
		"current_spend":   msg.CurrentSpend,
		"monthly_limit":   msg.MonthlyLimit,
		"percentage_used": msg.PercentageUsed,
		"threshold":       msg.Threshold,
		"timestamp":       msg.Timestamp.Format(time.RFC3339),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add custom headers if configured
	if authHeader := channel.Config["auth_header"]; authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := am.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}

	logger.Info("Sent webhook alert",
		"url", url,
		"budget", msg.BudgetName,
	)

	return nil
}

// formatEmailBody creates a formatted email body
func (am *AlertManager) formatEmailBody(msg AlertMessage) string {
	return fmt.Sprintf(`GPU Budget Alert

Budget: %s
Alert: %s
Severity: %s

Current Spending: $%.2f
Monthly Limit: $%.2f
Percentage Used: %.1f%%
Alert Threshold: %.0f%%

Timestamp: %s

This alert was triggered because your GPU spending has exceeded the configured threshold.

Please review your GPU resource usage and consider:
- Scaling down non-critical workloads
- Optimizing GPU utilization
- Reviewing autoscaling policies
- Adjusting budget limits if needed

---
GPU Autoscaler
`,
		msg.BudgetName,
		msg.AlertName,
		msg.Severity,
		msg.CurrentSpend,
		msg.MonthlyLimit,
		msg.PercentageUsed,
		msg.Threshold,
		msg.Timestamp.Format(time.RFC3339),
	)
}

// getSeverityColor returns a color code for Slack based on severity
func (am *AlertManager) getSeverityColor(severity string) string {
	switch severity {
	case "critical":
		return "danger"
	case "warning":
		return "warning"
	case "info":
		return "good"
	default:
		return "#808080"
	}
}

// mapSeverity maps our severity levels to PagerDuty severity levels
func (am *AlertManager) mapSeverity(severity string) string {
	switch severity {
	case "critical":
		return "critical"
	case "warning":
		return "warning"
	case "info":
		return "info"
	default:
		return "warning"
	}
}

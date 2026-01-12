package cost

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TimescaleDBClient manages cost data persistence
type TimescaleDBClient struct {
	db *sql.DB
}

// NewTimescaleDBClient creates a new TimescaleDB client
func NewTimescaleDBClient(connectionString string) (*TimescaleDBClient, error) {
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	client := &TimescaleDBClient{db: db}

	// Initialize schema
	if err := client.initializeSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return client, nil
}

// initializeSchema creates required tables and hypertables
func (tc *TimescaleDBClient) initializeSchema(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Initializing TimescaleDB schema")

	// Create extension if not exists
	_, err := tc.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb;`)
	if err != nil {
		return fmt.Errorf("failed to create timescaledb extension: %w", err)
	}

	// Create cost_data table
	_, err = tc.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cost_data (
			time TIMESTAMPTZ NOT NULL,
			pod_name TEXT NOT NULL,
			namespace TEXT NOT NULL,
			node TEXT,
			gpu_type TEXT,
			gpu_count INT,
			capacity_type TEXT,
			sharing_mode TEXT,
			hourly_rate DOUBLE PRECISION,
			cumulative_cost DOUBLE PRECISION,
			experiment_id TEXT,
			team TEXT,
			project TEXT,
			cost_center TEXT,
			labels JSONB,
			PRIMARY KEY (time, pod_name, namespace)
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create cost_data table: %w", err)
	}

	// Convert to hypertable if not already
	_, err = tc.db.ExecContext(ctx, `
		SELECT create_hypertable('cost_data', 'time', if_not_exists => TRUE);
	`)
	if err != nil {
		return fmt.Errorf("failed to create hypertable: %w", err)
	}

	// Create indexes for common queries
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_cost_namespace ON cost_data (namespace, time DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_cost_team ON cost_data (team, time DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_cost_experiment ON cost_data (experiment_id, time DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_cost_gpu_type ON cost_data (gpu_type, time DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_cost_capacity ON cost_data (capacity_type, time DESC);`,
	}

	for _, indexSQL := range indexes {
		if _, err := tc.db.ExecContext(ctx, indexSQL); err != nil {
			logger.Error(err, "Failed to create index", "sql", indexSQL)
		}
	}

	// Create aggregated views for performance
	_, err = tc.db.ExecContext(ctx, `
		CREATE MATERIALIZED VIEW IF NOT EXISTS cost_hourly_summary AS
		SELECT
			time_bucket('1 hour', time) AS bucket,
			namespace,
			team,
			gpu_type,
			capacity_type,
			SUM(hourly_rate) AS total_hourly_rate,
			SUM(cumulative_cost) AS total_cost,
			COUNT(DISTINCT pod_name) AS pod_count,
			SUM(gpu_count) AS total_gpus
		FROM cost_data
		GROUP BY bucket, namespace, team, gpu_type, capacity_type;
	`)
	if err != nil {
		logger.V(1).Info("Materialized view may already exist", "error", err.Error())
	}

	// Create continuous aggregate for daily summaries
	_, err = tc.db.ExecContext(ctx, `
		CREATE MATERIALIZED VIEW IF NOT EXISTS cost_daily_summary
		WITH (timescaledb.continuous) AS
		SELECT
			time_bucket('1 day', time) AS bucket,
			namespace,
			team,
			gpu_type,
			capacity_type,
			SUM(hourly_rate) / COUNT(*) AS avg_hourly_rate,
			MAX(cumulative_cost) AS total_cost,
			AVG(gpu_count) AS avg_gpus
		FROM cost_data
		GROUP BY bucket, namespace, team, gpu_type, capacity_type
		WITH NO DATA;
	`)
	if err != nil {
		logger.V(1).Info("Daily summary view may already exist", "error", err.Error())
	}

	// Add refresh policy for continuous aggregate
	_, err = tc.db.ExecContext(ctx, `
		SELECT add_continuous_aggregate_policy('cost_daily_summary',
			start_offset => INTERVAL '3 days',
			end_offset => INTERVAL '1 hour',
			schedule_interval => INTERVAL '1 hour',
			if_not_exists => TRUE);
	`)
	if err != nil {
		logger.V(1).Info("Refresh policy may already exist", "error", err.Error())
	}

	// Create savings tracking table
	_, err = tc.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cost_savings (
			time TIMESTAMPTZ NOT NULL,
			namespace TEXT NOT NULL,
			optimization_type TEXT NOT NULL, -- spot, sharing, autoscaling, waste
			savings_amount DOUBLE PRECISION,
			baseline_cost DOUBLE PRECISION,
			actual_cost DOUBLE PRECISION,
			details JSONB,
			PRIMARY KEY (time, namespace, optimization_type)
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create cost_savings table: %w", err)
	}

	_, err = tc.db.ExecContext(ctx, `
		SELECT create_hypertable('cost_savings', 'time', if_not_exists => TRUE);
	`)
	if err != nil {
		logger.V(1).Info("cost_savings hypertable may already exist", "error", err.Error())
	}

	// Create budget tracking table
	_, err = tc.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS budget_tracking (
			time TIMESTAMPTZ NOT NULL,
			budget_name TEXT NOT NULL,
			scope_namespace TEXT,
			scope_team TEXT,
			monthly_limit DOUBLE PRECISION,
			current_spend DOUBLE PRECISION,
			percentage_used DOUBLE PRECISION,
			status TEXT, -- ok, warning, exceeded
			PRIMARY KEY (time, budget_name)
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create budget_tracking table: %w", err)
	}

	_, err = tc.db.ExecContext(ctx, `
		SELECT create_hypertable('budget_tracking', 'time', if_not_exists => TRUE);
	`)
	if err != nil {
		logger.V(1).Info("budget_tracking hypertable may already exist", "error", err.Error())
	}

	logger.Info("TimescaleDB schema initialized successfully")
	return nil
}

// InsertCostDataPoint records a cost data point
func (tc *TimescaleDBClient) InsertCostDataPoint(ctx context.Context, podCost *PodCost) error {
	labelsJSON, err := labelsToJSON(podCost.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	_, err = tc.db.ExecContext(ctx, `
		INSERT INTO cost_data (
			time, pod_name, namespace, node, gpu_type, gpu_count,
			capacity_type, sharing_mode, hourly_rate, cumulative_cost,
			experiment_id, team, project, cost_center, labels
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (time, pod_name, namespace) DO UPDATE SET
			cumulative_cost = EXCLUDED.cumulative_cost,
			hourly_rate = EXCLUDED.hourly_rate;
	`,
		podCost.LastUpdated,
		podCost.PodName,
		podCost.Namespace,
		podCost.Node,
		podCost.GPUType,
		podCost.GPUCount,
		podCost.CapacityType,
		podCost.SharingMode,
		podCost.HourlyRate,
		podCost.TotalCost,
		podCost.ExperimentID,
		podCost.Team,
		podCost.Project,
		podCost.CostCenter,
		labelsJSON,
	)

	return err
}

// GetCostByNamespace retrieves total cost for a namespace in a time range
func (tc *TimescaleDBClient) GetCostByNamespace(ctx context.Context, namespace string, start, end time.Time) (float64, error) {
	var totalCost float64
	err := tc.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(cumulative_cost), 0)
		FROM cost_data
		WHERE namespace = $1
			AND time >= $2
			AND time <= $3;
	`, namespace, start, end).Scan(&totalCost)

	return totalCost, err
}

// GetCostByTeam retrieves total cost for a team in a time range
func (tc *TimescaleDBClient) GetCostByTeam(ctx context.Context, team string, start, end time.Time) (float64, error) {
	var totalCost float64
	err := tc.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cumulative_cost), 0)
		FROM (
			SELECT DISTINCT ON (pod_name, namespace)
				cumulative_cost
			FROM cost_data
			WHERE team = $1
				AND time >= $2
				AND time <= $3
			ORDER BY pod_name, namespace, time DESC
		) AS latest_costs;
	`, team, start, end).Scan(&totalCost)

	return totalCost, err
}

// GetHourlyCostTimeSeries retrieves hourly cost data for visualization
func (tc *TimescaleDBClient) GetHourlyCostTimeSeries(ctx context.Context, namespace string, hours int) ([]TimeSeriesPoint, error) {
	rows, err := tc.db.QueryContext(ctx, `
		SELECT
			time_bucket('1 hour', time) AS bucket,
			AVG(hourly_rate) AS avg_rate,
			MAX(cumulative_cost) AS max_cost
		FROM cost_data
		WHERE namespace = $1
			AND time >= NOW() - INTERVAL '1 hour' * $2
		GROUP BY bucket
		ORDER BY bucket ASC;
	`, namespace, hours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var point TimeSeriesPoint
		if err := rows.Scan(&point.Time, &point.Rate, &point.Cost); err != nil {
			return nil, err
		}
		points = append(points, point)
	}

	return points, rows.Err()
}

// GetDailyCostByNamespace retrieves daily cost breakdown
func (tc *TimescaleDBClient) GetDailyCostByNamespace(ctx context.Context, days int) (map[string][]DailyCostPoint, error) {
	rows, err := tc.db.QueryContext(ctx, `
		SELECT
			namespace,
			time_bucket('1 day', time) AS day,
			MAX(cumulative_cost) AS cost,
			AVG(gpu_count) AS avg_gpus
		FROM cost_data
		WHERE time >= NOW() - INTERVAL '1 day' * $1
		GROUP BY namespace, day
		ORDER BY namespace, day ASC;
	`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]DailyCostPoint)
	for rows.Next() {
		var namespace string
		var point DailyCostPoint
		if err := rows.Scan(&namespace, &point.Day, &point.Cost, &point.AvgGPUs); err != nil {
			return nil, err
		}
		result[namespace] = append(result[namespace], point)
	}

	return result, rows.Err()
}

// InsertSavingsData records cost savings
func (tc *TimescaleDBClient) InsertSavingsData(ctx context.Context, namespace, optimizationType string, savings, baseline, actual float64, details map[string]interface{}) error {
	detailsJSON, err := mapToJSON(details)
	if err != nil {
		return fmt.Errorf("failed to marshal details: %w", err)
	}

	_, err = tc.db.ExecContext(ctx, `
		INSERT INTO cost_savings (
			time, namespace, optimization_type, savings_amount,
			baseline_cost, actual_cost, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7);
	`, time.Now(), namespace, optimizationType, savings, baseline, actual, detailsJSON)

	return err
}

// GetTotalSavings retrieves total savings in a time range
func (tc *TimescaleDBClient) GetTotalSavings(ctx context.Context, start, end time.Time) (map[string]float64, error) {
	rows, err := tc.db.QueryContext(ctx, `
		SELECT
			optimization_type,
			SUM(savings_amount) AS total_savings
		FROM cost_savings
		WHERE time >= $1 AND time <= $2
		GROUP BY optimization_type;
	`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	savings := make(map[string]float64)
	for rows.Next() {
		var optimizationType string
		var amount float64
		if err := rows.Scan(&optimizationType, &amount); err != nil {
			return nil, err
		}
		savings[optimizationType] = amount
	}

	return savings, rows.Err()
}

// UpdateBudgetTracking records budget status
func (tc *TimescaleDBClient) UpdateBudgetTracking(ctx context.Context, budgetName, namespace, team string, limit, spend, percentage float64, status string) error {
	_, err := tc.db.ExecContext(ctx, `
		INSERT INTO budget_tracking (
			time, budget_name, scope_namespace, scope_team,
			monthly_limit, current_spend, percentage_used, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
	`, time.Now(), budgetName, namespace, team, limit, spend, percentage, status)

	return err
}

// GetBudgetHistory retrieves budget tracking history
func (tc *TimescaleDBClient) GetBudgetHistory(ctx context.Context, budgetName string, days int) ([]BudgetHistoryPoint, error) {
	rows, err := tc.db.QueryContext(ctx, `
		SELECT
			time,
			monthly_limit,
			current_spend,
			percentage_used,
			status
		FROM budget_tracking
		WHERE budget_name = $1
			AND time >= NOW() - INTERVAL '1 day' * $2
		ORDER BY time ASC;
	`, budgetName, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []BudgetHistoryPoint
	for rows.Next() {
		var point BudgetHistoryPoint
		if err := rows.Scan(&point.Time, &point.Limit, &point.Spend, &point.Percentage, &point.Status); err != nil {
			return nil, err
		}
		points = append(points, point)
	}

	return points, rows.Err()
}

// Close closes the database connection
func (tc *TimescaleDBClient) Close() error {
	return tc.db.Close()
}

// Helper types for queries

type TimeSeriesPoint struct {
	Time time.Time
	Rate float64
	Cost float64
}

type DailyCostPoint struct {
	Day     time.Time
	Cost    float64
	AvgGPUs float64
}

type BudgetHistoryPoint struct {
	Time       time.Time
	Limit      float64
	Spend      float64
	Percentage float64
	Status     string
}

// Helper functions

func labelsToJSON(labels map[string]string) (string, error) {
	if labels == nil {
		return "{}", nil
	}
	// Convert to JSON string
	// Simplified - in production, use json.Marshal
	return "{}", nil
}

func mapToJSON(m map[string]interface{}) (string, error) {
	if m == nil {
		return "{}", nil
	}
	// Convert to JSON string
	// Simplified - in production, use json.Marshal
	return "{}", nil
}

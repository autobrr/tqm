package expression

import (
	"context"
	"fmt"

	"github.com/expr-lang/expr"

	"github.com/autobrr/tqm/pkg/config"
)

func CheckTorrentSingleMatch(ctx context.Context, t *config.Torrent, expressions []CompiledExpression) (bool, error) {
	match, _, err := CheckTorrentSingleMatchWithReason(ctx, t, expressions)
	return match, err
}

func CheckTorrentAllMatch(ctx context.Context, t *config.Torrent, expressions []CompiledExpression) (bool, error) {
	match, _, err := CheckTorrentAllMatchWithReason(ctx, t, expressions)
	return match, err
}

func CheckTorrentSingleMatchWithReason(ctx context.Context, t *config.Torrent, expressions []CompiledExpression) (bool, string, error) {
	env := &evalContext{Torrent: t, ctx: ctx}

	for _, expression := range expressions {
		result, err := expr.Run(expression.Program, env)
		if err != nil {
			return false, "", fmt.Errorf("check expression: %w", err)
		}

		expResult, ok := result.(bool)
		if !ok {
			return false, "", fmt.Errorf("type assert expression result: %w", err)
		}

		if expResult {
			return true, expression.Text, nil
		}
	}

	return false, "", nil
}

func CheckTorrentAllMatchWithReason(ctx context.Context, t *config.Torrent, expressions []CompiledExpression) (bool, []string, error) {
	env := &evalContext{Torrent: t, ctx: ctx}
	var failedExpressions []string

	for _, expression := range expressions {
		result, err := expr.Run(expression.Program, env)
		if err != nil {
			return false, nil, fmt.Errorf("check expression: %w", err)
		}

		expResult, ok := result.(bool)
		if !ok {
			return false, nil, fmt.Errorf("type assert expression result: %w", err)
		}

		if !expResult {
			failedExpressions = append(failedExpressions, expression.Text)
		}
	}

	if len(failedExpressions) > 0 {
		return false, failedExpressions, nil
	}

	return true, nil, nil
}

func EvaluateFloat64Expression(ctx context.Context, t *config.Torrent, expression *CompiledExpression) (float64, error) {
	env := &evalContext{Torrent: t, ctx: ctx}

	result, err := expr.Run(expression.Program, env)
	if err != nil {
		return 0, fmt.Errorf("evaluate expression: %w", err)
	}

	switch v := result.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("expression result is not a number: %T", result)
	}
}

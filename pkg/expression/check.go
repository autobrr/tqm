package expression

import (
	"context"
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/autobrr/tqm/pkg/config"
)

func CheckTorrentSingleMatch(ctx context.Context, t *config.Torrent, exp []*vm.Program) (bool, error) {
	env := &evalContext{Torrent: t, ctx: ctx}

	for _, expression := range exp {
		result, err := expr.Run(expression, env)
		if err != nil {
			return false, fmt.Errorf("check expression: %w", err)
		}

		expResult, ok := result.(bool)
		if !ok {
			return false, fmt.Errorf("type assert expression result: %w", err)
		}

		if expResult {
			return true, nil
		}
	}

	return false, nil
}

func CheckTorrentAllMatch(ctx context.Context, t *config.Torrent, exp []*vm.Program) (bool, error) {
	env := &evalContext{Torrent: t, ctx: ctx}

	for _, expression := range exp {
		result, err := expr.Run(expression, env)
		if err != nil {
			return false, fmt.Errorf("check expression: %w", err)
		}

		expResult, ok := result.(bool)
		if !ok {
			return false, fmt.Errorf("type assert expression result: %w", err)
		}

		if !expResult {
			return false, nil
		}
	}

	return true, nil
}

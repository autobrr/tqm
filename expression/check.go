package expression

import (
	"fmt"

	"github.com/autobrr/tqm/config"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

func CheckTorrentSingleMatch(t *config.Torrent, exp []*vm.Program) (bool, error) {
	for _, expression := range exp {
		result, err := expr.Run(expression, t)
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

func CheckTorrentAllMatch(t *config.Torrent, exp []*vm.Program) (bool, error) {
	for _, expression := range exp {
		result, err := expr.Run(expression, t)
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

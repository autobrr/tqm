package expression

import (
	"context"
	"fmt"

	"github.com/expr-lang/expr"

	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/regex"
)

type evalContext struct {
	*config.Torrent
	ctx context.Context
}

func (e *evalContext) IsUnregistered() bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.IsUnregistered(e.ctx)
}

func (e *evalContext) IsTrackerDown() bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.IsTrackerDown()
}

func (e *evalContext) HasAllTags(tags ...string) bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.HasAllTags(tags...)
}

func (e *evalContext) HasAnyTag(tags ...string) bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.HasAnyTag(tags...)
}

func (e *evalContext) HasMissingFiles() bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.HasMissingFiles()
}

func (e *evalContext) RegexMatch(pattern string) bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.RegexMatch(pattern)
}

func (e *evalContext) RegexMatchAny(patternsStr string) bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.RegexMatchAny(patternsStr)
}

func (e *evalContext) RegexMatchAll(patternsStr string) bool {
	if e.Torrent == nil {
		return false
	}
	return e.Torrent.RegexMatchAll(patternsStr)
}

func Compile(filter *config.FilterConfiguration) (*Expressions, error) {
	exprEnv := &evalContext{}
	exp := new(Expressions)

	// validate all regex patterns in expressions
	patterns, err := getAllPatternsFromFilter(filter)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	if err := regex.ValidatePatterns(patterns); err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// compile ignores
	for _, ignoreExpr := range filter.Ignore {
		program, err := expr.Compile(ignoreExpr, expr.Env(exprEnv), expr.AsBool())
		if err != nil {
			return nil, fmt.Errorf("compile ignore expression: %q: %w", ignoreExpr, err)
		}

		exp.Ignores = append(exp.Ignores, CompiledExpression{
			Program: program,
			Text:    ignoreExpr,
		})
	}

	// compile removes
	for _, removeExpr := range filter.Remove {
		program, err := expr.Compile(removeExpr, expr.Env(exprEnv), expr.AsBool())
		if err != nil {
			return nil, fmt.Errorf("compile remove expression: %q: %w", removeExpr, err)
		}

		exp.Removes = append(exp.Removes, CompiledExpression{
			Program: program,
			Text:    removeExpr,
		})
	}

	// compile pauses
	for _, pauseExpr := range filter.Pause {
		program, err := expr.Compile(pauseExpr, expr.Env(exprEnv), expr.AsBool())
		if err != nil {
			return nil, fmt.Errorf("compile pause expression: %q: %w", pauseExpr, err)
		}

		exp.Pauses = append(exp.Pauses, CompiledExpression{
			Program: program,
			Text:    pauseExpr,
		})
	}

	// compile labels
	for _, labelExpr := range filter.Label {
		le := &LabelExpression{Name: labelExpr.Name}

		// compile updates
		for _, updateExpr := range labelExpr.Update {
			program, err := expr.Compile(updateExpr, expr.Env(exprEnv), expr.AsBool())
			if err != nil {
				return nil, fmt.Errorf("compile label update expression: %v: %q: %w", labelExpr.Name, updateExpr, err)
			}

			le.Updates = append(le.Updates, CompiledExpression{
				Program: program,
				Text:    updateExpr,
			})
		}

		exp.Labels = append(exp.Labels, le)
	}

	// compile tags
	for _, tagExpr := range filter.Tag {
		le := &TagExpression{Name: tagExpr.Name, Mode: tagExpr.Mode, UploadKb: tagExpr.UploadKb}

		if le.Mode == "" {
			le.Mode = "full"
		}

		switch le.Mode {
		case "add", "remove", "full":
		default:
			return nil, fmt.Errorf("invalid tag mode '%s' for tag '%s', must be one of: add, remove, full", le.Mode, le.Name)
		}

		// compile updates
		for _, updateExpr := range tagExpr.Update {
			program, err := expr.Compile(updateExpr, expr.Env(exprEnv), expr.AsBool())
			if err != nil {
				return nil, fmt.Errorf("compile tag update expression: %v: %q: %w", tagExpr.Name, updateExpr, err)
			}

			le.Updates = append(le.Updates, CompiledExpression{
				Program: program,
				Text:    updateExpr,
			})
		}

		exp.Tags = append(exp.Tags, le)
	}

	return exp, nil
}
